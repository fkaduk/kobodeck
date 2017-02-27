Design notes
============

<!-- markdown-toc start - Don't edit this section. Run M-x markdown-toc-generate-toc again -->
**Table of Contents**

 - [File synchronisation and deletion](#file-synchronisation-and-deletion)
 - [Wifi trigger](#wifi-trigger)
 - [Autoreload](#autoreload)
 - [Read status and other metadata](#read-status-and-other-metadata)
 - [Logging](#logging)
 - [Autobuilders](#autobuilders)

<!-- markdown-toc end -->


This section explains in more details how the program works
internally. It shouldn't be necessary to read this to operate the
program.

I wrote this to sync unread articles to my Kobo ebook reader, but it
should work everywhere you can compile a go program, which includes
GNU/Linux, Mac OS X, Windows and FreeBSD systems.

I wrote this in Go to get familiar with the language but also because
it simplifies deployment: a single static binary can be shipped
instead of having to ship a full interpreter in my normal language of
choice (Python).

File synchronisation and deletion
---------------------------------

The script looks at the `updated_at` field in the entries API to
determine if a local file needs to be overwritten. Empty and missing
files are always downloaded.

If there are more than `-count` entries, the program will start
deleting old files if the `-delete` flag is given. It looks at the
`id` listed in the API and removes any file that is not found in the
listing, based purely on the filename.

Files are downloaded in parallel, up to the limited defined by the
`-concurrency` commandline flag, which defaults to 6, taken from the
Firefox default. The original HTTP/1.1 RFC [RFC2616][] specified a
[limit of two parallel connections][], but no one respects that
anymore. The newer RFC about this ([RFC7230][]) specifies
[no explicit limit][] and web browsers usually stick to between 6
(Chrome, Firefox, IE9) and 13 (IE11) parallel connections, see
[this chart][] for more details.

[RFC2616]: https://tools.ietf.org/html/rfc2616
[limit of two parallel connections]: https://tools.ietf.org/html/rfc2616#section-8.1.4
[RFC7230]: https://tools.ietf.org/html/rfc7230
[no explicit limit]: https://tools.ietf.org/html/rfc7230#section-6.4
[this chart]: http://www.browserscope.org/?category=network

Wifi trigger
------------

The program can be ran by hand, but is also designed to run
automatically. The sync script that is the main inspiration for this
([kobo-wget-sync][]) uses udev to trigger downloads, using those
[rules][]:

    KERNEL=="eth*", ACTION=="add", RUN+="/usr/local/bin/wallabako-run"
    KERNEL=="wlan*", ACTION=="add", RUN+="/usr/local/bin/wallabako-run"

[kobo-wget-sync]: https://github.com/wernerb/kobo-wget-sync/
[rules]: https://github.com/wernerb/kobo-wget-sync/blob/master/src/etc/udev/rules.d/98-wget-sync.rules

We reused the `eth*` and `wlan*` rules to kick the script when the
network goes up. We haven't done that for the `usb*` rules as
they do not provide network, but since that's actually another hack
that can be done, it may be a useful addition as well.

The rules call the `wallabako-run` shell script
which acts as an intermediate configuration file for the main
command. You can tweak some settings there, but this should all really
be part of the main configuration file.

When the program starts, it tries to login to the Wallabag instance
over the network. If that fails, it will sleep one second and try
again. If that fails again, it will sleep an exponential number of
seconds (2, 5, 10, 17, ...) per attempts, up to 4 attempts
(configurable on the commandline) for a total of 35 seconds.

Autoreload
----------

When new files are downloaded, they are not automatically added to the
library. There doesn't seem to be a clear way to do this on the Kobo
reader, short of plugging/unplugging the USB key, doing some magic
commands and tapping the screen, or rebooting. I have summarized my
findings in [this post][] in the hope that someone has a better idea.

[this post]: https://www.mobileread.com/forums/showthread.php?p=3467503

We have used the "tap to Connect confirm" approach until a better
solution is found. This is done through the
`fake-connect-usb` script, which in turns talks to
the (mysterious and undocumented) `/tmp/nickel-hardware-status`
socket.

This is not very reliable: sometimes the wifi sync doesn't start, or
at least the dialog doesn't show up, or maybe I don't see it in time -
anyways, the thing doesn't resync. The above forum may have new
answers now. Also look for "sync" in `.kobo/Kobo/Kobo\ eReader.conf`,
there's stuff there like:

    LastSyncTime=@Variant(\0\0\0\x10\0%\x80\x3\0\xe9R \x2)
    PeriodicAutoSync=false
    syncOnNextBoot=false

Read status and other metadata
------------------------------

The "read" status is now propagated by API calls to the Wallabag
API. When an article is marked as read on the e-reader it will be
marked as read on the API and if that succeeds, will be *deleted*
locally (if the `-delete` flag is provided, which is not the case by
default anymore). If the API calls fails for some reason, the file is not
deleted. This is to avoid getting into a delete/download loop as the
next call would download the file and then delete it again.

Articles that are currently in the "reading" state are never
deleted.

We use the
[mattn sqlite library](https://github.com/mattn/go-sqlite3), which is
the
[recommended one](https://www.reddit.com/r/golang/comments/2tijbf/which_sqlite3_package_to_use_mattngosqlite3_or/). I
followed the basic
[golang wiki](https://github.com/golang/go/wiki/SQLInterface) tutorial
. In the `.kobo/KoboReader.sqlite` database, we look for the book
status in the `content` table for the `ReadStatus` column, which seems
to be `0` for unread, `1` for in progress and `2` for read. The file
path is in the `ContentID` column, like
`file:///mnt/onboard/wallabako/N.epub` where `N` is our entry ID, and
we need to restrict ourselves to `ContentType` 6 otherwise we get many
entries per book (maybe it's how the Kobo keeps track of chapters).

On the Wallabag side, we do a `PATCH` request on the API at
`/api/entries/{entry}.{_format}` where `{entry}` is the article entry
(a number that is taken from the filename) and `{_format}` is
`json`. Then we need to set `archive` to `1` as a parameter.

Logging
-------

Logs are printed to the console by default. They used to be written to
a logfile in `/mnt/onboard/wallabako.log.txt` so that they can be read
by the user on the reader directly, until the 1.0.0 release at which
point it became an option configurable with the `LogFile`
parameter. The logs are currently quite verbose. After 10 days of more
or less continuous operation, the logfile here had grown to around
400KB. We have implemented log rotation using [lumberjack][] so that
we don't take up all the space on devices from version 0.9. We could
also do log level filtering to limit the amount of data in the logfile
but then that would reduce our much-needed debugging capabilities. We
could also send debugging logs to syslog.

[lumberjack]: https://github.com/natefinch/lumberjack

There are a *lot* of [logging libraries][] for Go, which is probably a
result of the limited functionality available in the standard
library. See also this [rated list][]. After a short review, I have
found the following libraries:

[logging libraries]: https://github.com/avelino/awesome-go#logging
[rated list]: https://golanglibs.com/category/logging

* [mlog](https://github.com/jbrodriguez/mlog) - supports log
  rotation, not a drop-in replacement
* [logutils](https://github.com/hashicorp/logutils) - wraps the
  standard library to filter based on strings, a bit too hackish?
* [logging](https://github.com/op/go-logging) - multi-backend support
  with differenciated level filtering, colors, seems well-designed and
  self-contained, not a drop-in replacement, overkill?
* [rlog](https://github.com/romana/rlog) - log level filtering and
  file output configurable through config file or environment,
  standlone, no rotation, not a drop-in replacement
* [glog](https://godoc.org/github.com/golang/glog) - level filtering,
  hooks into the flags package for output control, Google's simple
  implementation, can hook into the builtin log package but not send
  to it, no log rotation
* [lumberjack][] - rotation for the builtin logger
* [logger](https://github.com/azer/logger) - timers, env-based log
  selection, JSON output, overkill?
* [clog](https://github.com/go-clog/clog) - parallelized logger, can
  log to slack, files, console, level filtering, poor documentation,
  overkill?

Those projects weren't seriously considered, but may be interesting in
other projects:

* [logrus](https://github.com/Sirupsen/logrus) - level filtering, *lots*
  of backends supported, environments, formatters, *no* log rotation,
  thread-safe, structured, colors, oh my... 
* [log4go](https://github.com/Kissaki/log4go) - level filtering,
  rotation, XML, drop-in compatible with log, multi-backend support
  with differenciated levels, 
  [unmaintained](https://github.com/alecthomas/log4go)?
* [seelog](https://github.com/cihub/seelog) - lots of features, but
  XML config.
* [zap](https://github.com/uber-go/zap) - really fast, but weird
  calling sequence
* [logrotate](https://github.com/NYTimes/logrotate) - if we *would*
  use a logrotate daemon or cronjob, this would allow use to
  gracefully handle signals
* [logxi](https://github.com/mgutz/logxi) - colors, env-triggered
  levels, simpler interface than logrus, fast, structured

In the end we resolved it was simpler to stick with the builtin logger
and use the lightweigth lumberjack library for log rotation. We also
have a "debug" configuration setting to enable more verbose output,
but no "verbose" flag yet, although that could be implemented (and the
script could default to being silent).

Note that there are [discussions][] to include a Logger interface in
the standard library. The [proposal][] currently includes two logging
levels: Debug and Info. So our work seems to be forward compatible
with the direction the community is taking.

[proposal]: https://docs.google.com/document/d/1nFRxQ5SJVPpIBWTFHV-q5lBYiwGrfCMkESFGNzsrvBU/edit#
[discussions]: https://groups.google.com/forum/#!topic/golang-dev/F3l9Iz1JX4g%5B51-75%5D

We note it is possible the logfile itself may cause problems with
library reloads: since it is an open file on the `/mnt/onboard`
filesystem, it may keep the refresh from working properly. The
alternative would be to store the logfile in another location. The
`/var/log` directory on the Kobo, as it has only 16KB of storage,
which should be enough for a few days of logs. Unfortunately the
lumberjack library only rotate files after [one megabyte][] has been
used, which makes it impossible for us to use that location for now.

[one megabyte]: https://github.com/natefinch/lumberjack/issues/37

Autobuilders
------------

We use Gitlab's Continuous Integration (CI) to build binary images for
deployment. Because we needed cross-compilers, we
[updated the official Golang docker images to stretch][] which was
done fairly quickly.

[update the official Golang docker images to stretch]: https://github.com/docker-library/official-images/issues/2639

We could setup our own docker image to speed up the build process. See
the [container registry][] documentation along with the
[Gitlab docker documentation][].

[container registry]: https://gitlab.com/help/user/project/container_registry
[Gitlab docker documentation]: https://docs.gitlab.com/ce/ci/docker/using_docker_images.html

An issue with the autobuilder is that we trust Gitlab.com to not
inject hostile paylods in the binaries. I provide signed binaries in
releases built on my own computer for verification, but they are not
built with the same environment so we can't actually verify those
builds. There was some research done on package authentication and
automated upgrades in issue [#3 on Gitlab][].

[#3 on Gitlab]: https://gitlab.com/anarcat/wallabako/issues/3
