# this is the proper way of doing static linking across architectures,
# cargo-culted from @tianon on #debian-golang
#GFLAGS+=-ldflags '-d -s -w' -a -tags netgo -installsuffix netgo
# i previously tried this, but that's the old way, cargo-culted from:
# https://dominik.honnef.co/posts/2015/06/go-musl/?
#GFLAGS+=--ldflags '-linkmode external -extldflags "-static"'

# to build for the Kobo, use:
# GOARCH=arm make build

all: lint build tarball

BINARY?=build/wallabako

tarball:
	@echo building Kobo tarball
	$(MAKE) build GOARCH=arm BINARY=root/usr/local/wallabako/wallabako
	tar -C root/ -c -z -f build/KoboRoot.tgz etc /etc/ssl/certs/ca-certificates.crt usr
    # fake arm build. this makes the tarball target more expensive,
    # but is useful to avoid leaving stuff behind for gitlab CI
	mv root/usr/local/wallabako/wallabako build/wallabako.arm

build: $(BINARY)

$(BINARY): *.go
	@echo building main program
	mkdir -p $$(dirname $(BINARY))
	go build $(GFLAGS) -o $@

clean:
	rm $(BINARY) root/usr/local/wallabako/wallabako || true

lint:
	@echo checking idioms and syntax
	go vet ./...
	golint ./...
	gofmt  -s -l .

test:
	echo 'no tests implemented yet, but if i would, i would do that with -race as well'
