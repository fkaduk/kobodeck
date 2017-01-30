# really make a static binary
GFLAGS+=--ldflags '-linkmode external -extldflags "-static"'

all: lint build

build: wallabako

wallabako: *.go
	go build $(GFLAGS) -o $@

clean:
	rm wallabako

lint:
	go vet ./...
	golint ./...
	gofmt  -s -l .

test:
	echo 'no tests implemented yet, but if i would, i would do that with -race as well'
