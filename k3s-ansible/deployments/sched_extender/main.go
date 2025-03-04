package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"sched_extender/pkg/sched_extension"
)

func main() {
	var port int
	flag.IntVar(&port, "port", 8888, "Port to listen on")
	flag.Parse()

	log.Printf("Starting Multi-Resource Scheduler Extender on port %d", port)
	server := sched_extension.SetupHTTPServer(port)

	// Start the server in a goroutine
	go func() {
		if err := server.ListenAndServe(); err != nil {
			log.Fatalf("HTTP server error: %v", err)
		}
	}()

	// Set up signal handling for graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	<-sigCh

	log.Println("Shutting down server...")
}
