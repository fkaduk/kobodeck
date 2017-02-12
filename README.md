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
* **status synchronization**: read books are deleted from the Kobo (keep
  them as "in reading" to avoid deletion) and marked as
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
get commandline access. See the [troubleshooting](#troubleshooting)
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
it will be marked as read in Wallabag and will be *deleted from your
reader*. This is because it is only a copy: you can always go back in
Wallabag and mark the article as unread and Wallabako will download it
again.

Wallabako also downloads a limited numbers of articles from Wallabag,
and it *will* remove extra articles (for example if they are too old
or were marked as read in Wallabag).

Wallabako will never delete articles you are currently reading, even
if they are marked as read in Wallabag.

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

Also note that from 0.3, Wallabako logs debug information into
`wallabako.log.txt` on the reader, so you can look into those files to see
if it is running correctly when you plug your reader in a computer.

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

    KERNEL=="eth*", RUN+="/usr/local/bin/wallabako-run"
    KERNEL=="wlan*", RUN+="/usr/local/bin/wallabako-run"

[kobo-wget-sync]: https://github.com/wernerb/kobo-wget-sync/
[rules]: https://github.com/wernerb/kobo-wget-sync/blob/master/src/etc/udev/rules.d/98-wget-sync.rules

We reused the `eth*` and `wlan*` rules to kick the script when the
network goes up or down. We haven't done that for the `usb*` rules as
they do not provide network, but since that's actually another hack
that can be done, it may be a useful addition as well.

The rules call the `wallabako-run` shell script
which acts as an intermediate configuration file for the main
command. You can tweak some settings there, but this should all really
be part of the main configuration file.

When the program starts, it tries to login to the Wallabag instance
over the network. If that fails, it will sleep one second and try
again. If that fails again, it will sleep an exponential number of
seconds (2, 5, 10, 17, ...) per attempts, up to 5 attempts
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
locally. If the API calls fails for some reason, the file is not
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

Remaining issues
================

Those are known issues with the program. There are also `XXX` markers
in the source code that show other issues that need to be checked.

Usability
---------

Usability tests with a friendly user have revealed severe issues with
installing and using Wallabako. Here are the issues we found.

 1. there's *no "Download" button* that the user can just follow to
    get the software.
 
 2. users do not read the documentation, because it's a wall of
    text. We need a simple step-by-step procedure to help people
    deploy that thing on their devices.

 3. instructions for copying the file in place were incorrectly
    telling the user to put the file at the toplevel directory when
    the file needs to be in the `.kobo` subdirectory. the screenshot
    provided still shows that mistake.

 4. the instructions to edit the file also didn't work because the
    user used their usual text editor (LibreOffice) to create the
    `.wallabako.js` file and, when the `.txt` format was chosen, the
    file created was actually `.wallabako.js.txt`, even though the
    user properly entered the filename. This is arguably a bug in
    LibreOffice, but we can't expect users to workaround that on their
    own.

 5. the configuration file written by LibreOffice was not recognized
    as the JSON parser would crash on the
    [BOM](http://www.unicode.org/faq/utf_bom.html#BOM) marker inserted
    at the beginning of the file.

 6. the user had to be told to connect the reader back to see what was
    happening - they didn't find the logfile on their own.

 7. user attempted to tap the "Sync" button on the homepage to sync
    articles, which fails because that doesn't trigger wallabako

Proposed solutions:

 1. <del>file a bug against Gitlab to allow hotlinking to latest
    release. workaround: make a website with a hand-crafted link</del>
    there is such a link already, and it works great:
    
    <https://gitlab.com/anarcat/wallabako/builds/artifacts/master/file/build/KoboRoot.tgz?job=compile>

    `master` can be replaced by a tag name and that also works. filed
    a [MR to document this][]

 2. make a separate website on Gitlab pages or Readthedocs with a
    simple splash page and step-by-step instructions, hard-linking to
    the released version if necessary.

 3. instructions already fixed to mention the `.kobo` directory, need
    to fix screenshot as well.

 4. possibly write an installer that will generate the config file for
    the user, using a simple (even if commandline) question/answer
    dialog to download relevant files, create config files, copy it in
    place and so on.

 5. what the fuck, seriously. fix the parser to ignore BOM
    markers. telling users to "use a proper editor" sounds more like
    evangelism than a usability fix. providing a template file
    (instead of copy-pasting) might be a good workaround as well.

 6. make the logfile visible from the e-reader, by using a `.txt`
    (works, and done!) or `.html` (to be tested) extension

 7. research the "Sync" button to see if it triggers something we can
    hook into, see if there's answers to [this post][post57] or post a
    new question elsewhere...
    
 [post57]: https://www.mobileread.com/forums/showpost.php?p=3473914&postcount=57
 [MR to document this]: https://gitlab.com/gitlab-org/gitlab-ce/merge_requests/9161

Autoconfiguration
-----------------

This requires a significant amount of work to work on a Kobo. Now, the
autobuilders on Gitlab generate a `KoboRoot.tgz` that *should* deploy
the binary, config files and everything. I have not tested this yet,
and even if it works, the configuration file needs to be edited before
this works correctly.

Besides, even with everything perfectly aligned on our side, we still
need the user to create an "app" on the Wallabag side, which is a
painful and confusing step to follow for new users. I have started a
discussion about the [password requirement of the API][]
which touches on part of that issue.

[password requirement of the API]: https://github.com/wallabag/wallabag/issues/2800

Automatic upgrades
------------------

The first configuration is painful enough that we might want allow for
automatic upgrades. We have automated builds, it should be possible
for clients to download the new tarball and extract it on the fly as
necessary.

This gives the developer and build server remote code execution on the
binary, so it should be handled carefully. We could use something like
[signify](https://www.openbsd.org/papers/bsdcan-signify.html) (I don't
want to depend on OpenPGP here) to sign packages so that the build
server is trusted only once. This also requires builds to be
reproducible. [sigtool](https://github.com/opencoff/sigtool) looks
like a go version of signify that we could use.

This should also be able to discriminate between snapshots and tagged
releases. Since 0.7, wallabako knows which version it's
running. Version information is embedded at compile-time with
compile flags. This was inspired by:

* https://www.reddit.com/r/golang/comments/4cpi2y/question_where_to_keep_the_version_number_of_a_go/
* https://www.atatus.com/blog/golang-auto-build-versioning/
* http://stackoverflow.com/questions/11354518/golang-application-auto-build-versioning

Shorter autoreload delay
------------------------

We may be able to read the input device to figure out when the
confirmation tap happens to shorten the delay until the reload. See
[this discussion](https://www.mobileread.com/forums/showthread.php?p=3350658#post3350658)
for the idea and look at what happens in `/dev/input/event1` when a
tap is made.

Port to Wallabag 2.2 API changes
---------------------------------

The new Wallabag release (2.2) gives us a new API to download actual
EPUBs directly, without having to login in a separate session. Before
we do this, my friendly provider needs to update the instance so I can
test this, which depends on the release stabilizing a little.

Timestamps and order
--------------------

The Kobo reader shows the articles in the wrong order. It *looks* like
an alphabetical order, but I suspect it may due to the file
modification dates.

Because we download files in parallel, the file creation dates are not
ordered. (Arguably, even if they were, they would technically be
incorrect as well.) We could use the article modification date and set
the file modification date to match that.

Performance
-----------

The program is generally very fast, or at least, as fast as Wallabag
can be. It will download files in parallel and will avoid already
downloaded files, as mentionned in the design notes.

However, the API listing is very heavy. For large number of articles
(50+) it because a major slowdown in the script. I have reported this
as a [performance issue in the entries API][], so we'll see where this
goes.

[performance issue in the entries API]: https://github.com/wallabag/wallabag/issues/2817

EPUB generation is also pretty slow, but I guess there's not much we
can do about this, even in Wallabag: we need to build that EPUB
somehow.

It is also possible that the `read` status propagation fails and that
the program repeatedly downloads the same file over and over
again. The pattern would look like this:

 1. notice file is read
 2. fail set it as read in Wallabag, but do not notice the failure
 3. delete the file anyways
 4. on the next run, download the file
 5. go to 1

This could only happen if, for some reason the Wallabag API returns a
200 error code when submitting the [`PATCH` API request][] for the
entry, which would probably an error on the API side, although it
doesn't clearly [document how to discover error conditions][].

[document how to discover error conditions]: https://github.com/wallabag/wallabag/issues/2859

[`PATCH` API request]: http://v2.wallabag.org/api/doc#patch--api-entries-{entry}.{_format}

Spurious triggers
-----------------

We trigger the script on *any* udev activity on the network
interfaces. That means we start the script when the interface goes
*down* as well, which is silly because, well, the network is down.

I also noticed that sometimes, turning wifi on did *not* trigger the
script. We *could* abuse the init system's `respawn` flag (as there is
no cron daemon) to fire up the program repeatedly (with a sleep in
between, of course). But this could affect battery usage, so use with
care...

Even worse, the sync gets triggered then the emulated disconnect
happens: the nickel environment resumes, reconnects the wifi, and then
... starts the sync again. In some cases, it starts up so fast that
the drive is mount mounted yet - or at least the drive fails to mount,
somehow. Maybe we should keep the lock a little longer in the end? Not
sure.

Slow builds
-----------

Because we add stuff on top of the base docker images, our CI builds
are slow. Maybe we could setup our own docker image to speed up the
build process. See the [container registry][] documentation along with
the [Gitlab docker documentation][].

Alternatively, we could just wait for the official image to
[fix itself][]. In the meantime, the [container registry][] is enabled
and has simple usage instructions that we could follow.

[fix itself]: https://github.com/docker-library/official-images/issues/2639
[container registry]: https://gitlab.com/help/user/project/container_registry
[Gitlab docker documentation]: https://docs.gitlab.com/ce/ci/docker/using_docker_images.html

Unit tests
----------

Since this was my first go program, I figured I would reduce the
learning curve by just writing code instead of also learning to write
unit tests. But it's never too late to write tests! Some references:

* [testing package](https://golang.org/pkg/testing/) - the
  basic package, and the [official tutorial](https://golang.org/doc/code.html#Testing)
* [httptest](https://golang.org/pkg/net/http/httptest/) - to test HTTP
  requests specifically, see also this
  [tutorial](https://elithrar.github.io/article/testing-http-handlers-go/)
  which also includes database mocking
* see also [those slides](https://talks.golang.org/2014/testing.slide)

Annotations and read progress support
-------------------------------------

Annotations and read position are not propagated back. We could
probably read the sqlite database and send that data back, eventually.

All this stuff is not part of the Wallabago Go API, which could be
[extended to support more operations](https://github.com/Strubbl/wallabago/issues/5).

Better logging
--------------

Logs are currently written in a single logfile that is never rotated
and is quite verbose. After 10 days of more or less continuous
operation, the logfile here had grown to around 400KB and is still
growing. We will need to implement log rotation, or, at the very
least, log level filtering to limit the amount of data in the
logfile. This will probably involve rolling our own mechanisms for
this, as we can't assume there's a logrotate in the Kobo reader. We
could also send debugging logs to syslog.

Furthermore, it would be neat if we could have those logs readable
*inside the device*. That would bring much more visibility to what's
going on in Wallabako to the user. Even though it would still be quite
obscure, it would be more convenient than having to plug the device in
or login over SSH.

There are a *lot* of [logging libraries][] for Go, which is probably a
result of the limited functionality available in the standard
library. See also this [rated list][]. Of those, I should mention:

[logging libraries]: https://github.com/avelino/awesome-go#logging
[rated list]: https://golanglibs.com/category/logging

* [mlog](https://github.com/jbrodriguez/mlog) - supports log rotation
* [logutils](https://github.com/hashicorp/logutils) - wraps the
  standard library to filter based on strings
* [logging](https://github.com/op/go-logging) - multi-backend support
  with differenciated level filtering, colors, seems well-designed and
  self-contained
* [rlog](https://github.com/romana/rlog) - log level filtering and
  file output configurable through config file or environment,
  standlone
* [glog](https://godoc.org/github.com/golang/glog) - level filtering,
  hooks into the flags package for output control, Google's simple
  implementation, can hook into the builtin log package
* [lumberjack](https://github.com/natefinch/lumberjack) - rotation for
  the builtin logger
* [logger](https://github.com/azer/logger) - timers, env-based log
  selection, JSON output
* [clog](https://github.com/go-clog/clog) - parallelized logger, can
  log to slack, files, console, level filtering, poor documentation

Probably shouldn't considered, but may be interesting in other
projects:

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
