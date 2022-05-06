# flags used to cross-compile to ARM, necessary for Kobo devices
CROSS_COMPILE_FLAGS=GOARCH=arm GOOS=linux

# comment this out to revert to the old "cgo" implementation that
# directly links with sqlite3. the "modernc" implementation is a pure
# go implementation that is much faster to compile and actually
# cross-compiles correctly for the older kobo kernels (2.6!)
# GFLAGS+=--tags "sqlite3"
#
# this is also necessary, to tell go to use CGO and the right C
# cross-compiler
#
# CROSS_COMPILE_FLAGS+=CGO_ENABLED=1 CC="arm-linux-gnueabihf-gcc"
#
# finally, cross-compiling will require the gcc-arm-linux-gnueabihf
# package as well.

# to do a fully static build we tried this:
#
# LDFLAGS=-d -s -v -w -linkmode external -extldflags -static
# GFLAGS+=-a -tags netgo -installsuffix netgo -v --tags "linux"
#
# but this also failed on older kernels, when building on anything
# older than Debian buster. that is presumably due to a flaw in GCC
# cross compilation that we couldn't diagnose. we were building the
# binaries in a debian stretch image (golang:stretch) to work around
# that problem, but that isn't necessary in "modern" mode.

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
	$(MAKE) build BINARY=build/wallabako.arm $(CROSS_COMPILE_FLAGS)
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
	strip $@

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
