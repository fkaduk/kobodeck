FROM golang:stretch AS builder

RUN apt-get update && apt-get install -y gcc-arm-linux-gnueabihf golint

# Build wallabako
WORKDIR /go/src/wallabako/
ADD . /go/src/wallabako/

RUN make

FROM debian:stable-slim

COPY --from=builder /go/src/wallabako/build/wallabako.arm /go/src/wallabako/build/wallabako.x86_64 /usr/local/bin/
