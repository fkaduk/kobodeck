Wallabag downloader
===================

This tool is designed to download EPUB (or eventually, other formats)
of individual unread articles from a Wallabag instance.

It is designed to be fast and ran incrementally: subsequent runs
should not redownload the files unless they have changed.

I wrote this to sync unread articles to my Kobo reader. I wrote this
in Go to get familiar with the language but also because it simplifies
deployment: a single static binary can be shipped instead of having to
ship a full interpreter in my normal language of choice (Python).

Installation
------------

Simply do the usual:

    go get gitlab.com/anarcat/wallabako

If you are unfamiliar with go, you may want to read up on the
[getting started](https://golang.org/doc/install) instructions. If you
do not wish to install golang at all, you can also download the
compiled binaries directly from the website:

> <https://gitlab.com/anarcat/wallabako/builds/artifacts/master/download?job=compile>

This will give you a ZIP file with standalone binaries for the
supported architectures (currently `amd64`, AKA `x86_64` and `arm7`).

Usage
-----

To use, fill in the fields in the `config.json` file. You will need to
create a "client" in the Wallabag interface first and copy those
secrets in the configuration file, along with your username and
password and the Wallabag URL, which should not have a trailing slash.

You will probably want to save that file to another location, for
example on your Kobo it should be in:

    cp config.json /mnt/onboard/.wallabako.js

Then to actually download the EPUB files:

    wallabako -config /mnt/onboard/.wallabako.js -output /mnt/onboard/wallabako/

The program is very verbose. Sorry.

Troubleshooting
---------------

### x509: failed to load system roots and no roots provided

You may see this error when running on weird environments;

```
[root@(none) ~]# ./wallabako -config /mnt/onboard/.wallabako.js -output /mnt/onboard/wallabako/
2017/01/30 14:45:46 logging in to https://example.net/wallabag
2017/01/30 14:45:51 <nil> Get https://example.net/wallabag/login: x509: failed to load system roots and no roots provided
2017/01/30 14:45:51 completed in 5.12s
panic: runtime error: invalid memory address or nil pointer dereference
[signal SIGSEGV: segmentation violation code=0x1 addr=0x0 pc=0x11280]

goroutine 1 [running]:
panic(0x2558a0, 0x1061e008)
	/usr/lib/go-1.7/src/runtime/panic.go:500 +0x33c
main.login(0x1060ee20, 0x19, 0x1067d958, 0x7, 0x1060ee60, 0x14, 0x0)
	/home/anarcat/go/src/gitlab.com/anarcat/wallabako/main.go:59 +0x280
main.main()
	/home/anarcat/go/src/gitlab.com/anarcat/wallabako/main.go:147 +0x280
```

This is because your operating system doesn't ship standard X509
certificates in the location the program expects them to be. A
workaround I have found is to copy the
`/etc/ssl/certs/ca-certificates.crt` provided by the `ca-certificates`
package in Debian in the machine.

> Note: it *may* be possible to fix the program to ignore the
> [SystemRootsError](https://golang.org/pkg/crypto/x509/#SystemRootsError)
> but I would advise against it, if only for obvious security
> reasons...
