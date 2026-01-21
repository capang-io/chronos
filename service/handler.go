package service

import (
	"chronos/cache"
	"chronos/worker"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
)

// JobHandler encapsulates all dependencies for our API routes.
type JobHandler struct {
	runner *worker.Runner
	cache  *cache.Cache
}

// NewJobHandler creates a new instance with its required dependencies.
func NewJobHandler(r *worker.Runner, c *cache.Cache) *JobHandler {
	return &JobHandler{
		runner: r,
		cache:  c,
	}
}

// HandleRun processes the NDJSON upload and starts a job.
func (h *JobHandler) HandleRun(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed. Use POST.", http.StatusMethodNotAllowed)
		return
	}

	contentType := r.Header.Get("Content-Type")
	if contentType != "application/x-ndjson" && contentType != "application/jsonl" {
		http.Error(w, "Invalid Content-Type. Expected application/x-ndjson", http.StatusUnsupportedMediaType)
		return
	}

	jobUUID := uuid.New().String()
	tempFile, err := os.CreateTemp("", fmt.Sprintf("job-%s-*.ndjson", jobUUID))
	if err != nil {
		log.Printf("Error creating temp file: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	defer tempFile.Close()

	if _, err = io.Copy(tempFile, r.Body); err != nil {
		os.Remove(tempFile.Name())
		http.Error(w, "Error while reading NDJSON data.", http.StatusInternalServerError)
		return
	}

	filePath := tempFile.Name()
	jobID := strings.TrimSuffix(filepath.Base(filePath), ".ndjson")

	// Execute the job in the background
	go h.asyncRunJob(filePath)

	w.Header().Set("X-Job-ID", jobID)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]string{
		"message": "Job accepted",
		"job_id":  jobID,
	})
}

// HandleStatus checks the job status using the injected cache.
func (h *JobHandler) HandleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed. Use GET.", http.StatusMethodNotAllowed)
		return
	}

	keys := r.URL.Query()["key"]
	if len(keys) == 0 {
		http.Error(w, "Missing parameter 'key'", http.StatusBadRequest)
		return
	}

	// Using the injected cache client instead of GetInstance()
	statusData, err := h.cache.GetStats(keys[0])
	if err != nil {
		http.Error(w, "Error reading from cache", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(statusData)
}

// HandleInfo provides a simple health check.
func (h *JobHandler) HandleInfo(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok"))
}

// asyncRunJob is an internal helper for background processing.
func (h *JobHandler) asyncRunJob(filePath string) {
	defer os.Remove(filePath)
	if err := h.runner.Run(filePath); err != nil {
		log.Printf("Error executing job for file %s: %v", filePath, err)
	}
}
