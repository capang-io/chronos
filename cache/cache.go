package cache

import (
	"chronos/models"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// Global context for Redis operations
var ctx = context.Background()

type Cache struct {
	client *redis.Client
}

type CacheEntry struct {
	Key   string
	Value string
}

// Stats holds the count and the exact insertion DateTime objects
type Stats struct {
	Count   int
	MinTime time.Time
	MaxTime time.Time
}

const DefaultTTL = 1 * time.Hour

var (
	instance *Cache
	once     sync.Once
)

func GetInstance() (*Cache, error) {
	var err error

	// sync.Once guarantees that the connection logic is executed EXACTLY once
	once.Do(func() {
		// Redis configuration (modify address and password if necessary)
		dbStr := os.Getenv("REDIS_DB")
		db := 0
		if dbStr != "" {
			db, _ = strconv.Atoi(dbStr)
		}

		// Pool and timeout tuning with sane defaults, overridable via env
		poolSize := 10
		if s := os.Getenv("REDIS_POOL_SIZE"); s != "" {
			if n, e := strconv.Atoi(s); e == nil {
				poolSize = n
			}
		}

		minIdle := 2
		if s := os.Getenv("REDIS_MIN_IDLE"); s != "" {
			if n, e := strconv.Atoi(s); e == nil {
				minIdle = n
			}
		}

		dialTimeout := 5 * time.Second
		readTimeout := 5 * time.Second
		writeTimeout := 5 * time.Second

		rdb := redis.NewClient(&redis.Options{
			Addr:            os.Getenv("REDIS_ADDR"),
			Password:        os.Getenv("REDIS_PASSWORD"),
			DB:              db,
			PoolSize:        poolSize,
			MinIdleConns:    minIdle,
			DialTimeout:     dialTimeout,
			ReadTimeout:     readTimeout,
			WriteTimeout:    writeTimeout,
			MaxRetries:      3,
			MinRetryBackoff: 100 * time.Millisecond,
			MaxRetryBackoff: 1 * time.Second,
		})

		// Verify connection
		if _, pingErr := rdb.Ping(ctx).Result(); pingErr != nil {
			err = pingErr
			return
		}

		instance = &Cache{client: rdb}
	})

	return instance, err
}

// CloseCache closes the connection to the Redis server
func (c *Cache) CloseCache() error {
	if c.client != nil {
		return c.client.Close()
	}
	return nil
}

// ReadFromCache retrieves a specific item from Redis
func (c *Cache) ReadFromCache(primaryKey string, rowKey int) (*models.ResponseStatus, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("redis client is not initialized")
	}

	key := fmt.Sprintf("%s:%d", primaryKey, rowKey)

	val, err := c.client.Get(ctx, key).Bytes()
	if err == redis.Nil {
		return nil, fmt.Errorf("key not found")
	} else if err != nil {
		return nil, err
	}

	var status models.ResponseStatus
	if err := json.Unmarshal(val, &status); err != nil {
		return nil, err
	}

	return &status, nil
}

func (c *Cache) WriteToCache(status models.ResponseStatus) error {
	now := time.Now()
	data, err := json.Marshal(status)
	if err != nil {
		return err
	}

	key := fmt.Sprintf("%s:%d", status.PrimaryKey, status.RowKey)

	// Use a pipeline to reduce round-trips for the write
	pipe := c.client.Pipeline()
	pipe.Set(ctx, key, data, DefaultTTL)
	if _, err := pipe.Exec(ctx); err != nil {
		return err
	}

	// Update stats using a Lua script (atomic min/max + incr)
	return c.UpdateStats(status.PrimaryKey, now)
}

