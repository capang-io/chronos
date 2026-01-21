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

		rdb := redis.NewClient(&redis.Options{
			Addr:     os.Getenv("REDIS_ADDR"),
			Password: os.Getenv("REDIS_PASSWORD"),
			DB:       db,
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

	// 1. Write the main data
	err = c.client.Set(ctx, key, data, DefaultTTL).Err()
	if err != nil {
		return err
	}

	// 2. Update the aggregated statistics
	return c.UpdateStats(status.PrimaryKey, now)
}

// UpdateStats updates count, min and max insertion times in a Redis Hash
func (c *Cache) UpdateStats(primaryKey string, insertionTime time.Time) error {
	statsKey := fmt.Sprintf("stats:%s", primaryKey)

	// Convert time to string for storage
	newTimeStr := insertionTime.Format(time.RFC3339Nano)

	// Increment count
	err := c.client.HIncrBy(ctx, statsKey, "count", 1).Err()
	if err != nil {
		return err
	}

	// Handle Min/Max logic: we use a transaction to fetch current values and update if necessary
	res, err := c.client.HMGet(ctx, statsKey, "min_time", "max_time").Result()
	if err != nil {
		return err
	}

	// Logic to update Min
	if res[0] == nil {
		c.client.HSet(ctx, statsKey, "min_time", newTimeStr)
	} else {
		currentMin, _ := time.Parse(time.RFC3339Nano, res[0].(string))
		if insertionTime.Before(currentMin) {
			c.client.HSet(ctx, statsKey, "min_time", newTimeStr)
		}
	}

	// Logic to update Max
	if res[1] == nil {
		c.client.HSet(ctx, statsKey, "max_time", newTimeStr)
	} else {
		currentMax, _ := time.Parse(time.RFC3339Nano, res[1].(string))
		if insertionTime.After(currentMax) {
			c.client.HSet(ctx, statsKey, "max_time", newTimeStr)
		}
	}

	// Keep the stats alive for the same duration as the data
	return c.client.Expire(ctx, statsKey, DefaultTTL).Err()
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

	for iter.Next(ctx) {
		key := iter.Val()
		val, err := c.client.Get(ctx, key).Result()
		if err != nil {
			log.Printf("Error getting value for key %s: %v", key, err)
			continue
		}

		results = append(results, CacheEntry{
			Key:   key,
			Value: val,
		})
	}

	if err := iter.Err(); err != nil {
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
