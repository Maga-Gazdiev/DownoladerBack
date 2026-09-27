FROM golang:1.25.5-bookworm AS build
WORKDIR /src
COPY go.mod ./
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /out/downloader ./cmd/app

FROM node:22-bookworm-slim AS pot-build
ARG BGUTIL_VERSION=2.0.0
RUN apt-get update && apt-get install -y --no-install-recommends git ca-certificates \
    && rm -rf /var/lib/apt/lists/*
RUN git clone --depth 1 --branch "$BGUTIL_VERSION" https://github.com/Brainicism/bgutil-ytdlp-pot-provider.git /opt/bgutil
WORKDIR /opt/bgutil/server
RUN npm ci --no-audit --no-fund && npx tsc && npm prune --omit=dev --no-audit --no-fund
RUN node build/generate_once.js --version

FROM python:3.12-slim-bookworm
ARG BGUTIL_VERSION=2.0.0
RUN apt-get update && apt-get install -y --no-install-recommends ffmpeg ca-certificates \
    && rm -rf /var/lib/apt/lists/* \
    && pip install --no-cache-dir "yt-dlp[default,curl-cffi]" "bgutil-ytdlp-pot-provider==${BGUTIL_VERSION}" \
    && useradd --create-home --uid 10001 downloader
COPY --from=pot-build /usr/local/bin/node /usr/local/bin/node
COPY --from=pot-build /opt/bgutil/server /opt/bgutil/server
COPY --from=build /out/downloader /usr/local/bin/downloader
USER downloader
WORKDIR /home/downloader
ENV YOUTUBE_PO_TOKEN_HOME=/opt/bgutil/server GOGC=50
ENTRYPOINT ["downloader"]
CMD ["serve"]
