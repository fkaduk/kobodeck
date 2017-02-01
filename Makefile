# this is the proper way of doing static linking across architectures,
# cargo-culted from @tianon on #debian-golang
#GFLAGS+=-ldflags '-d -s -w' -a -tags netgo -installsuffix netgo
# i previously tried this, but that's the old way, cargo-culted from:
# https://dominik.honnef.co/posts/2015/06/go-musl/?
#GFLAGS+=--ldflags '-linkmode external -extldflags "-static"'

# to build for the Kobo, use:
# GOARCH=arm make build

all: lint build tarball

GNUARCH?=$(shell arch)

BINARY?=build/wallabako.$(GNUARCH)

tarball:
	@echo building Kobo tarball
	$(MAKE) build GOARCH=arm BINARY=build/wallabako.arm
	cp $(BINARY) root/usr/local/bin/wallabako
    # make sure we ship a SSL certs file as the Kobo doesn't have any (!)
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
	go vet ./...
	golint ./...
	gofmt  -s -l .

test:
	echo 'no tests implemented yet, but if i would, i would do that with -race as well'
