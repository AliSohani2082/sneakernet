ARCHES  ?= amd64 arm64 386 armv7
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: all build test lint fetch dev-xray bundle clean

all: build

## build: cross-compile the sneakernet binary for every architecture
build:
	@for a in $(ARCHES); do \
		goarch=$$a; goarm=; \
		if [ $$a = armv7 ]; then goarch=arm; goarm=7; fi; \
		echo "build linux/$$a"; \
		CGO_ENABLED=0 GOOS=linux GOARCH=$$goarch GOARM=$$goarm \
			go build -trimpath -ldflags '$(LDFLAGS)' -o build/$$a/sneakernet ./cmd/sneakernet || exit 1; \
	done

## test: unit + offline integration tests (needs `make dev-xray` for the Xray ones)
test:
	go test ./...

lint:
	gofmt -l . | (! grep .)
	go vet ./...
	sh -n bootstrap/install.sh bootstrap/uninstall.sh scripts/*.sh

## fetch: download the pinned Xray releases into .cache/deps (needs internet)
fetch:
	sh scripts/fetch-deps.sh

## dev-xray: unpack the amd64 Xray build that the tests run against
dev-xray: fetch
	mkdir -p .cache/xray-amd64
	unzip -q -o .cache/deps/Xray-linux-64.zip -d .cache/xray-amd64
	chmod +x .cache/xray-amd64/xray

## bundle: assemble dist/sneakernet/ for the Ventoy stick (offline)
bundle: build
	VERSION=$(VERSION) ARCHES="$(ARCHES)" sh scripts/make-bundle.sh dist/sneakernet

clean:
	rm -rf build dist
