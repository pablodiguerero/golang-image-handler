package main

import (
	"log"
	"net/http"
	"os"
	"time"

	"image.it-lab.su/internal/imagehandler"
)

func main() {
	handler, err := imagehandler.NewFromEnv()
	if err != nil {
		log.Fatal(err)
	}

	port := os.Getenv("APP_PORT")
	if port == "" {
		port = "80"
	}

	mux := http.NewServeMux()
	mux.Handle("/images/", handler)
	mux.HandleFunc("/health-check/", handler.HealthCheck)

	server := &http.Server{
		Addr:              ":" + port,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	log.Printf("image handler listening on %s", server.Addr)
	log.Fatal(server.ListenAndServe())
}
