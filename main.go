package main

import (
	"chronos/cache"
	"chronos/service"
	"chronos/worker"
	"log"
	"net/http"
	"os"
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

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Fatal(http.ListenAndServe(":"+port, nil))
}
