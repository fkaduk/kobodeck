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
