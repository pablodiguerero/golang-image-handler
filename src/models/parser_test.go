package models_test

import (
    "testing"
    "time"

    "github.com/stretchr/testify/assert"
    "image.it-lab.su/models"
)

type TestCase struct {
    fileUrl   string
    wepSupprt bool
}

type TestCases []TestCase

func TestParser(t *testing.T) {
    cases := TestCases{
        TestCase{fileUrl: "files/foto_Demidova_S.Yu.-fill-800x600.jpg", wepSupprt: false},
        TestCase{fileUrl: "files/realty-fill-100x100-q60.jpg", wepSupprt: false},
        TestCase{fileUrl: "files/realty-fill-100x100-q90.jpg", wepSupprt: false},
        TestCase{fileUrl: "files/realty-fill-100x100.jpg", wepSupprt: true},
        TestCase{fileUrl: "files/realty-fill-100x100-q60.jpg", wepSupprt: true},
        TestCase{fileUrl: "files/realty-fill-100x100-q90.jpg", wepSupprt: true},
    }

    for i := 0; i < len(cases); i++ {
        start := time.Now()

        parserJpg, errJpg := models.ParsePath(cases[i].fileUrl, cases[i].wepSupprt)
        assert.Nil(t, errJpg)

        jpgPath := parserJpg.GetFullSizePath()
        t.Logf("%s", jpgPath)

        jpgPathCached := parserJpg.GetCachePath()
        t.Logf("%s", jpgPathCached)

        elapsed := time.Since(start)
        t.Logf("Binomial took %s", elapsed)
    }
}
