package worker

import (
	"bufio"
	"chronos/cache"
	"chronos/models"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const (
	// Default number of Consumers for the pool
	DefaultNumConsumers = 4
	// Default size for the record queue channel
	DefaultBufferSize = 100
)

// The primary struct representing the background job runner.
type Runner struct {
	Cache *cache.Cache
	// Shared resources (channels, Consumer pool control)
	recordsQ     chan Record
	statusQ      chan models.ResponseStatus
	numConsumers int

	// WaitGroup for tracking the permanent Consumer goroutines
	ConsumerWG sync.WaitGroup
	// Channel to signal Consumers to stop
	stopConsumers chan struct{}
}

// NewRunner initializes and returns a new Runner instance with the necessary dependencies
// and initializes the shared channels and Consumer count.
func NewRunner() (*Runner, error) {
	c, err := cache.GetInstance()
	if err != nil {
		return nil, fmt.Errorf("failed to start cache: %w", err)
	}

	return &Runner{
		Cache:         c,
		recordsQ:      make(chan Record, DefaultBufferSize),
		statusQ:       make(chan models.ResponseStatus, DefaultBufferSize),
		numConsumers:  DefaultNumConsumers,
		stopConsumers: make(chan struct{}),
	}, nil
}

// Start launches the background goroutines (Consumers and cache writer).
func (r *Runner) Start() {
	log.Printf("Starting shared runner with %d Consumers...", r.numConsumers)

	// This goroutine listens to statusQ and writes to the cache
	go r.Cache.Listen(r.statusQ)

	// Start Consumer pool
	r.ConsumerWG.Add(r.numConsumers)
	for i := 1; i <= r.numConsumers; i++ {
		go Consumer(i, r.recordsQ, r.statusQ, &r.ConsumerWG)
	}
}

// Close performs a graceful shutdown of the Consumer pool and closes the cache.
func (r *Runner) Close() {
	// Signal Consumers to stop
	close(r.stopConsumers)

	// Wait for all Consumers to finish
	r.ConsumerWG.Wait()

	// Close the shared statusQ channel
	close(r.statusQ)

	// Close the cache
	if err := r.Cache.CloseCache(); err != nil {
		log.Printf("Error closing cache: %v", err)
	}

	log.Println("Chronos runner gracefully shut down.")
}

// Process a file using the existing Consumer pool.
func (r *Runner) Run(filePath string) error {
	log.Printf("Dispatching job for file: %s", filePath)

	// File and Configuration Reading
	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("failed to open file %s: %w", filePath, err)
	}

	conf, err := readConfiguration(file)
	if err != nil {
		return fmt.Errorf("failed to read configuration: %w", err)
	}

	primaryKey := conf.PrimaryKey

	go Publish(primaryKey, file, conf, r.recordsQ)

	// Since the Consumers are permanent, we don't call wg.Wait() here.
	// The `Run` method immediately returns, allowing the caller to dispatch another job.

	return nil
}

// readConfiguration reads the first line of the file and decodes it into a Configuration struct
func readConfiguration(f *os.File) (models.Configuration, error) {
	// Rewind the file pointer to the beginning before scanning
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return models.Configuration{}, fmt.Errorf("failed to rewind file pointer: %w", err)
	}

	scanner := bufio.NewScanner(f)

	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return models.Configuration{}, fmt.Errorf("error during file scan: %w", err)
		}
		return models.Configuration{}, fmt.Errorf("the file is empty or does not contain any records")
	}

	line := scanner.Text()
	var conf models.Configuration

	if err := json.Unmarshal([]byte(line), &conf); err != nil {
		return models.Configuration{}, fmt.Errorf("failed to decode JSON: %w", err)
	}

	// Set offset for the next read to start at the second line (after the config line)
	// We advance the file pointer by the length of the line read + 1 for the newline character
	if _, err := f.Seek(int64(len(line))+1, io.SeekStart); err != nil {
		return models.Configuration{}, fmt.Errorf("failed to advance file pointer: %w", err)
	}

	conf.PrimaryKey = strings.TrimSuffix(filepath.Base(f.Name()), ".ndjson")

	return conf, nil
}
