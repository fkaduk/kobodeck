GNUARCH ?= $(shell arch)
BINARY  ?= build/kobodeck.$(GNUARCH)
SOURCES  = $(wildcard *.go) go.mod go.sum kobodeck.toml Makefile

GFLAGS += -ldflags="-s -w -X main.version=$(shell git describe --always --dirty --tags)"
CROSS_COMPILE_FLAGS = GOARCH=arm GOOS=linux CGO_ENABLED=0

.PHONY: all tarball build tag clean check test

all: check tarball

tarball:
	@echo building Kobo tarball
	$(MAKE) -B build BINARY=build/kobodeck.arm $(CROSS_COMPILE_FLAGS)
	mkdir -p root/usr/local/bin
	cp build/kobodeck.arm root/usr/local/bin/kobodeck
	touch root/usr
	tar --owner=0 --group=0 --mode='u+rwX,go+rX,go-w' \
		-C root/ -c -z -f build/KoboRoot.tgz etc usr
	rm root/usr/local/bin/kobodeck

build: $(BINARY)

$(BINARY): $(SOURCES)
	mkdir -p $$(dirname $(BINARY))
	CGO_ENABLED=0 go build $(GFLAGS) -o $@

tag:
	@test -z "$$(git status --porcelain)" || (echo "error: working tree is dirty"; exit 1)
	@read -p "Version (e.g. v2.0.0): " v && \
	  echo "Tagging $$v at $$(git rev-parse --short HEAD)" && \
	  read -p "Push to origin? [y/N] " confirm && [ "$$confirm" = "y" ] && \
	  git tag $$v && git push origin $$v

clean:
	rm -f build/kobodeck.* build/KoboRoot.tgz

check:
	go vet ./...
	@out=$$(gofmt -s -l .); if [ -n "$$out" ]; then echo "gofmt: these files need formatting:"; echo "$$out"; exit 1; fi
	go mod tidy -diff
	markdownlint **/*.md

test: tarball
	CGO_ENABLED=0 go test .
	./vm_test.sh
