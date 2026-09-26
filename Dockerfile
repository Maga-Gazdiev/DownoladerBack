FROM golang:1.25.5-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /out/downloader ./cmd/app

FROM node:22-bookworm-slim AS node-runtime

FROM python:3.12-slim-bookworm
RUN apt-get update && apt-get install -y --no-install-recommends ffmpeg ca-certificates \
    && rm -rf /var/lib/apt/lists/* \
    && pip install --no-cache-dir "yt-dlp[default]" gallery-dl \
    && useradd --create-home --uid 10001 downloader \
    && mkdir -p /data/downloads && chown downloader:downloader /data/downloads
COPY --from=node-runtime /usr/local/bin/node /usr/local/bin/node
COPY --from=build /out/downloader /usr/local/bin/downloader
USER downloader
WORKDIR /data
ENV DOWNLOAD_DIR=/data/downloads YOUTUBE_COOKIES_BROWSER="" GOSTREAMPULLER_NO_AUTO_INSTALL=1
ENTRYPOINT ["downloader"]
CMD ["serve"]
