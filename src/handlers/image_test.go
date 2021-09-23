package handlers_test

import (
    "io"
    "net/http/httptest"
    "testing"

    "image.it-lab.su/handlers"
)

type TestCase struct {
    url       string
    wepSupprt bool
}

type TestCases []TestCase

func TestImageResponse(t *testing.T) {
    cases := TestCases{
        TestCase{url: "/images/files/realty-fill-1000x1000.jpg", wepSupprt: false},
        TestCase{url: "/images/files/realty-fill-1000x1000-q90.jpg", wepSupprt: false},
        TestCase{url: "/images/files/realty-fit-800x800.jpg", wepSupprt: false},
        TestCase{url: "/images/files/realty-fit-800x800-q90.jpg", wepSupprt: false},
        TestCase{url: "/images/files/realty-fitstrict-800x800.jpg", wepSupprt: false},
        TestCase{url: "/images/files/realty-fitstrict-800x800-q90.jpg", wepSupprt: false},
        TestCase{url: "/images/files/realty-fill-1000x1000.jpg", wepSupprt: false},
        TestCase{url: "/images/files/realty-fill-1000x1000-q90.jpg", wepSupprt: true},
        TestCase{url: "/images/files/realty-fit-800x800.jpg", wepSupprt: true},
        TestCase{url: "/images/files/realty-fit-800x800-q90.jpg", wepSupprt: true},
        TestCase{url: "/images/files/realty-fitstrict-800x800.jpg", wepSupprt: true},
        TestCase{url: "/images/files/realty-fitstrict-800x800-q90.jpg", wepSupprt: true},
    }

    for i := 0; i < len(cases); i++ {
        request := httptest.NewRequest("GET", cases[i].url, nil)

        if cases[i].wepSupprt {
            request.Header.Add("Accept", "image/WeBp")
        }

        writer := httptest.NewRecorder()

        handlers.ImageHandler(writer, request)
        resp := writer.Result()
        body, _ := io.ReadAll(resp.Body)

        t.Log(resp.Header["Content-Length"])
        t.Logf("Got body with len: %d byte(s)", len(body))
    }
}
