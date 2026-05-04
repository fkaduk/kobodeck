GNUARCH ?= $(shell arch)
BINARY  ?= build/kobodeck.$(GNUARCH)

# Embed the version number in the binary.
GFLAGS += -ldflags="-X main.version=$(shell git describe --always --dirty --tags)"
CROSS_COMPILE_FLAGS = GOARCH=arm GOOS=linux CGO_ENABLED=0

all: check build tarball

tarball:
	@echo building Kobo tarball
	$(MAKE) build BINARY=build/kobodeck.arm $(CROSS_COMPILE_FLAGS)
	cp build/kobodeck.arm root/usr/local/bin/kobodeck
	touch root/usr
	tar -C root/ -c -z -f build/KoboRoot.tgz etc usr
	rm root/usr/local/bin/kobodeck

build: $(BINARY)

$(BINARY): *.go
	mkdir -p $$(dirname $(BINARY))
	CGO_ENABLED=0 go build $(GFLAGS) -o $@
	strip $@ || true

clean:
	rm -f build/kobodeck.* build/KoboRoot.tgz

check: lint test

lint:
	go vet ./...
	@out=$$(gofmt -s -l .); if [ -n "$$out" ]; then echo "gofmt: these files need formatting:"; echo "$$out"; exit 1; fi
	go mod tidy
	@out=$$(git diff --name-only go.mod go.sum); if [ -n "$$out" ]; then echo "go.mod/go.sum out of sync, run go mod tidy"; git checkout go.mod go.sum; exit 1; fi

test:
	CGO_ENABLED=0 go test -timeout 120s ./...

