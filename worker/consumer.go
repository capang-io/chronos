package worker

import (
	"bytes"
	"chronos/models"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"
)

// The Consumer function processes records from the recordsQ, calls the task, and
// sends the result to the statusQ.
func Consumer(id int, recordsQ <-chan Record, chStatus chan<- models.ResponseStatus, wg *sync.WaitGroup) {
	defer wg.Done()

	log.Printf("Consumer %d started\n", id)

	for rec := range recordsQ {
		respBody, statusCode, err := Task(rec.Payload, rec.Configuration)
		statusString := "success"
		errString := ""

		if err != nil {
			statusString = "failed"
			errString = err.Error()
			log.Printf("Consumer %d failed to process %s:%d. Status: %d, Error: %s",
				id, rec.PrimaryKey, rec.RowKey, statusCode, errString)
		}

		// Send the final status to the cache writer channel.
		chStatus <- models.ResponseStatus{
			PrimaryKey: rec.PrimaryKey,
			RowKey:     rec.RowKey,
			Status:     statusString,
			// Code:       statusCode,
			Response: respBody,
			Error:    errString,
		}
	}
	log.Printf("Consumer %d finished\n", id)
}

// Task sends the payload and returns the response body, status code, and any error.
func Task(payload string, conf models.Configuration) (string, int, error) {
	// Check protocol
	if conf.Protocol != "http" && conf.Protocol != "https" {
		return "", 0, fmt.Errorf("unsupported protocol: %s", conf.Protocol)
	}

	// Construct the URI from the configuration
	uri := fmt.Sprintf("%s://%s:%v%s", conf.Protocol, conf.Host, conf.Port, conf.Path)

	// Prepare the payload
	jsonPayload := bytes.NewBufferString(payload)

	// Perform the request
	resp, err := http.Post(uri, "application/json", jsonPayload)
	if err != nil {
		return "", 0, fmt.Errorf("error sending POST request: %w", err)
	}

	// Ensure the response body is closed to prevent leaks
	defer resp.Body.Close()

	// Read the response body
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", resp.StatusCode, fmt.Errorf("error reading response body: %w", err)
	}

	// Return the result: cast the byte slice to a string
	return string(bodyBytes), resp.StatusCode, nil
}
