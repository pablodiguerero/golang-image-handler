package handlers

import (
	"net/http"
)

// HealthcheckHandler - static handler for provide healthcheck info
func HealthcheckHandler(w http.ResponseWriter, r *http.Request) {
    w.Header().Add("ContentType", "application/json")
    w.Write([]byte("{\"status\": \"ok\"}"))
}
