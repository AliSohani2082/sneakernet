ARCHES  := amd64 arm64 386 armv7
DIST    := dist/v2ray-kit
LDFLAGS := -s -w

.PHONY: all build test fetch bundle encrypt-servers clean

all: build

build:
	@for a in $(ARCHES); do \
		goarch=$$a; goarm=; \
		if [ $$a = armv7 ]; then goarch=arm; goarm=7; fi; \
		echo "build linux/$$a"; \
		CGO_ENABLED=0 GOOS=linux GOARCH=$$goarch GOARM=$$goarm \
			go build -trimpath -ldflags '$(LDFLAGS)' -o build/$$a/v2kit ./cmd/v2kit || exit 1; \
	done

test:
	go test ./...

# Needs internet. Downloads pinned artifacts from versions.lock into .cache/.
fetch:
	sh scripts/fetch-deps.sh

bundle: build
	sh scripts/make-bundle.sh $(DIST)

encrypt-servers:
	age -p -o config/servers.age config/servers.txt

clean:
	rm -rf build dist
