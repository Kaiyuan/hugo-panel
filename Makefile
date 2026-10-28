SHELL := /bin/sh
BINARY := bin/hugo-panel
VERSION ?= 0.0.2
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
BUILD_TIME ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

.PHONY: build run test vet check docker clean

build:
	CGO_ENABLED=1 go build -trimpath \
		-ldflags "-s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.buildTime=$(BUILD_TIME)" \
		-o $(BINARY) ./cmd/hugo-panel

run:
	CGO_ENABLED=1 DATA_DIR=./data HUGO_DIR=./hugo go run ./cmd/hugo-panel

test:
	CGO_ENABLED=1 go test ./...

vet:
	CGO_ENABLED=1 go vet ./...

check: test vet

docker:
	docker build --build-arg VERSION=$(VERSION) --build-arg COMMIT=$(COMMIT) --build-arg BUILD_TIME=$(BUILD_TIME) -t kaiyuan/hugo-panel:$(VERSION) .

clean:
	rm -rf bin
