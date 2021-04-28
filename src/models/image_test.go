package models_test

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"image.it-lab.su/models"
)

func TestImageHandlingWoCache(t *testing.T) {
	start := time.Now()

	image, err := models.LoadImage("files/realty-fill-100x100.jpg")
	assert.Nil(t, err)

	path := image.MakeCachedImage()
	assert.Nil(t, err)

	_, err = os.Stat(path)
	assert.Nil(t, err)

	elapsed := time.Since(start)
	t.Logf("Binomial took %s", elapsed)
}

func TestImageHandlingWithCache(t *testing.T) {
	start := time.Now()

	image, err := models.LoadImage("files/realty-fill-720x900.jpg")
	assert.Nil(t, err)

	path := image.Parser.GetCachedJpgPath()
	if stat, err := os.Stat(path); os.IsNotExist(err) {
		t.Log("File does not exists, creating")
		_ = image.MakeCachedImage()
	} else {
		t.Logf("File size is %d kb", stat.Size() / 1024)
	}

	assert.Nil(t, err)

	elapsed := time.Since(start)
	t.Logf("Binomial took %s", elapsed)
}

func TestWebpImage(t *testing.T) {
    start := time.Now()

    image, err := models.LoadImage("files/cover-fill-720x900.webp")
    assert.Nil(t, err)

    path := image.Parser.GetCachedWebpPath()
    if stat, err := os.Stat(path); os.IsNotExist(err) {
		t.Log("File does not exists, creating")
		_ = image.MakeCachedImage()
	} else {
		t.Logf("File size is %d kb", stat.Size() / 1024)
	}

	assert.Nil(t, err)

	elapsed := time.Since(start)
	t.Logf("Binomial took %s", elapsed)
}
