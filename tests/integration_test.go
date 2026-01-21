package tests

import (
	"bufio"
	"chronos/cache"
	"chronos/service"
	"chronos/worker"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

// Helper function to create the "Huge File" dynamically
func generateTestFile(t *testing.T, numLines int) string {
	t.Helper() // Marks this as a helper for better error reporting

	tempFile, err := os.CreateTemp("", "huge_test_*.ndjson")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer tempFile.Close()

	writer := bufio.NewWriter(tempFile)
	header := `{"protocol":"http","host":"localhost","port":"8080","path":"/upload",",metadata":[{"key":"Accept", "value":"*/*"}]}`
	writer.WriteString(header + "\n")

	for i := 0; i < numLines; i++ {
		fmt.Fprintf(writer, `{"id":%d,"payload":"payload%d"}`+"\n", i, i)
	}

	writer.Flush()
	return tempFile.Name()
}

func TestLargeJobUpload_Integration(t *testing.T) {
	// Setup Dependencies
	r, _ := worker.NewRunner()
	r.Start()
	c, _ := cache.GetInstance()

	// Create the handler (using the clean JobHandler we discussed earlier)
	h := service.NewJobHandler(r, c)

	// Generate a "huge" file for the test (e.g., 50,000 lines)
	filePath := generateTestFile(t, 50000)
	defer os.Remove(filePath) // Clean up after test finishes

	// Open the generated file to stream it in the request
	fileReader, err := os.Open(filePath)
	if err != nil {
		t.Fatalf("Could not open test file: %v", err)
	}
	defer fileReader.Close()

	// Setup Test Server
	req := httptest.NewRequest(http.MethodPost, "/run", fileReader)
	req.Header.Set("Content-Type", "application/x-ndjson")
	rr := httptest.NewRecorder()

	// Execute
	h.HandleRun(rr, req)

	// Assertions
	if rr.Code != http.StatusAccepted {
		t.Errorf("Expected status 202, got %d. Body: %s", rr.Code, rr.Body.String())
	}

	jobID := rr.Header().Get("X-Job-ID")
	if jobID == "" {
		t.Error("Response did not return an X-Job-ID header")
	}
}
