# syntax=docker/dockerfile:1

FROM golang:1.24-trixie AS build

ARG VERSION=dev
ARG COMMIT=none
ARG BUILD_TIME=unknown
ARG GOPROXY=https://goproxy.cn,direct

ENV GOPROXY=${GOPROXY}
RUN apt-get update && apt-get install -y --no-install-recommends gcc libc6-dev ca-certificates \
    && rm -rf /var/lib/apt/lists/*
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
COPY web ./web
RUN CGO_ENABLED=1 go build -trimpath \
    -ldflags "-s -w -X main.version=${VERSION} -X main.commit=${COMMIT} -X main.buildTime=${BUILD_TIME}" \
    -o /out/hugo-panel ./cmd/hugo-panel

# The official Hugo extended binaries are linked against glibc, so the runtime
# stage must use a glibc-based image (Alpine/musl cannot execute them).
FROM debian:trixie-slim

ARG HUGO_VERSION=0.150.1

RUN set -eux; \
    apt-get update; \
    apt-get install -y --no-install-recommends \
      ca-certificates git golang-go gosu libgcc-s1 libstdc++6 tzdata wget; \
    rm -rf /var/lib/apt/lists/*; \
    case "$(dpkg --print-architecture)" in \
      amd64) hugo_arch=amd64 ;; \
      arm64) hugo_arch=arm64 ;; \
      *) echo "unsupported architecture: $(dpkg --print-architecture)" >&2; exit 1 ;; \
    esac; \
    wget -q --tries=3 --timeout=60 -O /tmp/hugo.tar.gz "https://github.com/gohugoio/hugo/releases/download/v${HUGO_VERSION}/hugo_extended_${HUGO_VERSION}_linux-${hugo_arch}.tar.gz"; \
    tar -xzf /tmp/hugo.tar.gz -C /usr/local/bin hugo; \
    rm -f /tmp/hugo.tar.gz; \
    hugo version; \
    groupadd --gid 1000 hugo; \
    useradd --uid 1000 --gid 1000 --home-dir /app --create-home --shell /usr/sbin/nologin hugo; \
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
