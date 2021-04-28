package handlers_test

import (
	"io"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"image.it-lab.su/handlers"
)

func TestImageResponse(t *testing.T) {
    request := httptest.NewRequest("GET", "/images/files/cover-1-fill-300x900.jpg", nil)
    writer := httptest.NewRecorder()

    handlers.ImageHandler(writer, request)
    resp := writer.Result()
    body, _ := io.ReadAll(resp.Body)

    t.Log(resp.Header["Content-Length"])
    t.Logf("Got body with len: %d byte(s)", len(body))
}

func TestImageBadResponse(t *testing.T) {
    request := httptest.NewRequest("GET", "/images/files/not-exists-fill-450x800.jpg", nil)
    writer := httptest.NewRecorder()

    handlers.ImageHandler(writer, request)
    resp := writer.Result()

    assert.Equal(t, resp.StatusCode, 404)
}
