# syntax=docker/dockerfile:1

FROM golang:1.24-trixie AS build

ARG VERSION=0.0.3
ARG COMMIT=none
ARG BUILD_TIME=unknown
ARG GOPROXY=https://goproxy.cn,direct

ENV GOPROXY=${GOPROXY}
# The SQLite driver uses cgo, so the panel is linked against glibc and the
# runtime image must be Debian-based (Alpine uses musl and cannot run it).
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

# The official Hugo extended binary and the cgo build of the panel are linked
# against glibc, so use a Debian runtime instead of Alpine.
FROM debian:trixie-slim

# The extended binary is resolved from the latest upstream release at build
# time, so the image does not need a manual version bump for every Hugo
# release. Pass --build-arg HUGO_VERSION=x.y.z to pin a specific version.
ARG HUGO_VERSION=

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
    if [ -z "${HUGO_VERSION}" ]; then \
      HUGO_VERSION="$(wget -qO- --tries=3 --timeout=60 https://api.github.com/repos/gohugoio/hugo/releases/latest \
        | grep -o '"tag_name": *"[^"]*"' | head -n1 | sed 's/.*"v\([^"]*\)"/\1/')"; \
    fi; \
    if [ -z "${HUGO_VERSION}" ]; then \
      echo "could not determine the latest Hugo version" >&2; exit 1; \
    fi; \
    echo "Installing Hugo ${HUGO_VERSION}"; \
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