// UpdateStats updates count, min and max insertion times in a Redis Hash
func (c *Cache) UpdateStats(primaryKey string, insertionTime time.Time) error {
	statsKey := fmt.Sprintf("stats:%s", primaryKey)

	// Convert time to string for storage
	newTimeStr := insertionTime.Format(time.RFC3339Nano)
	// Lua script to atomically HINCRBY, update min/max and set TTL
	script := `
local statsKey = KEYS[1]
local newTime = ARGV[1]
local ttl = tonumber(ARGV[2])
redis.call('HINCRBY', statsKey, 'count', 1)
local curMin = redis.call('HGET', statsKey, 'min_time')
if (not curMin) or (newTime < curMin) then
  redis.call('HSET', statsKey, 'min_time', newTime)
end
local curMax = redis.call('HGET', statsKey, 'max_time')
if (not curMax) or (newTime > curMax) then
  redis.call('HSET', statsKey, 'max_time', newTime)
end
if ttl and ttl > 0 then
  redis.call('EXPIRE', statsKey, ttl)
end
return 1
`

	ttlSeconds := int(DefaultTTL.Seconds())
	return c.client.Eval(ctx, script, []string{statsKey}, newTimeStr, ttlSeconds).Err()
}

// Listen waits for updates on the channel and writes them to Redis
func (c *Cache) Listen(statusQ <-chan models.ResponseStatus) {
	for status := range statusQ {
		if err := c.WriteToCache(status); err != nil {
			log.Printf("Error writing to Redis: %v", err)
		}
	}
}

// GetData retrieves all entries associated with a given primaryKey prefix
func (c *Cache) GetData(primaryKey string) ([]CacheEntry, error) {
	var results []CacheEntry
	prefix := primaryKey + ":*"

	// Use Scan instead of Keys to avoid blocking the Redis server on large databases
	iter := c.client.Scan(ctx, 0, prefix, 0).Iterator()

	// Batch keys to use MGET and reduce round-trips
	batch := make([]string, 0, 128)
	const batchSize = 100

	flushBatch := func() error {
		if len(batch) == 0 {
			return nil
		}
		vals, err := c.client.MGet(ctx, batch...).Result()
		if err != nil {
			return err
		}

		for i, v := range vals {
			if v == nil {
				continue
			}
			if str, ok := v.(string); ok {
				results = append(results, CacheEntry{Key: batch[i], Value: str})
			} else if b, ok := v.([]byte); ok {
				results = append(results, CacheEntry{Key: batch[i], Value: string(b)})
			}
		}
		batch = batch[:0]
		return nil
	}

	for iter.Next(ctx) {
		key := iter.Val()
		batch = append(batch, key)
		if len(batch) >= batchSize {
			if err := flushBatch(); err != nil {
				return nil, err
			}
		}
	}

	if err := iter.Err(); err != nil {
		return nil, err
	}

	if err := flushBatch(); err != nil {
		return nil, err
	}

	return results, nil
}

type RealTimeStats struct {
	Count   int
	MinTime time.Time
	MaxTime time.Time
}

func (c *Cache) GetStats(primaryKey string) (*RealTimeStats, error) {
	statsKey := fmt.Sprintf("stats:%s", primaryKey)

	data, err := c.client.HGetAll(ctx, statsKey).Result()
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("no stats found")
	}

	count, _ := strconv.Atoi(data["count"])
	minTime, _ := time.Parse(time.RFC3339Nano, data["min_time"])
	maxTime, _ := time.Parse(time.RFC3339Nano, data["max_time"])

	return &RealTimeStats{
		Count:   count,
		MinTime: minTime,
		MaxTime: maxTime,
	}, nil
}

// Print logs all retrieved data for a specific primaryKey
func (c *Cache) Print(primaryKey string) {
	results, err := c.GetData(primaryKey)
	if err != nil {
		log.Printf("Error during data retrieval: %v", err)
		return
	}

	if len(results) == 0 {
		log.Printf("No data found for the primaryKey: %s\n", primaryKey)
		return
	}

	log.Printf("Found %d elements for the primaryKey '%s':\n", len(results), primaryKey)
	for _, entry := range results {
		log.Printf("KEY: %-10s | VALUE: %s\n", entry.Key, entry.Value)
	}
}
