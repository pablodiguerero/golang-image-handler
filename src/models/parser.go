package models

import (
    "errors"
    "fmt"
    "os"
    "regexp"
    "strconv"
)

type ParseResult struct {
    Path        string
    BaseName    string
    Exten       string
    Width       int
    Height      int
    Handle      string
    AllowedWebp bool
    Quality     int
}

func (result ParseResult) GetFullSizePath() string {
    path := fmt.Sprintf("%s/%s.%s", result.Path, result.BaseName, result.Exten)

    if os.Getenv("APP_IMAGE_ROOT") != "" {
        return fmt.Sprintf("%s/%s", os.Getenv("APP_IMAGE_ROOT"), path)
    }

    return path
}

func (result ParseResult) GetCachePath() string {
    altExten := result.Exten

    if result.AllowedWebp && os.Getenv("APP_ALLOW_WEBP") == "1" {
        altExten = "webp"
    }

    path := fmt.Sprintf("%s/%s-%s-%dx%d-q%d.%s", result.Path, result.BaseName, result.Handle, result.Width, result.Height, result.Quality, altExten)

    if os.Getenv("APP_IMAGE_CACHE") != "" {
        return fmt.Sprintf("%s/%s", os.Getenv("APP_IMAGE_CACHE"), path)
    }

    return path
}

func ParsePath(path string, allowedWebp ...bool) (ParseResult, error) {
    var width int
    var height int
    var quality int
    var err error

    allowedWebpLocal := false

    if len(allowedWebp) > 0 {
        allowedWebpLocal = allowedWebp[0]
    }

    re := regexp.MustCompile(`(?i)(.+)\/([a-z0-9_\-\.]+)\-(fill|fit|fitstrict)\-([0-9]+)x([0-9]+)(?:-q([0-9]+))?\.(jpg|png|jpeg|webp)$`)
    regexpResult := re.FindStringSubmatch(path)

    if len(regexpResult) != 8 {
        return ParseResult{}, errors.New("ErrorWhilePathParse")
    }

    width, _ = strconv.Atoi(regexpResult[4])
    height, _ = strconv.Atoi(regexpResult[5])

    if quality, err = strconv.Atoi(regexpResult[6]); err != nil {
        quality = 100
    }

    return ParseResult{
        Path:        regexpResult[1],
        BaseName:    regexpResult[2],
        Quality:     quality,
        Exten:       regexpResult[7],
        Width:       width,
        Height:      height,
        Handle:      regexpResult[3],
        AllowedWebp: allowedWebpLocal,
    }, nil
}
