ARCHES  ?= amd64 arm64 386 armv7
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)
# Mounted Ventoy partition for `make stick` (empty: found automatically)
VENTOY  ?=

.PHONY: all help build test lint fetch dev-xray bundle tarball stick clean

all: build

## help: list the targets
help:
	@echo "Usage: make <target> [ARCHES='amd64 arm64'] [VENTOY=/path/to/Ventoy]"
	@echo
	@sed -n 's/^## //p' $(MAKEFILE_LIST) | column -t -s ':' 2>/dev/null || sed -n 's/^## //p' $(MAKEFILE_LIST)

## build: cross-compile the sneakernet binary for every architecture
build:
	@command -v go >/dev/null || { echo "go not found: install Go 1.26+ (or run: nix develop)" >&2; exit 1; }
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

## lint: gofmt, go vet, sh -n (+ shellcheck when installed)
lint:
	gofmt -l . | (! grep .)
	go vet ./...
	sh -n bootstrap/install.sh bootstrap/uninstall.sh scripts/*.sh
	@# SC1007: `CDPATH= cd` is intentional (unset CDPATH for that one command)
	@if command -v shellcheck >/dev/null; then shellcheck -e SC1007 bootstrap/*.sh scripts/*.sh; else echo "shellcheck not installed, skipped"; fi

## fetch: download the pinned Xray releases into .cache/deps (needs internet)
fetch:
	sh scripts/fetch-deps.sh

## dev-xray: unpack the amd64 Xray build that the tests run against
dev-xray: fetch
	mkdir -p .cache/xray-amd64
	unzip -q -o .cache/deps/Xray-linux-64.zip -d .cache/xray-amd64
	chmod +x .cache/xray-amd64/xray

## bundle: assemble dist/sneakernet/ for the Ventoy stick (offline, after make fetch)
bundle: build
	VERSION=$(VERSION) ARCHES="$(ARCHES)" sh scripts/make-bundle.sh dist/sneakernet

## tarball: dist/sneakernet.tar.gz + .sha256, the release asset scripts/install.sh downloads
tarball: bundle
	tar -czf dist/sneakernet.tar.gz -C dist sneakernet
	cd dist && sha256sum sneakernet.tar.gz > sneakernet.tar.gz.sha256
	@cat dist/sneakernet.tar.gz.sha256

## stick: bundle, then copy it onto the Ventoy stick (keeps servers.txt there)
stick: bundle
	sh scripts/install.sh --from dist/sneakernet $(if $(VENTOY),--to "$(VENTOY)")

## clean: remove build/ and dist/
clean:
	rm -rf build dist
