GO ?= go
GOFMT ?= gofmt
DOCKER_GO_IMAGE ?= golang:1.24
VERSION ?= 0.0.0-dev

.PHONY: build build-bridge build-node release-assets fmt test vet cover run run-bridge run-node \
	build-docker build-bridge-docker build-node-docker release-assets-docker fmt-docker test-docker vet-docker cover-docker

build:
	$(GO) build -ldflags "-X main.version=$(VERSION)" -o bin/moltnet ./cmd/moltnet

build-bridge:
	$(GO) build -ldflags "-X main.version=$(VERSION)" -o bin/moltnet-bridge ./cmd/moltnet-bridge

build-node:
	$(GO) build -ldflags "-X main.version=$(VERSION)" -o bin/moltnet-node ./cmd/moltnet-node

release-assets:
	rm -rf dist/release
	mkdir -p dist/release
	for target in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64; do \
		os=$${target%/*}; \
		arch=$${target#*/}; \
		workdir=$$(mktemp -d); \
		for cmd in moltnet; do \
			GOOS=$$os GOARCH=$$arch CGO_ENABLED=0 $(GO) build -trimpath -ldflags "-s -w -X main.version=$(VERSION)" -o $$workdir/$$cmd ./cmd/$$cmd || exit 1; \
		done; \
		tar -C $$workdir -czf dist/release/moltnet_$${os}_$${arch}.tar.gz moltnet; \
		rm -rf $$workdir; \
	done

fmt:
	$(GOFMT) -w $(shell find . -name '*.go' | sort)

test:
	$(GO) test ./...

vet:
	$(GO) vet ./...

cover:
	$(GO) test ./... -coverprofile=coverage.out -covermode=atomic
	$(GO) tool cover -func=coverage.out

run:
	$(GO) run ./cmd/moltnet start

run-bridge:
	$(GO) run ./cmd/moltnet bridge ./bridge.json

run-node:
	$(GO) run ./cmd/moltnet node start

build-docker:
	docker run --rm -v "$(PWD):/app" -w /app $(DOCKER_GO_IMAGE) sh -lc '/usr/local/go/bin/go build -ldflags "-X main.version=$(VERSION)" -o bin/moltnet ./cmd/moltnet'

build-bridge-docker:
	docker run --rm -v "$(PWD):/app" -w /app $(DOCKER_GO_IMAGE) sh -lc '/usr/local/go/bin/go build -ldflags "-X main.version=$(VERSION)" -o bin/moltnet-bridge ./cmd/moltnet-bridge'

build-node-docker:
	docker run --rm -v "$(PWD):/app" -w /app $(DOCKER_GO_IMAGE) sh -lc '/usr/local/go/bin/go build -ldflags "-X main.version=$(VERSION)" -o bin/moltnet-node ./cmd/moltnet-node'

release-assets-docker:
	docker run --rm -v "$(PWD):/app" -w /app $(DOCKER_GO_IMAGE) sh -lc '\
		rm -rf dist/release && mkdir -p dist/release && \
		for target in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64; do \
			os=$${target%/*}; \
			arch=$${target#*/}; \
			workdir=$$(mktemp -d); \
			for cmd in moltnet; do \
				GOOS=$$os GOARCH=$$arch CGO_ENABLED=0 /usr/local/go/bin/go build -trimpath -ldflags "-s -w -X main.version=$(VERSION)" -o $$workdir/$$cmd ./cmd/$$cmd || exit 1; \
			done && \
			tar -C $$workdir -czf dist/release/moltnet_$${os}_$${arch}.tar.gz moltnet && \
			rm -rf $$workdir; \
		done'

fmt-docker:
	docker run --rm -v "$(PWD):/app" -w /app $(DOCKER_GO_IMAGE) sh -lc '/usr/local/go/bin/gofmt -w $$(find . -name "*.go" | sort)'

test-docker:
	docker run --rm -v "$(PWD):/app" -w /app $(DOCKER_GO_IMAGE) sh -lc '/usr/local/go/bin/go test ./...'

vet-docker:
	docker run --rm -v "$(PWD):/app" -w /app $(DOCKER_GO_IMAGE) sh -lc '/usr/local/go/bin/go vet ./...'

cover-docker:
	docker run --rm -v "$(PWD):/app" -w /app $(DOCKER_GO_IMAGE) sh -lc '/usr/local/go/bin/go test ./... -coverprofile=coverage.out -covermode=atomic && /usr/local/go/bin/go tool cover -func=coverage.out'
