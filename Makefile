# this is the proper way of doing static linking across architectures,
# cargo-culted from @tianon on #debian-golang
#GFLAGS+=-ldflags '-d -s -w' -a -tags netgo -installsuffix netgo
# i previously tried this, but that's the old way, cargo-culted from:
# https://dominik.honnef.co/posts/2015/06/go-musl/?
#GFLAGS+=--ldflags '-linkmode external -extldflags "-static"'

# to build for the Kobo, use:
# GOARM=7 GOARCH=arm make build

all: lint build

build: wallabako

wallabako: *.go
	go build $(GFLAGS) -o $@

clean:
	rm wallabako || true

lint:
	go vet ./...
	golint ./...
	gofmt  -s -l .

test:
	echo 'no tests implemented yet, but if i would, i would do that with -race as well'
