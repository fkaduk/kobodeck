GNUARCH ?= $(shell arch)
BINARY  ?= build/kobodeck.$(GNUARCH)
SOURCES  = $(wildcard *.go) go.mod go.sum kobodeck.toml Makefile

GFLAGS += -ldflags="-s -w -X main.buildVersion=$(shell git describe --always --dirty --tags)"
CROSS_COMPILE_FLAGS = GOARCH=arm GOOS=linux CGO_ENABLED=0

.PHONY: all tarball build tag clean check lint fmt fmt-check mod-check test test-e2e

all: tarball

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
	rm -f ./build/*

check: lint fmt-check mod-check test

lint:
	go vet ./...
	docker run --rm \
		--env NPM_CONFIG_UPDATE_NOTIFIER=false \
		--volume "$(CURDIR):/workspace:ro" \
		--workdir /workspace \
		node:22-alpine \
		npx --yes markdownlint-cli@0.49.1 README.md
	docker run --rm \
		--volume "$(CURDIR):/workspace:ro" \
		--workdir /workspace \
		koalaman/shellcheck-alpine:v0.11.0 \
		shellcheck vm_test.sh vm_test_guest.sh

fmt:
	gofmt -s -w .
	docker run --rm \
		--user "$$(id -u):$$(id -g)" \
		--volume "$(CURDIR):/workspace" \
		--workdir /workspace \
		mvdan/shfmt:v3.13.1 \
		-w vm_test.sh vm_test_guest.sh

fmt-check:
	@out=$$(gofmt -s -l .); if [ -n "$$out" ]; then echo "gofmt: these files need formatting:"; echo "$$out"; exit 1; fi
	docker run --rm \
		--volume "$(CURDIR):/workspace:ro" \
		--workdir /workspace \
		mvdan/shfmt:v3.13.1 \
		-d vm_test.sh vm_test_guest.sh

mod-check:
	go mod tidy -diff

test:
	CGO_ENABLED=0 go test .

test-e2e: tarball
	./vm_test.sh
