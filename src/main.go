package main

import (
	"fmt"
	"net/http"
	"os"

	"image.it-lab.su/handlers"
)

func main() {
    port := os.Getenv("APP_PORT")

    if port == "" {
        port = "80"
    }

    mux := http.ServeMux{}

    mux.HandleFunc("/health/", handlers.HealthcheckHandler)
    mux.HandleFunc("/images/", handlers.ImageHandler)

    http.ListenAndServe(fmt.Sprintf(":%s", port), &mux)
}
