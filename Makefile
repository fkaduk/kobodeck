GNUARCH ?= $(shell arch)
BINARY  ?= build/readeckobo.$(GNUARCH)

# Embed the version number in the binary.
GFLAGS += -ldflags="-X main.version=$(shell git describe --always --dirty --tags)"

# Pure-Go modernc SQLite backend: no CGO, no C toolchain required.
# Cross-compilation to ARM is therefore straightforward.
CROSS_COMPILE_FLAGS = GOARCH=arm GOOS=linux CGO_ENABLED=0

all: check build tarball

tarball:
	@echo building Kobo tarball
	$(MAKE) build BINARY=build/readeckobo.arm $(CROSS_COMPILE_FLAGS)
	cp build/readeckobo.arm root/usr/local/bin/readeckobo
	touch root/usr
	tar -C root/ -c -z -f build/KoboRoot.tgz etc usr
	rm root/usr/local/bin/readeckobo

build: $(BINARY)

$(BINARY): *.go
	mkdir -p $$(dirname $(BINARY))
	CGO_ENABLED=0 go build $(GFLAGS) -o $@
	strip $@ || true

clean:
	rm -f build/readeckobo.* build/KoboRoot.tgz

check: lint test

lint:
	go vet ./...
	@out=$$(gofmt -s -l .); if [ -n "$$out" ]; then echo "gofmt: these files need formatting:"; echo "$$out"; exit 1; fi

test:
	CGO_ENABLED=0 go test -timeout 120s ./...

sign: check build tarball
	rm -f build/*.asc
	for bin in build/readeckobo.* build/KoboRoot.tgz; do \
		gpg --detach-sign -a "$$bin"; \
	done

HOST ?= localhost

deploy: tarball
	pv build/KoboRoot.tgz | ssh root@$(HOST) 'cd / ; tar zxf -'
