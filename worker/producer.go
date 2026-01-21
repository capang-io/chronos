package worker

import (
	"chronos/models"
	"encoding/json"
	"io"
	"log"
	"os"
)

// Record matches NDJSON structure
type Record struct {
	PrimaryKey    string
	RowKey        int    `json:"id,omitempty"`
	Payload       string `json:"payload"`
	Configuration models.Configuration
}

// Producer: reads from NDJSON and sends to channel
func Publish(primaryKey string, f *os.File, conf models.Configuration, ch chan<- Record) {
	decoder := json.NewDecoder(f)

	for {
		var rec Record
		if err := decoder.Decode(&rec); err != nil {
			if err == io.EOF {
				break
			}
			log.Printf("decode error: %v", err)
			continue
		}

		rec.PrimaryKey = primaryKey
		rec.Configuration = conf
		ch <- rec
	}
}
