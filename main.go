package main

import (
	"bytes"
	"chronos/cache"
	"chronos/service"
	"chronos/worker"
	"log"
	"net/http"
	_ "net/http/pprof"
	"os"
	"runtime/trace"
	"strconv"
	"time"
)

func main() {
	// Initialize dependencies
	r, _ := worker.NewRunner()
	r.Start()

	c, _ := cache.GetInstance()

	// Initialize handlers
	h := service.NewJobHandler(r, c)

	// Register routes
	http.HandleFunc("/run", h.HandleRun)
	http.HandleFunc("/status", h.HandleStatus)
	http.HandleFunc("/info", h.HandleInfo)

	// pprof endpoints are registered by importing net/http/pprof for side-effects

	// runtime/trace capture endpoint: /debug/trace?duration=5 (seconds)
	http.HandleFunc("/debug/trace/capture", func(w http.ResponseWriter, r *http.Request) {
		dur := 5
		if s := r.URL.Query().Get("duration"); s != "" {
			if n, err := strconv.Atoi(s); err == nil && n > 0 {
				dur = n
			}
		}

		var buf bytes.Buffer
		if err := trace.Start(&buf); err != nil {
			http.Error(w, "failed to start trace: "+err.Error(), http.StatusInternalServerError)
			return
		}

		// Capture for the requested duration
		time.Sleep(time.Duration(dur) * time.Second)
		trace.Stop()

		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", "attachment; filename=trace.out")
		if _, err := w.Write(buf.Bytes()); err != nil {
			log.Printf("error writing trace response: %v", err)
		}
	})

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Fatal(http.ListenAndServe(":"+port, nil))
}
