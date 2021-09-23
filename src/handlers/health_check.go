package handlers

import (
    "encoding/json"
    "io/ioutil"
    "log"
    "net/http"
    "os"
)

type HealthResponse struct {
    Status  string `json:"status"`
    Healthy bool   `json:"healthy"`
}

func HealthCheckHandler(writer http.ResponseWriter, request *http.Request) {
    defer func() {
        if err := recover(); err != nil {
            log.Println(err)

            writer.WriteHeader(http.StatusInternalServerError)
            writer.Write([]byte("Error while request handling"))
        }
    }()

    files, err := ioutil.ReadDir(os.Getenv("APP_IMAGE_ROOT"))

    if err != nil {
        panic(err)
    }

    response := &HealthResponse{Status: "OK", Healthy: len(files) > 0}
    jsonResponse, _ := json.Marshal(response)
    writer.Header().Add("Content-Type", "application/json")
    writer.Write(jsonResponse)
}
