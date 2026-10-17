FROM golang:1.23-alpine3.20 AS builder

WORKDIR /app
COPY src/go.mod ./
RUN go mod download
COPY src/ ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /image-handler .

FROM alpine:3.20

RUN apk add --no-cache ca-certificates vips vips-heif vips-tools \
    && mkdir -p /cache

COPY --from=builder /image-handler /usr/local/bin/image-handler
ENV APP_PORT=80 \
    APP_IMAGE_CACHE=/cache \
    APP_MAX_CONCURRENT_TRANSFORMS=2 \
    VIPS_CONCURRENCY=1
EXPOSE 80
HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 CMD wget -qO- http://127.0.0.1:80/health-check/ >/dev/null || exit 1
ENTRYPOINT ["/usr/local/bin/image-handler"]
