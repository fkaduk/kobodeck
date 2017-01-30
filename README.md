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

To use, fill in the fields in the `config.json` file. Make sure you
don't commit changes to that file:

    git update-index --assume-unchanged config.json

Then you can simply run the tool which will download files in the
current directory:

    go run config.go main.go  --count 10
