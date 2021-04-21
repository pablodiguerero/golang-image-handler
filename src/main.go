package main

import (
	"fmt"
	"log"
	"os"
	"regexp"

	"github.com/valyala/fasthttp"
	"image-handler.it-lab.su/common"
	"image-handler.it-lab.su/utils"
)

type fastHTTPHandler struct{}

func (h *fastHTTPHandler) handle(ctx *fasthttp.RequestCtx) {

	if "/health-check/" == fmt.Sprintf("%s", ctx.Path()) {
		ctx.Write([]byte("Checked"))
		return
	} else {
		pathRe := regexp.MustCompile(`^/images/(.+)/(.+?).([a-z0-9\\-]+).(png|jpg|jpeg)$`)
		headerRed := regexp.MustCompile(`(image\/webp)`);

		response := pathRe.FindStringSubmatch(string(ctx.Path()))
		webpHeader := os.Getenv("APP_DISABLE_WEBP") == "1" && len(headerRed.FindStringSubmatch(string(ctx.Request.Header.Peek("Accept")))) > 0

		if len(response) == 0 {
			ctx.Error("Url not found", 404)
			return
		}

		image, err := common.GetImage(response[1], response[2], response[3], response[4], webpHeader)

		if err != nil {
			ctx.Error(fmt.Sprintf("Server error: %q", err.Error()), 500)
			return
		}

		if loadCache := image.LoadCache(); loadCache {
			if (image.IsAllowedWebp()) {
                ctx.SetContentType("image/webp")
            } else {
                ctx.SetContentType("image/jpeg")
            }

			ctx.Write(image.GetBody())
			return
		}

		if err = image.Handle(); err != nil {
			ctx.Error(fmt.Sprintf("Server error: %q", err.Error()), 500)
			return
		}

		if err = image.Cache(); err != nil {
			fmt.Println(err)
		}

        if (image.IsAllowedWebp()) {
            ctx.SetContentType("image/webp")
        } else {
            ctx.SetContentType("image/jpeg")
        }

		_, err = ctx.Write(image.GetBody())

		if err != nil {
			log.Printf("Server error: %q", err.Error())
		}
	}
}

func main() {
	address := utils.GetAddress()
	serverHandler := &fastHTTPHandler{}

	log.Printf("Start application at port %q", address)

	if err := fasthttp.ListenAndServe(address, serverHandler.handle); err != nil {
		log.Fatalf("Error in ListenAndServe: %s", err)
	}
}
