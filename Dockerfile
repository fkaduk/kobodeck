FROM golang:stretch

RUN apt-get update && apt-get install -y gcc-arm-linux-gnueabihf golint

# Build wallabako
WORKDIR /wallabako/
ADD . /wallabako/

CMD ["make", "tarball"]
