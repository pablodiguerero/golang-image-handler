package main

import (
	"fmt"
	"log"
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
		re := regexp.MustCompile(`^/images/(.+)/(.+?).([a-z0-9\\-]+).(png|jpg|jpeg)$`)
		response := re.FindStringSubmatch(string(ctx.Path()))

		if len(response) == 0 {
			ctx.Error("Url not found", 404)
			return
		}

		image, err := common.GetImage(response[1], response[2], response[3], response[4])

		if err != nil {
			ctx.Error(fmt.Sprintf("Server error: %q", err.Error()), 500)
			return
		}

		if loadCache := image.LoadCache(); loadCache {
			ctx.SetContentType("image/jpeg")
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

		ctx.SetContentType("image/jpeg")
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
