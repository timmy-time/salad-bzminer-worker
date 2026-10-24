IMAGE ?= salad-bzminer-worker:local
VERSION ?= dev
REVISION ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
BUILD_DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

.PHONY: fmt vet test image smoke clean

fmt:
	gofmt -w cmd internal tests

vet:
	go vet ./...

test:
	go test -race ./...

image:
	docker build \
		--build-arg VERSION=$(VERSION) \
		--build-arg REVISION=$(REVISION) \
		--build-arg BUILD_DATE=$(BUILD_DATE) \
		-t $(IMAGE) .

smoke:
	IMAGE=$(IMAGE) ./tests/integration/test-image.sh

clean:
	rm -rf bin tests/integration/.tmp
