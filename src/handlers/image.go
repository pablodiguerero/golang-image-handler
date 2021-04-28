package handlers

import (
	"errors"
	"log"
	"net/http"
	"os"
	"regexp"

	"image.it-lab.su/models"
)

const bufferSize = 64

func ImageHandler(writer http.ResponseWriter, request *http.Request) {
    defer func() {
		if err := recover(); err != nil {
			log.Println(err)

            writer.WriteHeader(http.StatusNotFound)
            writer.Write([]byte("Error while image handling"))
		}
	}()

    rePath := regexp.MustCompile(`/images/(.+)$`)
    regexpPathResult := rePath.FindStringSubmatch(request.URL.String())

    reWebp := regexp.MustCompile(`image/(webp|WEBP)`)
    regexpWebpResult := reWebp.FindStringSubmatch(request.Header.Get("Accept"))

    webpSupported := len(regexpWebpResult) > 1

    if len(regexpPathResult) < 2 {
        panic(errors.New("WrongURL"))
    }

    var buf []byte

    imageFormat := "jpeg"
    image := models.LoadImage(regexpPathResult[1])
    imagePath := image.Parser.GetCachedJpgPath()

    if (webpSupported) {
        imageFormat = "webp"
        imagePath = image.Parser.GetCachedWebpPath()
    }

    if _, err := os.Stat(imagePath); os.IsNotExist(err) {
        imagePath = image.MakeCachedImage(imageFormat)
    }

    cachedFile, _ := os.Open(imagePath)
    buf = make([]byte, bufferSize)
    reads, err := cachedFile.Read(buf)

    for reads > 0 && err == nil {
        if _, err := writer.Write(buf); err != nil {
            log.Fatal("Failed to write to response. Error: ", err.Error())
        }

        reads, err = cachedFile.Read(buf)
    }

}
