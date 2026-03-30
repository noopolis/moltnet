GO ?= go
GOFMT ?= gofmt
DOCKER_GO_IMAGE ?= golang:1.24

.PHONY: build build-bridge fmt test cover run run-bridge \
	build-docker build-bridge-docker fmt-docker test-docker cover-docker

build:
	$(GO) build -o bin/moltnet ./cmd/moltnet

build-bridge:
	$(GO) build -o bin/moltnet-bridge ./cmd/moltnet-bridge

fmt:
	$(GOFMT) -w $(shell find . -name '*.go' | sort)

test:
	$(GO) test ./...

cover:
	$(GO) test ./... -coverprofile=coverage.out -covermode=atomic
	$(GO) tool cover -func=coverage.out

run:
	$(GO) run ./cmd/moltnet

run-bridge:
	$(GO) run ./cmd/moltnet-bridge ./bridge.json

build-docker:
	docker run --rm -v "$(PWD):/app" -w /app $(DOCKER_GO_IMAGE) sh -lc '/usr/local/go/bin/go build -o bin/moltnet ./cmd/moltnet'

build-bridge-docker:
	docker run --rm -v "$(PWD):/app" -w /app $(DOCKER_GO_IMAGE) sh -lc '/usr/local/go/bin/go build -o bin/moltnet-bridge ./cmd/moltnet-bridge'

fmt-docker:
	docker run --rm -v "$(PWD):/app" -w /app $(DOCKER_GO_IMAGE) sh -lc '/usr/local/go/bin/gofmt -w $$(find . -name "*.go" | sort)'

test-docker:
	docker run --rm -v "$(PWD):/app" -w /app $(DOCKER_GO_IMAGE) sh -lc '/usr/local/go/bin/go test ./...'

cover-docker:
	docker run --rm -v "$(PWD):/app" -w /app $(DOCKER_GO_IMAGE) sh -lc '/usr/local/go/bin/go test ./... -coverprofile=coverage.out -covermode=atomic && /usr/local/go/bin/go tool cover -func=coverage.out'
