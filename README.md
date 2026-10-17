# Image handler

The service creates and caches responsive image variants while preserving the URL format used by existing sites. It uses libvips, which decodes and resizes images in a streaming pipeline instead of holding the full source image in Go memory.

## Existing URLs remain valid

```
/images/<directory>/<basename>[.|-]fill-<width>x<height>[-q<1..100>].<jpg|jpeg|png|webp|avif>
/images/<directory>/<basename>[.|-]fit-<width>x<height>[-q<1..100>].<jpg|jpeg|png|webp|avif>
/images/<directory>/<basename>[.|-]fitstrict-<width>x<height>[-q<1..100>].<jpg|jpeg|png|webp|avif>
```

Both the historical dot separator (`photo.fill-200x200.jpg`) and the deployed hyphen separator (`photo-fill-200x200.jpg`) work. `fill` crops from the centre, `fit` preserves the aspect ratio within the requested bounds, and `fitstrict` adds white padding to return the exact requested dimensions. URLs without `-q` keep the historical quality value of 100.

The extension identifies the source file. Response format is negotiated from `Accept`: AVIF takes precedence, then WebP, then JPEG. This lets sites keep all their current URLs while browsers get a modern variant where supported. The response sets `Vary: Accept` and a variant-specific ETag.

## Environment

| Variable | Default | Purpose |
| --- | --- | --- |
| `APP_PORT` | `80` | HTTP port. |
| `APP_IMAGE_ROOT` | required | Absolute path of source images. |
| `APP_IMAGE_CACHE` | system temp directory | Absolute path of generated images. |
| `APP_ALLOW_AVIF` | `1` | Set to `0` to disable AVIF. |
| `APP_ALLOW_WEBP` | `1` | Set to `0` to disable WebP. |
| `APP_ACCEL_REDIRECT_PREFIX` | unset | Internal Nginx prefix for serving cached files. |
| `APP_MAX_DIMENSION` | `8192` | Maximum requested width or height. |
| `APP_MAX_OUTPUT_PIXELS` | `40000000` | Maximum width × height. |
| `APP_MAX_SOURCE_BYTES` | `52428800` | Maximum source file size. |
| `APP_MAX_CONCURRENT_TRANSFORMS` | `2` | Process-wide transform limit. |
| `APP_TRANSFORM_TIMEOUT_SECONDS` | `60` | Per-image transform deadline. |
| `APP_BROWSER_CACHE_TTL_SECONDS` | `3600` | Browser and CDN HTTP cache TTL. |
| `APP_CACHE_TTL_SECONDS` | `604800` | Delete generated variants older than this age. |
| `APP_CACHE_CLEANUP_INTERVAL_SECONDS` | `86400` | Run generated-cache cleanup once per day. |
| `APP_CACHE_MAX_BYTES` | `10737418240` | Evict oldest generated variants after this size. |

The handler incorporates source size and modification time into every cache key. It writes via a private temporary directory and atomic rename, so incomplete files are never served. Concurrent requests for the same missing key wait for one transform instead of creating duplicate work. A lightweight Go goroutine runs on startup and then once every 24 hours to remove generated variants older than seven days; it also evicts the oldest variants when the generated cache exceeds 10 GiB.

## Run locally

```sh
docker compose up --build
curl -H 'Accept: image/avif,image/webp' http://localhost:8000/images/sample/photo-fit-400x300.jpg -o photo
```

The Compose configuration enables `X-Accel-Redirect`: after a cache miss the app generates the result, then Nginx delivers it directly from `/cache` using `sendfile`. Nginx also caches successful negotiated responses for one hour, revalidates variants separately by `Accept`, and its built-in cache manager clears inactive entries after seven days or at 5 GiB. Disable direct static delivery by removing `APP_ACCEL_REDIRECT_PREFIX`.

## CI and images

GitHub Actions runs tests for pull requests and `master`, then publishes Git tags and releases to `ghcr.io/<owner>/<repository>:<tag>` and `:latest`. Publishing needs no personal token; the workflow grants `packages: write` to its publish job and uses `GITHUB_TOKEN`.
