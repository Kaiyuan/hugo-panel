# syntax=docker/dockerfile:1

FROM golang:1.24-alpine AS build

ARG VERSION=0.0.3
ARG COMMIT=none
ARG BUILD_TIME=unknown
ARG GOPROXY=https://goproxy.cn,direct

ENV GOPROXY=${GOPROXY}
# The SQLite driver uses cgo, so the panel is linked against musl here and the
# resulting binary runs on Alpine without any glibc compatibility layer.
RUN apk add --no-cache ca-certificates gcc musl-dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
COPY web ./web
RUN CGO_ENABLED=1 go build -trimpath \
    -ldflags "-s -w -X main.version=${VERSION} -X main.commit=${COMMIT} -X main.buildTime=${BUILD_TIME}" \
    -o /out/hugo-panel ./cmd/hugo-panel

# The pinned official Hugo extended binary is linked against glibc. Alpine's
# gcompat provides the glibc ABI and libstdc++ provides the C++ runtime needed
# by LibSass, so the extended edition (SCSS/Sass) works without switching the
# whole image back to Debian.
FROM alpine:3.24

ARG HUGO_VERSION=0.150.1

RUN set -eux; \
    apk add --no-cache \
      ca-certificates git go gcompat libstdc++ su-exec tzdata wget; \
    case "$(apk --print-arch)" in \
      x86_64) hugo_arch=amd64 ;; \
      aarch64) hugo_arch=arm64 ;; \
      *) echo "unsupported architecture: $(apk --print-arch)" >&2; exit 1 ;; \
    esac; \
    wget -q --tries=3 --timeout=60 -O /tmp/hugo.tar.gz "https://github.com/gohugoio/hugo/releases/download/v${HUGO_VERSION}/hugo_extended_${HUGO_VERSION}_linux-${hugo_arch}.tar.gz"; \
    tar -xzf /tmp/hugo.tar.gz -C /usr/local/bin hugo; \
    rm -f /tmp/hugo.tar.gz; \
    hugo version; \
    addgroup -g 1000 hugo; \
    adduser -u 1000 -D -H -h /app -s /sbin/nologin -G hugo hugo; \
    mkdir -p /app/hugo-panel/data /app/hugo; \
    chown -R hugo:hugo /app

WORKDIR /app/hugo-panel
COPY --from=build /out/hugo-panel /usr/local/bin/hugo-panel
COPY docker/entrypoint.sh /usr/local/bin/entrypoint.sh
RUN chmod 0755 /usr/local/bin/entrypoint.sh

ENV PORT=8080 \
    DATA_DIR=/app/hugo-panel/data \
    HUGO_DIR=/app/hugo \
    HUGO_BINARY=/usr/local/bin/hugo \
    HOME=/app \
    GOMODCACHE=/app/hugo-panel/data/go-mod-cache \
    GOCACHE=/app/hugo-panel/data/go-cache \
    HUGO_CACHEDIR=/app/hugo-panel/data/hugo-cache \
    COOKIE_SECURE=auto

VOLUME ["/app/hugo-panel/data", "/app/hugo"]
EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=3 \
  CMD wget -qO- http://127.0.0.1:8080/api/health >/dev/null || exit 1

ENTRYPOINT ["/usr/local/bin/entrypoint.sh"]
CMD ["hugo-panel"]
