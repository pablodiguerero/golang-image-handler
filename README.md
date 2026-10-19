# Image handler

The service creates and caches responsive image variants while preserving the URL format used by existing sites. It uses libvips, which decodes and resizes images in a streaming pipeline instead of holding the full source image in Go memory.

## Existing URLs remain valid

```
/images/<directory>/<basename>[.|-]fill-<width>x<height>[-q<1..100>].<jpg|jpeg|png|webp|avif|gif|tif|tiff>
/images/<directory>/<basename>[.|-]fit-<width>x<height>[-q<1..100>].<jpg|jpeg|png|webp|avif|gif|tif|tiff>
/images/<directory>/<basename>[.|-]fitstrict-<width>x<height>[-q<1..100>].<jpg|jpeg|png|webp|avif|gif|tif|tiff>
/images/<directory>/<basename>[.|-]shrink-<width>x<height>[-q<1..100>].<jpg|jpeg|png|webp|avif|gif|tif|tiff>
/images/<directory>/<basename>.original.<jpg|jpeg|png|webp|avif|gif|tif|tiff>
```

Both the historical dot separator (`photo.fill-200x200.jpg`) and the deployed hyphen separator (`photo-fill-200x200.jpg`) work. `fill` crops from the centre, `fit` preserves the aspect ratio within the requested bounds, and `fitstrict` adds white padding to return the exact requested dimensions. URLs without `-q` use quality 100 unless `APP_DEFAULT_QUALITY` sets another value.

`shrink` fits the image inside the box like `fit` but never upscales: a source smaller than the box keeps its size. For `shrink` only, one side may be `0`, meaning unbounded on that side (`shrink-1920x0` limits the width only); both sides `0` is invalid. The other operations require both sides to be at least 1.

`original` is retained for existing galleries and image links. Basenames may contain Unicode characters, including the Cyrillic product names already stored by deployed sites.

The extension identifies the source file. Sources may be `jpg`, `jpeg`, `png`, `webp`, `avif`, `gif`, `tif` or `tiff` (case-insensitive); an animated GIF yields its first frame. JPEG output is flattened onto a white background, so transparent sources no longer turn black; WebP and AVIF keep their alpha channel. Response format is negotiated from `Accept`: AVIF takes precedence, then WebP, then JPEG. This lets sites keep all their current URLs while browsers get a modern variant where supported. The response sets `Vary: Accept` and a variant-specific ETag.

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
| `APP_MAX_SOURCE_PIXELS` | `40000000` | Maximum decoded pixel count for an `original` variant. |
| `APP_MAX_OUTPUT_BYTES` | `52428800` | Maximum generated variant size. |
| `APP_MAX_CONCURRENT_TRANSFORMS` | `2` | Process-wide transform limit. |
| `APP_TRANSFORM_TIMEOUT_SECONDS` | `60` | Per-image transform deadline. |
| `APP_BROWSER_CACHE_TTL_SECONDS` | `3600` | Browser and CDN HTTP cache TTL. |
| `APP_CACHE_TTL_SECONDS` | `604800` | Delete generated variants older than this age. |
| `APP_CACHE_CLEANUP_INTERVAL_SECONDS` | `86400` | Run generated-cache cleanup once per day. |
| `APP_CACHE_MAX_BYTES` | `10737418240` | Evict oldest generated variants after this size. |
| `APP_ALLOWED_SIZES` | unset | Comma-separated allowlist of variants, for example `fill-260x260,shrink-500x700,shrink-1920x0,original`. Tokens are `<operation>-<W>x<H>` or `original`; quality is not part of a token. Unset or empty allows everything. |
| `APP_DEFAULT_QUALITY` | `100` | Quality (1..100) for URLs without `-q`. An invalid value stops the service at startup. |
| `APP_ADMIN_TOKEN` or `APP_ADMIN_TOKEN_FILE` | unset | Enables the protected cache administration API. |
| `APP_ADMIN_MAX_PATHS` | `500` | Maximum paths in one cache administration request. |
| `APP_ADMIN_WARM_WORKERS` | transform limit | Concurrent warm tasks; transform concurrency remains capped independently. |

## Restricting variants

When `APP_ALLOWED_SIZES` is set, a request for any other variant returns 404 without doing any work, and the cache administration API reports such paths as `invalid image path`. The hyphen form is used in the list whichever separator the URL uses (`photo.fill-260x260.jpg` matches `fill-260x260`). An invalid token stops the service at startup with a message naming the variable.

## Cache administration API

Set `APP_ADMIN_TOKEN_FILE` to a container secret. The API is disabled when no token is configured. It accepts only authenticated `POST` requests with `Authorization: Bearer <token>` and is intended for an internal network.

`POST /admin/cache/keys` accepts a JSON body such as:

```json
{"paths":["/images/media/item_images/racquet.fitstrict-250x250.jpg"],"formats":["jpg","webp","avif"]}
```

It returns the versioned cache key and current presence for every requested variant. `POST /admin/cache/warm` uses the same body and materializes missing variants. It shares the normal transform limit, so a warm-up cannot exceed the handler's configured CPU and memory budget.

The handler incorporates source size and modification time into every cache key. It writes via a private temporary directory and atomic rename, so incomplete files are never served. Concurrent requests for the same missing key wait for one transform instead of creating duplicate work. A lightweight Go goroutine runs on startup and then once every 24 hours to remove generated variants older than seven days; it also evicts the oldest variants when the generated cache exceeds 10 GiB.

## Run locally

```sh
docker compose up --build
curl -H 'Accept: image/avif,image/webp' http://localhost:8000/images/sample/photo-fit-400x300.jpg -o photo
```

The Compose configuration enables `X-Accel-Redirect`: after a cache miss the app generates the result, then Nginx delivers it directly from `/cache` using `sendfile`. Nginx also caches successful negotiated responses for one hour, revalidates variants separately by `Accept`, and its built-in cache manager clears inactive entries after seven days or at 5 GiB. Disable direct static delivery by removing `APP_ACCEL_REDIRECT_PREFIX`.

## CI and images

GitHub Actions runs tests for pull requests and `master`, then publishes Git tags and releases to `ghcr.io/<owner>/<repository>:<tag>` and `:latest`. Publishing needs no personal token; the workflow grants `packages: write` to its publish job and uses `GITHUB_TOKEN`.
