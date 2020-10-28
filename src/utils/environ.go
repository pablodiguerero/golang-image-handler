package utils

import (
	"log"
	"os"
	"strconv"
)

var defaultPort int = 8001
var imageRoot string

func init() {
	imageRoot = os.Getenv("APP_IMAGE_ROOT")
	if len(imageRoot) == 0 {
		log.Panicln("APP_IMAGE_ROOT is undefined")
	}
}

// GetAddress - return application port
func GetAddress() string {

	if _, err := strconv.Atoi(os.Getenv("APP_PORT")); err != nil {
		return ":" + strconv.Itoa(defaultPort)
	}

	return ":" + os.Getenv("APP_PORT")
}

// GetImageCache - return image common folder
func GetImageCache() string {
	return os.Getenv("APP_IMAGE_CACHE")
}

// GetImageRoot - return image common folder
func GetImageRoot() string {
	return imageRoot
}
