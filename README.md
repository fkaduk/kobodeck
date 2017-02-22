Wallabag downloader
===================

<img alt="Logo" src="assets/logo.png" align="right" />

This tool is designed to automatically download Wallabag articles into
your local computer or Kobo ebook reader.

Features:

* **fast**: downloads only files that have changed, in parallel
* **unattended**: runs in the background, when the wifi is turned on, only
  requires you to tap the fake USB connection screen for the Kobo to
  rescan its database
* **status synchronization**: read books are marked as
  read in the Wallabag instance
* **easy install**: just drop the magic file in your kobo reader like any
  other book, edit one configuration file and you're done

The instructions here are mostly for the Kobo E-readers but may work
for other platforms. I have tested this on a Debian GNU/Linux 9
("stretch") system, a Kobo Glo HD and a Kobo Touch.

Table of contents:

- [Installation](#installation)
- [Configuration](#configuration)
- [Usage](#usage)
- [Support](#support)
- [Troubleshooting](#troubleshooting)
- [Contributing](#contributing)
- [Credits](#credits)
- [Design notes](#design-notes)
- [Remaining issues](#remaining-issues)

<img alt="screenshot of a KoboRoot.tgz file in a Kobo reader" src="assets/kobotgz-screenshot.png" align="right" />

Download and install
====================

Quick start for Kobo devices:

 1. connect your reader to your computer with a USB cable
 2. [download][] the [latest `KoboRoot.tgz`][]
 3. save the file in the `.kobo` directory of your e-reader
 4. create the configuration file as explained in the [configuration](#configuration)
section
 5. disconnect the reader

[latest `KoboRoot.tgz`]: https://gitlab.com/anarcat/wallabako/builds/artifacts/master/file/build/KoboRoot.tgz?job=compile
[download]: https://gitlab.com/anarcat/wallabako/builds/artifacts/master/file/build/KoboRoot.tgz?job=compile

When you disconnect the reader, it will perform what looks like an
upgrade, but it's just the content of the `KoboRoot.tgz` being
automatically deployed. If you connect the reader again, the
`KoboRoot.tgz` file should have disappeared.

When you connect your reader to a Wifi access point, the wallabako
program should run, which should create a `wallabako.log.txt` file at
the top directory of the reader which you can use to diagnose
problems, see also the [troubleshooting](#troubleshooting) section.

Configuration
=============

The next step is to configure Wallabako by creating a `.wallabako.js`
file in the top directory of the reader, with the following content:

    {
      "WallabagURL": "https://app.wallabag.it",
      "ClientId": "14_2vun20ernfy880wgkk88gsoosk4csocs4ccw4sgwk84gc84o4k",
      "ClientSecret": "69k0alx9bdcsc0c44o84wk04wkgw0c0g4wkww8c0wwok0sk4ok",
      "UserName": "joelle",
      "UserPassword": "your super password goes here"
    }

Make sure you use a plain text editor like `Gedit` or `Notepad`, as
LibreOffice will cause you trouble!

Let's take this one step at a time. First, the weird curly braces
syntax is because this is a [JSON](https://en.wikipedia.org/wiki/JSON)
configuration file. Make sure you keep all the curly braces, quotes,
colons and commas (`{`, `}`, `"`, `:`, `,`).

 1. The first item is the `WallabagURL`. This is the address of the
    service you are using. In the above example, we use the official
    [Wallabag.it](https://wallabag.it/) service, but this will change
    depending on your provider. Make sure there is *no* trailing slash
    (`/`).

 2. The second and third items are the "client tokens". Those are
    tokens that you need to create in the Wallabag web interface, in
    the `Developer` section. Simply copy paste those values in place.
 
 3. The fourth and fifth items are your username and passwords. We
    would prefer to not ask you your password, but unfortunately, that
    is [still required by the Wallabag API][password requirement of the API]

Also note that some commandline flags are hardcoded in the
`wallabag-run` script. To modify those, you will
need to modify the file in the `KoboRoot.tgz` file or hack the kobo to
get commandline access. There are also more settings you can set in
the configuration file, see the [troubleshooting](#troubleshooting)
section for more information.

<img alt="screenshot of the connect dialog on a Kobo Glo HD reader" src="assets/connect-dialog.png" align="right" />

Usage
=====

Kobo devices
------------

If everything was deployed correctly, Wallabako should run the next
time you activate the wireless connection on your device. You will
notice it is running because after a while, the dialog that comes up
when you connect your device with a cable will come up, even though
the device is not connected! Simply tap the `Connect` button to
continue the synchronisation and the library will find the new entries.

When an article downloaded with Wallabako is finished on your reader,
it will be marked as read in Wallabag.

Wallabako also downloads a limited numbers of articles from Wallabag,
and it *will* remove extra articles (for example if they are too old
or were marked as read in Wallabag).

By default, Wallabako will not delete old files from your reader - you
will need to remove those files through the reader interface
yourself. This is to avoid unnecessary synchronisations which are
distracting to the user.

Commandline
-----------

Wallabako can also be compiled installed on a regular computer,
provided that you have the go suite installed. Simply do the usual:

    go get gitlab.com/anarcat/wallabako

If you are unfamiliar with go, you may want to read up on the
[getting started][] instructions. If you do not wish to install golang
at all, you can also [download the standalone binaries][x86_64] for
[64 bits][x86_64] (aka `amd64` or `x86_64`) or [ARM][arm]
(e.g. Raspberry PI).

 [x86_64]: https://gitlab.com/anarcat/wallabako/builds/artifacts/master/file/build/wallabako.x86_64?job=compile
 [arm]: https://gitlab.com/anarcat/wallabako/builds/artifacts/master/file/build/wallabako.arm?job=compile
 [getting started]: https://golang.org/doc/install

You also need to create a [configuration](#configuration) file as
detailed above.

The program looks for the file in the following locations:

1. `$HOME/.config/wallabako.js`
2. `$HOME/.wallabako.js`
3. `/mnt/onboard/.wallabako.js`
4. `/etc/wallabako.js`

You will probably want to choose the first option unless you are
configuring this as a system-level daemon.

Then you can just run wallabako on the commandline. For example, this
will download your articles in the `epubs` directory in your home:

    wallabako -output ~/epubs

Use the `-h` flag for more information about the various flags you can
use on the commandline.

The program is pretty verbose, here's an example run:

    $ wallabako -output /tmp
    2017/01/31 22:16:41 logging in to https://example.net/wallabag
    2017/01/31 22:16:41 CSRF token found: 200 OK
    2017/01/31 22:16:41 logged in successful: 302 Found
    2017/01/31 22:16:42 found 65 unread entries
    2017/01/31 22:16:42 URL https://example.net/wallabag/export/23152.epub older than local file /tmp/23152.epub, skipped
    2017/01/31 22:16:42 URL https://example.net/wallabag/export/23179.epub older than local file /tmp/23179.epub, skipped
    2017/01/31 22:16:42 URL https://example.net/wallabag/export/23170.epub older than local file /tmp/23170.epub, skipped
    2017/01/31 22:16:42 URL https://example.net/wallabag/export/23180.epub older than local file /tmp/23180.epub, skipped
    2017/01/31 22:16:42 URL https://example.net/wallabag/export/23160.epub older than local file /tmp/23160.epub, skipped
    2017/01/31 22:16:42 processed: 5, downloaded: 0
    2017/01/31 22:16:42 completed in 1.44s

You can also run the program straight from source with:

    go run *.go

Support
=======

I will provide only limited free support for this tool. I wrote it,
after all, for my own uses. People are welcome to [file issues][] and
[send patches][], of course, but I cannot cover for every possible use
cases. There is also a [discussion on MobileRead.com][] if you prefer
that format.

 [file issues]: https://gitlab.com/anarcat/wallabako/issues
 [send patches]: https://gitlab.com/anarcat/wallabako/merge_requests
 [discussion on MobileRead.com]: https://www.mobileread.com/forums/showthread.php?p=3467945

Troubleshooting
===============

To troubleshoot issues with the script, you may need to get
commandline access into it, which is beyond the scope of this
documentation. See the following tutorial for example.

 * [Hacking the Kobo Touch for Dummies](http://www.chauveau-central.net/pub/KoboTouch/)
 * [Kobo Touch Hacking](https://wiki.mobileread.com/wiki/Kobo_Touch_Hacking)

Below are issues and solutions I have found during development that
you may stumble upon. Normally, if you install the package correctly,
you shouldn't get those errors so please do file a bug if you can
reproduce this issue.

Logging
-------

Versions from 0.3 to 1.0 were writing debugging information in the
`wallabako.log.txt` on the reader. This is now disabled by default
(see [this discussion for why][]) but can be enabled again by adding a
`LogFile` option in the configuration file, like this:

    {
      "WallabagURL": "https://app.wallabag.it",
      "ClientId": "14_2vun20ernfy880wgkk88gsoosk4csocs4ccw4sgwk84gc84o4k",
      "ClientSecret": "69k0alx9bdcsc0c44o84wk04wkgw0c0g4wkww8c0wwok0sk4ok",
      "UserName": "joelle",
      "UserPassword": "your super password goes here",
      "LogFile": "/mnt/onboard/wallabako.log.txt"
    }

[this discussion for why]: https://gitlab.com/anarcat/wallabako/merge_requests/1

This will make a `wallabako.log` file show up on your reader that you
can check to see what's going on with the command.

You can increase the verbosity of those logs with the `Debug`
commandline flag or configuration option (set to `true`, without
quotes). WARNING: this *will* include your password and authentication
tokens, so be careful where you send this output.

Configuration file details
--------------------------

Most commandline options (except `-version` and `-config`) can also be
set in the configuration file. Here are the configuration options and
their matching configuration file settings:

| Configuration | Flag           | Default           | Meaning |
| ------------- | -------------- | ----------------- | ------- |
| `Debug`       | `-debug`       | `false`           | include (lots of!) additional debugging information in logs, including passwords  and confidential data |
| `Delete`      | `-delete`      | `false`           | delete EPUB files marked as read or missing from Wallabag |
| `Database`    | `-database`    | `/mnt/onboard/.kobo/KoboReader.sqlite` | path to the Kobo database |
| `Concurrency` | `-concurrency` | 6                 | number of downloads to process in parallel |
| `Count`       | `-count`       | -1                | number of articles to fetch, -1 means use Wallabag default |
| `Exec`        | `-exec`        | nothing           | execute the given command when files have changed |
| `LogFile`     | N/A            | no logging        | rotated logfile to store debug information |
| `OutputDir`   | `-output`      | current directory | output directory to save files into |
| `PidFile`     | `-pidfile`     | `wallabako.pid`   | pidfile to write to avoid multiple runs |
| `RetryMax`    | `-retry`       | 4                 | number of attempts to login the website, with exponential backoff delay |

The pidfile is actually written in one of those directories, the first
one found that works:

 1. `/var/run`
 2. `/run`
 3. `/run/user/UID`
 4. `/home/USER/.`

There's no `-logfile` option anymore since this was not really useful:
you can just redirect output to a file using shell redirection (`>
logfile`). Also, it was difficult to implement logging for
configuration file discovery while at the same time allowing the
logfile to be changed when commandline flags are parsed.

Finally, note that some of those settings are hardcoded in the
`wallabako-run` wrapper script and therefore cannot be overriden in
the configuration file. Those are:

| Flag      | Value                             |
| --------- | --------------------------------- |
| `-output` | `/mnt/onboard/wallabako`          |
| `-exec`   | `/usr/local/bin/fake-connect-usb` |

Changing those settings could be dangerous. In particular, changing
the `-output` directory while enabling `-delete` could delete files
unexpectedly if they match the magic pattern (`N.epub` where N is an
integer).

x509: failed to load system roots and no roots provided
-------------------------------------------------------

You may see this error when running on weird environments;

```
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

By default, the tarball creation script adds that magick file to the
`KoboRoot.tgz` archive, which should work around this problem. But
this was never tested from scratch.

> Note: it *may* be possible to fix the program to ignore the
> [SystemRootsError](https://golang.org/pkg/crypto/x509/#SystemRootsError)
> but I would advise against it, if only for obvious security
> reasons...

Command not running
-------------------

If you notice that udev is not running your command, for some reason,
you can restart it with `--debug` which is very helpful. Example:

    [root@(none) ~]# ps ax | grep udev
      621 root       0:00 /sbin/udevd -d
     1242 root       0:00 grep udev
    [root@(none) ~]# kill 621
    [root@(none) ~]# /sbin/udevd --debug
    [1256] parse_file: reading '/lib/udev/rules.d/50-firmware.rules' as rules file
    [1256] parse_file: reading '/lib/udev/rules.d/50-udev-default.rules' as rules file
    [1256] parse_file: reading '/lib/udev/rules.d/60-persistent-input.rules' as rules file
    [1256] parse_file: reading '/lib/udev/rules.d/75-cd-aliases-generator.rules' as rules file
    [1256] parse_file: reading '/etc/udev/rules.d/90-wallabako.rules' as rules file
    [1256] parse_file: reading '/lib/udev/rules.d/95-udev-late.rules' as rules file
    [1256] parse_file: reading '/lib/udev/rules.d/kobo.rules' as rules file
    [...]
    [1276] util_run_program: '/usr/local/bin/wallabako-run' (stdout) '2017/01/31 00:03:50 logging in to https://example.net/wallabag'
    [1256] event_queue_insert: seq 859 queued, 'remove' 'module'
    [1256] event_fork: seq 859 forked, pid [1289], 'remove' 'module', 0 seconds old
    [1276] util_run_program: '/usr/local/bin/wallabako-run' (stdout) '2017/01/31 00:03:50 failed to get login page:Get https://example.net/wallabag/login: dial tcp: lookup example.net on 192.168.0.1:53: dial udp 192.168.0.1:53: connect: network is unreachable'

In the above case, network is down, probably because the command ran
too fast. You can adjust the delay in `wallabako-run`, but really this
should be automated in the script (which should retry a few times
before giving up).

Contributing
============

Contributions are very welcome. Send merge requests, issues and bug
reports on the [wallabako project on Gitlab][].

[wallabako project on Gitlab]: https://gitlab.com/anarcat/wallabako/

The documentation is currently all in this README file and can be
[edited online][] once you register. The
[discussion on MobileRead.com][] may also be a good place to get help
if you need to.

[edited online]: https://gitlab.com/anarcat/wallabako/edit/master/README.md

Credits
=======

Wallabako was written by The Anarcat and reviewed by friendly Debian
developers `juliank` and `stapelberg`. `smurf` also helped in
reviewing the code and answering my million newbie questions about go.

Also thanks to [Norbert Preining][] for pulishing the
[Kobo firmare images][] that got me started into this path and allowed
me to easily root my reader. This inspired me to start the related
[kobo-ssh][] project to build smaller, ssh-only images.

[kobo-ssh]: https://gitlab.com/anarcat/kobo-ssh
[Norbert Preining]: https://www.preining.info/
[Kobo firmare images]: https://www.preining.info/blog/2016/01/kobo-firmware-3-19-5761-mega-update-ksm-nickel-patch-ssh-fonts/

This program and documentation is distributed under the AGPLv3
license, see the LICENSE file for more information.

Design notes
============

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
  implementation, can hook into the builtin log package, no log
  rotation
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
and use the lightweigth lumberjack library for log rotation.

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

Remaining issues
================

There are `XXX` markers in the source code that show other issues that
need to be checked. The other known issues previously stored in this
file have been moved to the [Gitlab issue queue][] to allow for better
visibility and public collaboration.

[Gitlab issue queue]: https://gitlab.com/anarcat/wallabako/issues
