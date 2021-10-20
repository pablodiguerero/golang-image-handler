package handlers

import (
    "errors"
    "log"
    "net/http"
    "os"
    "regexp"

    ua "github.com/mileusna/useragent"

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

    reWebp := regexp.MustCompile(`(?i)image/(webp)`)
    regexpWebpResult := reWebp.FindStringSubmatch(request.Header.Get("Accept"))

    userAgent := ua.Parse(request.Header.Get("User-Agent"))

    webpSupported := os.Getenv("APP_ALLOW_WEBP") == "1" && !userAgent.IsSafari() && len(regexpWebpResult) > 1

    if len(regexpPathResult) < 2 {
        panic(errors.New("WrongURL"))
    }

    image := models.LoadImage(regexpPathResult[1], webpSupported)
    imagePath := image.Parser.GetCachePath()

    if _, err := os.Stat(imagePath); os.IsNotExist(err) {
        var cacheErr error
        imagePath, cacheErr = image.MakeCachedImage()

        if cacheErr != nil {
            log.Fatal("Failed to write cache: ", cacheErr.Error())
            panic(cacheErr)
        }
    }

    if image.Parser.AllowedWebp {
        writer.Header().Add("Content-Type", "image/webp")
    } else {
        writer.Header().Add("Content-Type", "image/jpeg")
    }

    cachedFile, _ := os.Open(imagePath)
    buf := make([]byte, bufferSize)
    reads, err := cachedFile.Read(buf)

    for reads > 0 && err == nil {
        if _, err := writer.Write(buf); err != nil {
            log.Fatal("Failed to write to response. Error: ", err.Error())
        }

        reads, err = cachedFile.Read(buf)
    }
}
