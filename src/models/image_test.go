package models_test

import (
    "testing"
    "time"

    "github.com/stretchr/testify/assert"
    "image.it-lab.su/models"
)

func TestImageConveter(t *testing.T) {
    cases := TestCases{
        TestCase{fileUrl: "files/realty-fill-1000x1000.jpg", wepSupprt: false},
        TestCase{fileUrl: "files/realty-fill-800x800-q60.jpg", wepSupprt: false},
        TestCase{fileUrl: "files/realty-fill-800x800-q90.jpg", wepSupprt: false},
        TestCase{fileUrl: "files/realty-fill-1000x1000.jpg", wepSupprt: true},
        TestCase{fileUrl: "files/realty-fill-800x800-q60.jpg", wepSupprt: true},
        TestCase{fileUrl: "files/realty-fill-800x800-q90.jpg", wepSupprt: true},
    }

    for i := 0; i < len(cases); i++ {
        start := time.Now()

        image := models.LoadImage(cases[i].fileUrl, cases[i].wepSupprt)
        file, err := image.LoadCached()

        if err != nil {
            path, _ := image.MakeCachedImage()
            t.Log(path)
        } else {
            t.Log(file)
        }

        elapsed := time.Since(start)
        t.Logf("Binomial took %s", elapsed)
    }
}

func TestImage(t *testing.T) {
    defer func() {
        if err := recover(); err != nil {
            t.Log(err)
            assert.NotNil(t, err)
        }
    }()

    /**
     * Testing jpg / png images
     */
    start := time.Now()

    models.LoadImage("files/realty-fill-100x100.jpg")

    elapsed := time.Since(start)
    t.Logf("Binomial took %s", elapsed)

    /**
     * Testing webp support
     */
    start = time.Now()
    models.LoadImage("files/fake-realty-fill-100x100.jpg", true)

    elapsed = time.Since(start)
    t.Logf("Binomial took %s", elapsed)
}
