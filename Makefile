# to do a static build we need this:
#LDFLAGS=-d -s -v -w -linkmode external -extldflags -static
#GFLAGS+=-a -tags netgo -installsuffix netgo -v --tags "linux"
# in theory, this would allow us to build a working wallabako
# anywhere, but in practice it doesn't work, see
# https://gitlab.com/anarcat/wallabako/-/issues/43

# embed the version number in the binary
GFLAGS+=-ldflags="$(LDFLAGS) -X main.version=$(shell git describe --always --dirty)"

# to build for the Kobo, use:
#
# GOARCH=arm make build
#
# this will fail to connect to the database because the sqlite plugin
# is a C extension. see the tarball target for how to build the
# package correctly.

all: lint build tarball

GNUARCH?=$(shell arch)

BINARY?=build/wallabako.$(GNUARCH)

tarball:
	@echo building Kobo tarball
	$(MAKE) build BINARY=build/wallabako.arm GOARCH=arm GOOS=linux CGO_ENABLED=1 CC="arm-linux-gnueabihf-gcc" $(GFLAGS)
	cp build/wallabako.arm root/usr/local/bin/wallabako
    # make sure we ship a SSL certs file as the Kobo doesn't have any (!)
    # Ensure root/usr modified time is updated to avoid tar 'file changed as we read it' issue in containers
	touch root/usr
	tar -C root/ -c -z -f build/KoboRoot.tgz etc /etc/ssl/certs/ca-certificates.crt usr
    # remove temporary file
	rm root/usr/local/bin/wallabako

build: $(BINARY)

$(BINARY): *.go
	@echo building main program
	mkdir -p $$(dirname $(BINARY))
	go build $(GFLAGS) -o $@

clean:
	rm $(BINARY) || true

lint:
	@echo checking idioms and syntax
	go vet .
	golint .
	gofmt  -s -l .
	go test

sign: lint build tarball
	@echo signing all binaries
	rm -f build/*.asc
	for bin in build/* ; do \
		gpg --detach-sign -a "$$bin"; \
	done

HOST?=localhost

deploy: tarball
	@echo deploying to $(HOST)
	pv build/KoboRoot.tgz | ssh root@$(HOST) 'cd / ; tar zxf -'
