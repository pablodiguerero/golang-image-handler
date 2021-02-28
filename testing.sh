#!/usr/bin/env bash
for FILE in $(ls "$1"); do
  exten="${FILE#*.}"
  basename="${FILE%.*}"
  img1="$basename.fit-100x100.$exten"
  img2="$basename.fit-250x250.$exten"
  img3="$basename.fill-100x100.$exten"
  img4="$basename.fill-250x250.$exten"

  curl --silent --write-out '[%{time_total}sec]: %{http_code} -> %{size_download}\n' -o /dev/null "http://localhost:8000/images/sample/$img1"
  curl --silent --write-out '[%{time_total}sec]: %{http_code} -> %{size_download}\n' -o /dev/null "http://localhost:8000/images/sample/$img2"
  curl --silent --write-out '[%{time_total}sec]: %{http_code} -> %{size_download}\n' -o /dev/null "http://localhost:8000/images/sample/$img3"
  curl --silent --write-out '[%{time_total}sec]: %{http_code} -> %{size_download}\n' -o /dev/null "http://localhost:8000/images/sample/$img4"
  curl --silent --write-out '[%{time_total}sec]: %{http_code} -> %{size_download}\n' -o /dev/null -H "Accept: image/avif,image/webp,image/apng"  "http://localhost:8000/images/sample/$img1"
  curl --silent --write-out '[%{time_total}sec]: %{http_code} -> %{size_download}\n' -o /dev/null -H "Accept: image/avif,image/webp,image/apng"  "http://localhost:8000/images/sample/$img2"
  curl --silent --write-out '[%{time_total}sec]: %{http_code} -> %{size_download}\n' -o /dev/null -H "Accept: image/avif,image/webp,image/apng"  "http://localhost:8000/images/sample/$img3"
  curl --silent --write-out '[%{time_total}sec]: %{http_code} -> %{size_download}\n' -o /dev/null -H "Accept: image/avif,image/webp,image/apng"  "http://localhost:8000/images/sample/$img4"
done
