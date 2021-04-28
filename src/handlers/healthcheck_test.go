package handlers_test

import (
	"io"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"image.it-lab.su/handlers"
)

func TestHealthCheckHandler(t *testing.T) {
    req := httptest.NewRequest("GET", "/", nil)
    w := httptest.NewRecorder()
    handlers.HealthcheckHandler(w, req)

    resp := w.Result()
    body, _ := io.ReadAll(resp.Body)

    assert.Equal(t, resp.StatusCode, 200)
    assert.GreaterOrEqual(t, len(body), 1)
}
