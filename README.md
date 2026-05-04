# Readeck Article Downloader for Kobo

**kobodeck** is a minimalist article downloader for Kobo devices.

It can fetch content from a Readeck instance,
and sync its status (read/archived) from the Kobo device
to the Readeck server.

The code is forked from
[wallabako](https://gitlab.com/anarcat/wallabako).

## who is this for ?

This plugin could be useful for you if you
- do not want to use [KOReader](https://koreader.rocks/). If you do, check out the readeck plugin [xx](xx) or OPDS, which KOReader and Readeck natively support.
- are ok with mixing ebooks and with articles in the UI, ie if
- are fine with a lack of ui. Content fetching and downloads happen in the background.

## how to use it

When the wifi is turned on, kobodeck fakes a USB connection.
...
When you press **Connect**, the fake USB connection will close immediately and a rescan of the database will be triggered.
If you press **Cancel**, ...

<img alt="screenshot of the connect dialog on a Kobo Glo HD reader" src="assets/connect-dialog.png" align="right" />

# Testing

The instructions here are mostly for the Kobo E-readers but may work
for other platforms. I have tested this on a Debian GNU/Linux 9
("stretch") system, a Kobo Glo HD and a Kobo Touch.

## Installation or Upgrade

To install or upgrade,

1. obtain the latest `KoboRoot.tgz` either by downloading the binary or compiling it yourself
1. save the file in the `.kobo` directory of your e-reader
1. copy and edit the configuration file `.kobodeck.toml` — see [`root/etc/kobodeck.toml`](root/etc/kobodeck.toml) for the full annotated example
1. optionally verify your configuration with `kobodeck --check` before deploying via `kobodeck --check TODO`
1. store the `.kobodeck.toml` in the root of your kobo device
1. safely disconnect the reader; it should restart, install kobodeck and remove `KoboRoot.tgz`

# Usage
## Kobo devices

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

## Commandline

Wallabako can also be compiled installed on a regular computer,
provided that you have the go suite installed. Simply do the usual:

    go get gitlab.com/anarcat/wallabako

If you are unfamiliar with go, you may want to read up on the
[getting started][] instructions. If you do not wish to install golang
at all, you can also [download the standalone binaries][x86_64] for
[64 bits][x86_64] (aka `amd64` or `x86_64`) or [ARM][arm]
(e.g. Raspberry PI).

 [x86_64]: https://gitlab.com/anarcat/wallabako/builds/artifacts/main/file/build/wallabako.x86_64?job=compile
 [arm]: https://gitlab.com/anarcat/wallabako/builds/artifacts/main/file/build/wallabako.arm?job=compile
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

Building for the Kobo requires more work, see the [contribution
guide](CONTRIBUTING.md) for details.

## On-screen display

Out of the box, wallabako does not know how to give user feedback on
ebook readers like the Kobo. It can (and will), however, use the
[fbink][] program if it's found. If you have [koreader][] or [kfmon][]
installed, it should be able to find it there.

This feature will show wallabako's log in a crude overlay. By default,
it piles them up on two lines at the bottom of the screen. This is
less intrusive but can be hard to read.

To get the full experience, try to enable `FbinkInteractive` in the
configuration file, which will scroll all the messages, one per line,
from the top of the display, completely clobbering the current
display. This can be extremely distracting which is why it's off by
default.

Even without that setting, `fbink` will be used to display messages,
if available. The difference is `fbink` will be called repeatedly,
clobbering previous messages but limiting the amount of text flooding
the display.

And, of course, if you haven't modified your reader far enough to have
`fbink` actually installed, you'll never see that good stuff. If you
want to *only* install `fbink` (without koreader of kfmon), it seems
the canonical installation instructions are in [this forum post](https://www.mobileread.com/forums/showthread.php?t=299110).

[fbink]: https://github.com/NiLuJe/FBInk
[kfmon]: https://github.com/NiLuJe/kfmon/

# Uninstalling

Because of the peculiar way Kobo devices are provisioned, uninstalling
Wallabako can be tricky. For that reason, Wallabako supports a special
setting called `Uninstall` in its configuration file. For example, the
following, minimal configuration file in `.wallabako.js` at the root
of your Kobo directory (when plugged in your computer) will tell
Wallabako to uninstall itself:

```
{ "Uninstall": true }
```

It's also possible to pass the `-uninstall` flag on the command-line.

Note that this uninstall procedure is not *fully* complete: the file
`etc/ssl/certs/ca-certificates.crt` is left installed on the
machine. That's because that's a critical system file that we feel
relatively comfortable shipping, but removing it from some systems
could be catastrophic. It's typically a small file (around 223KB) so
it is not considered to be a big problem. If you *really* want to
remove that as well, you need to specify an extra flag:

```
{
  "Uninstall": true,
  "UninstallCerts": true
}
```

Similarly, we do not delete the configuration file created by the user
or the files downloaded by Wallabako. We assume that, if the user is
able to edit the configuration file or deploy the `KoboRoot.tgz` file,
the user is also capable of removing those files themselves, as they
are accessible when connecting the Kobo to a computer.

Uninstalling Wallabako with itself has been supported since
1.7.0. Before that, only manual uninstallation methods were available.

## Manual uninstall

If you have command-line access, you can likely remove the affected
files yourself. At the time of writing, the files deployed by the
`KoboRoot.tgz` installer are:

```
etc/udev/rules.d/90-wallabako.rules
etc/wallabako.js
etc/ssl/certs/ca-certificates.crt
usr/local/bin/wallabako
usr/local/bin/fake-connect-usb
usr/local/bin/wallabako-run
```

Of those, the most important is `/etc/udev/rules.d/90-wallabako.rules`
because that is where `wallabako` gets called automatically on network
changes. So removing the `.rules` file should be enough to keep
Wallabako from starting at all as well.

Another option is to remove the `.wallabako.js` file altogether. That
will "unconfigure" Wallabako which will still fire automatically when
the network comes up, but it will do nothing.

# Troubleshooting

When you connect your reader to a Wifi access point, the wallabako
program should run, which should create a `wallabako.log.txt` file at
the top directory of the reader which you can use to diagnose
problems, see also the [troubleshooting](#troubleshooting) section.

To troubleshoot issues with the script, you may need to get
commandline access into it, which is beyond the scope of this
documentation. See the following tutorial for example.

 * [Hacking the Kobo Touch for Dummies](http://www.chauveau-central.net/pub/KoboTouch/)
 * [Kobo Touch Hacking](https://wiki.mobileread.com/wiki/Kobo_Touch_Hacking)

Below are issues and solutions I have found during development that
you may stumble upon. Normally, if you install the package correctly,
you shouldn't get those errors so please do file a bug if you can
reproduce this issue.

## Logging

Versions from 0.3 to 1.0 were writing debugging information in the
`wallabako.log.txt` on the reader. This is now disabled by default
(see [this discussion for why][]) but can be enabled again by adding a
`logfile` option in the configuration file, like this:

    {
      "WallabagURL": "https://app.wallabag.it",
      "ClientId": "14_2vun20ernfy880wgkk88gsoosk4csocs4ccw4sgwk84gc84o4k",
      "ClientSecret": "69k0alx9bdcsc0c44o84wk04wkgw0c0g4wkww8c0wwok0sk4ok",
      "UserName": "joelle",
      "UserPassword": "your super password goes here",
      "logfile": "/mnt/onboard/wallabako.log.txt"
    }

[this discussion for why]: https://gitlab.com/anarcat/wallabako/merge_requests/1

This will make a `wallabako.log` file show up on your reader that you
can check to see what's going on with the command.

You can increase the verbosity of those logs with the `Debug`
commandline flag or configuration option (set to `true`, without
quotes). WARNING: this *will* include your password and authentication
tokens, so be careful where you send this output.

## Configuration file details

Most commandline options (except `-version` and `-config`) can also be
set in the configuration file. Here are the configuration options and
their matching configuration file settings:

| Configuration       | Flag              | Default                                | Meaning                                                                                                                             |
|---------------------|-------------------|----------------------------------------|-------------------------------------------------------------------------------------------------------------------------------------|
| `Debug`             | `-debug`          | `false`                                | include (lots of!) additional debugging information in logs, including passwords  and confidential data                             |
| `Delete`            | `-delete`         | `false`                                | delete EPUB files marked as read or missing from Wallabag                                                                           |
| `Database`          | `-database`       | `/mnt/onboard/.kobo/KoboReader.sqlite` | path to the Kobo database                                                                                                           |
| `Workers`           | `-concurrency`    | 2                                      | number of downloads to process in parallel                                                                                          |
| `Limit`             | `-count`          | -1                                     | number of articles to fetch, -1 means use Wallabag default                                                                          |
| `PostSync`          | `-exec`           | nothing                                | execute the given command when files have changed                                                                                   |
| `Log`               | `-logfile`        | no logging                             | rotated logfile to store debug information                                                                                          |
| `Output`            | `-output`         | current directory                      | output directory to save files into                                                                                                 |
| `PIDFile`           | `-pidfile`        | `wallabako.pid`                        | pidfile to write to avoid multiple runs                                                                                             |
| `RetryMax`          | `-retry`          | 4                                      | number of attempts to login the website, with exponential backoff delay                                                             |
| `Timeout`           | `-timeout`        | 300                                    | timeout for HTTP requests, in seconds                                                                                               |
| `Labels`            | `-tags`           | no tags filtering                      | a comma-separated list of tags to filter for                                                                                        |
| `Plato.LibraryPath` | N/A               | `/mnt/onboard`                         | For [plato document reader](https://github.com/baskerville/plato) integration, the value of `[[libraries.path]]` in `Settings.toml` |
| `Fbink`             | N/A               | `false`                                | use [fbink][] to overlay logs directly on the kobo screen, can be noisy                                                             |
| `FbinkInteractive`  | N/A               | `false`                                | use full screen interactive [fbink][] mode                                                                                          |
| `Uninstall`         | `-uninstall`      | `false`                                | uninstall Wallabako (!)                                                                                                             |
| `UninstallCerts`    | `-uninstallcerts` | `false`                                | also uninstall `ca-certificates.crt`                                                                                                |

Some more details about specific settings:

 * The `Limit` option actually defaults to 30 in Wallabag, at the time
   of writing. You may want to bump that up if you have more than 30
   unread articles, see [below][] for details.

   [below]: #some-articles-are-not-downloaded-or-disappear

 * The `PIDFile` is actually written in one of those directories, the
   first one found that works:

    1. `/var/run`
    2. `/run`
    3. `/run/user/UID`
    4. `/home/USER/.`

 * The `Fbink` settings only work if `fbink` is installed and
   available in the `$PATH`, wallabako doesn't ship fbink itself.

Finally, note that some of those settings are hardcoded in the
`wallabako-run` wrapper script and therefore cannot be overridden in
the configuration file. Those are:

| Flag      | Value                             |
| --------- | --------------------------------- |
| `-output` | `/mnt/onboard/wallabako`          |
| `-exec`   | `/usr/local/bin/fake-connect-usb` |

Changing those settings could be dangerous. In particular, changing
the `-output` directory while enabling `-delete` could delete files
unexpectedly if they match the magic pattern (`N.epub` where N is an
integer).

See [`root/etc/kobodeck.toml`](root/etc/kobodeck.toml) for the full annotated example configuration file.

## Configuration file is not found even if present

This can happen if you have some sort of a syntax error in the
configuration file. For example, this can happen if you have a
double-quote in your password and you didn't properly escape it.

The configuration file is a [JSON file][], parsed by the
[Unmarshal][] function of the
[Golang json package][]. [Wikipedia says][] that:

> JSON's basic data types are: [...] String: a sequence of zero or
> more Unicode characters. Strings are delimited with double-quotation
> marks and support a backslash escaping syntax.

This means that if you have a password that is, say `foo"bar`, you can
escape the special character with a backslash, like this:

```
"UserPassword":  "foo\"bar",
```

According to the [JSON specification][], strings should be delimited
by double-quote (`"`) and unless you have weird control characters in
your passwords (e.g. tab or formfeed), double-quote and backslash are
the only characters you should need to escape.

Another common error is to add an extra comma (`,`) on the final
entry, or omitting the brackets (`{` or `}`). Files with
[BOM markers][] used to cause issues as well, but that has been fixed
in the Wallabago library since 0.7.

[BOM markers]: https://en.wikipedia.org/wiki/Byte_order_mark

[JSON file]: https://en.wikipedia.org/wiki/JSON
[Unmarshal]: https://golang.org/pkg/encoding/json/#Unmarshal
[Golang json package]: https://golang.org/pkg/encoding/json/
[Wikipedia says]: https://en.wikipedia.org/wiki/JSON#Data_types.2C_syntax_and_example
[JSON specification]: http://json.org/
[issue #16]: https://gitlab.com/anarcat/wallabako/issues/16

## Some articles are not downloaded or disappear

If you can't seem to synchronize all your articles and you have a
large number of unread articles, you may want to change the `Limit`
field in the configuration file. By default, Wallabako only downloads
a part of the database: it is limited by the number of articles
returned by the Wallabag listing (`30` at the time of writing). 

Also, if the `Delete` option is set, older articles will be *deleted*
from the Kobo reader as well.

Note that it should be fairly safe to use a larger number here, as
only `Workers` (e.g. 6) articles will be downloaded in parallel at
a time. It could make the first listing request slower, however, if
you have a huge number of articles. We have reports of operation with
60 articles without significant performance issues.

## Unable to open database file

If you see this warning message repeated:

    2017/03/01 21:06:49 unable to open database file

It is because the database cannot be found. By default, the database
path is hardcoded to `/mnt/onboard/.kobo/KoboReader.sqlite`, which is
likely to work only on Kobo readers. If you are running this on your
desktop or another reader, you should disable the database by using
the `-database` flag:

    wallabako -database ""

... or configuration option in `.wallabako.js`:

    "Database": "",

Such configuration should silence those warnings as Wallabako will not
attempt to open a database file.

Note that the warnings can also safely be ignored.

## x509: failed to load system roots and no roots provided

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

## Command not running

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

# Known issues

Like any program, Wallabako is imperfect. There is a list of [known
issues](https://gitlab.com/anarcat/wallabako/issues) on the main site, but some issues do not depend on
Wallabako, as they are problems with Wallabag itself. Most of the
issues we have found in Wallabag are documented in [issue
#2821](https://github.com/wallabag/wallabag/issues/2821).

# Credits

Wallabako was written by The Anarcat and reviewed by friendly Debian
developers `juliank` and `stapelberg`. `smurf` also helped in
reviewing the code and answering my million newbie questions about go.

Also thanks to [Norbert Preining][] for publishing the
[Kobo firmware images][] that got me started into this path and allowed
me to easily root my reader. This inspired me to start the related
[kobo-ssh][] project to build smaller, ssh-only images.

[kobo-ssh]: https://gitlab.com/anarcat/kobo-ssh
[Norbert Preining]: https://www.preining.info/
[Kobo firmware images]: https://www.preining.info/blog/2016/01/kobo-firmware-3-19-5761-mega-update-ksm-nickel-patch-ssh-fonts/

This program and documentation is distributed under the AGPLv3
license, see the LICENSE file for more information.

# Contributing

See the [contribution guide](CONTRIBUTING.md) for more information. In
short: this is a free software project and you are welcome to join us
in improving it, both by fixing things, reporting issues or
documentation.

## Design notes

Moved a [separate document](DESIGN.md).

## Remaining issues

There are `XXX` markers in the source code that show other issues that
need to be checked. The other known issues previously stored in this
file have been moved to the [Gitlab issue queue][] to allow for better
visibility and public collaboration.

[Gitlab issue queue]: https://gitlab.com/anarcat/wallabako/issues

- Add signing via CI

- The integration test creates the Nickel SQLite database with a
  minimal 3-column schema. It should instead use the full real Kobo
  schema (stored as `testdata/nickel-schema.sql`) so that tests fail
  if the code makes assumptions that break on a schema change after a
  firmware update.

- The udev rule in `root/etc/udev/rules.d/90-kobodeck.rules` triggers
  on `eth*` in addition to `wlan*`. Kobos have no ethernet port, so the
  `eth*` line is dead code and should be removed.

- Sync favourite/starred status from the Kobo to Readeck (in addition to
  read status).

- Sync highlights and annotations from the Kobo (`Bookmark` table in
  `KoboReader.sqlite`) to Readeck's annotations API.

- Sync reading progress (current position) from the Kobo to Readeck,
  once Readeck exposes a progress field in its API.


# Related projects

Other Kobo-related software has support for Wallabako, and may be
easier to use than this program.

 * [KOReader](https://koreader.rocks/) includes a native plug-in that fully integrates with
   Wallabag servers. The [KOReader wiki](https://github.com/koreader/koreader/wiki) has [installation
   instructions](https://github.com/koreader/koreader/wiki#installationupgrading) and [details about Wallabag integration](https://github.com/koreader/koreader/wiki/Wallabag). That
   implementation is discussed in the [design](DESIGN.md) document as well.
 * [Plato](https://github.com/baskerville/plato/) also includes an [article fetcher](https://github.com/baskerville/plato/blob/master/doc/ARTICLE_FETCHER.md) with support for
   Wallabag

Note that Wallabako will also automatically fetch read/unread statuses
Plato and Koreader metadata, on top of the built-in Nickel interface
shipped with the Kobo.
