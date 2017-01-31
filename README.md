Wallabag downloader
===================

This tool is designed to download EPUB (or eventually, other formats)
of individual unread articles from a Wallabag instance.

It is designed to be fast and ran incrementally: subsequent runs
should not redownload the files unless they have changed.

Context
-------

I wrote this to sync unread articles to my Kobo ebook reader, but it
should work everywhere you can compile a go program, which includes
GNU/Linux, Mac OS X, Windows and FreeBSD systems.

I wrote this in Go to get familiar with the language but also because
it simplifies deployment: a single static binary can be shipped
instead of having to ship a full interpreter in my normal language of
choice (Python).

The following instructions assume your are familiar with the
commandline. To install this on your Kobo reader, you will need to
first hack it. See the following tutorials for more information:

 * [Hacking the Kobo Touch for Dummies](http://www.chauveau-central.net/pub/KoboTouch/)
 * [Kobo Touch Hacking](https://wiki.mobileread.com/wiki/Kobo_Touch_Hacking)

Those instructions are mostly for the Kobo Touch but may work for
other platforms. I have tested this on a Debian GNU/Linux 9
("stretch") system and a Kobo Glo HD.

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
supported architectures (currently `amd64`, AKA `x86_64` and
`arm7`).

In that archive, there is also a `KoboRoot.tgz` file that *may* allow
you to automatically configure the system, although you will at least
need to change the configuration file to add your credentials. This
was *not* tested, use at your own risk.

Usage
-----

To use, fill in the fields in the `etc/wallabako.js` file. You will need to
create a "client" in the Wallabag interface first and copy those
secrets in the configuration file, along with your username and
password and the Wallabag URL, which should not have a trailing slash.

Then to actually download the EPUB files:

    wallabako -config /etc/wallabako.js -output /mnt/onboard/wallabako/

The program is pretty verbose, here's an example run:

    $ go run main.go -config ~/.wallabako.js -output ~/tmp/epubs -count 1
    2017/01/30 16:31:12 logging in to https://example.net/wallabag
    2017/01/30 16:31:13 CSRF token found:  200 OK
    2017/01/30 16:31:13 logged in successful: 302 Found
    2017/01/30 16:31:13 found 65 unread entries
    2017/01/30 16:31:13 URL https://example.net/wallabag/export/23160.epub older than local file /home/anarcat/tmp/epubs/1234.epub, skipped
    2017/01/30 16:31:13 completed in 0.83s

Automatic configuration can be performed with the `KoboRoot.tgz` file,
otherwise you will need to deploy the files in the `root/` directory
by hand somehow. This has not yet been tested in a deployment from
scratch, but the test deployment has been working a few times.

So this actually works!

Design notes
------------

### File deletion and synchronisation

The script looks at the `updated_at` field in the entries API to
determine if a local file needs to be overwritten. Empty and missing
files are always downloaded.

If there are more than `-count` entries, the program will start
deleting old files if the `-delete` flag is given. It looks at the
`id` listed in the API and removes any file that is not found in the
listing, based purely on the filename.

### Wifi trigger

The program can be ran by hand, but is also designed to run
automatically. The sync script that is the main inspiration for this
([kobo-wget-sync](https://github.com/wernerb/kobo-wget-sync/)) uses
udev to trigger downloads, using those
[rules](https://github.com/wernerb/kobo-wget-sync/blob/master/src/etc/udev/rules.d/98-wget-sync.rules):

    KERNEL=="eth*", RUN+="/usr/local/wallabako/wallabako-run" 
    KERNEL=="wlan*", RUN+="/usr/local/wallabako/wallabako-run"

We reused the `eth*` and `wlan*` rules to kick the script when the
network goes up or down. We haven't done that for the `usb*` rules as
they do not provide network, but since that's actually another hack
that can be done, it may be a useful addition as well.

The rules call the `/usr/local/wallabako/wallabako-run` shell script
which acts as an intermediate configuration file for the main
command. You can tweak some settings there, but this should all really
be part of the main configuration file.

### Autoreload

When new files are downloaded, they are not automatically added to the
library. There doesn't seem to be a clear way to do this on the Kobo
reader, short of plugging/unplugging the USB key, doing some magic
commands and tapping the screen, or rebooting. I have summarized my
findings in
[this post](https://www.mobileread.com/forums/showthread.php?p=3467503)
in the hope that someone has a better idea.

<del>So far, the simplest solution would be to reboot when the
filesystem is changed. This can be done with the `-exec /sbin/reboot`
flag.</del> Unfortunately, even that doesn't trigger a refresh. We
have used the "tap to Connect confirm" approach until a better
solution is found. This is done through the
`usr/local/wallabako/fake-connect-usb` script, which in turns talks to
the (mysterious and undocumented) `/tmp/nickel-hardware-status`
socket.

Remaining issues
----------------

Those are known issues with the program. There are also `XXX` markers
in the source code that show other issues that need to be checked.

### Autoconfiguration

This requires a significant amount of work to work on a Kobo. Ideally,
we would just ship a KoboRoot.tgz that would work everywhere.

There is work done here - the autobuilders on Gitlab should generate a
`KoboRoot.tgz` that would deploy the binary, config files and
everything, but it is not tested yet.

### Logging

Debugging this script is hard. There are no logs and it's been mostly
tested on the commandline so far. There are tips on how to debug
`udev`, below, but we should have a more readily accessible logfile.

### Port to Wallabag 2.2 API changes

The new Wallabag release (2.2) gives us a new API to download actual
EPUBs directly, without having to login in a separate session. Before
we do this, my friendly provider needs to update the instance so I can
test this, which depends on the release stabilizing a little.

### Read status and other metadata

The "read" status is not propagated: when an article is read on the
e-reader, it's not propagated back to the Wallabag site. Similarly,
annotations are not sent back either. We could probably read the
sqlite database and send that data back, eventually.

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

### Command not running

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
    [1276] util_run_program: '/usr/local/wallabako/wallabako-run' (stdout) '2017/01/31 00:03:50 logging in to https://example.net/wallabag'
    [1256] event_queue_insert: seq 859 queued, 'remove' 'module'
    [1256] event_fork: seq 859 forked, pid [1289], 'remove' 'module', 0 seconds old
    [1276] util_run_program: '/usr/local/wallabako/wallabako-run' (stdout) '2017/01/31 00:03:50 failed to get login page:Get https://example.net/wallabag/login: dial tcp: lookup example.net on 192.168.0.1:53: dial udp 192.168.0.1:53: connect: network is unreachable'

In the above case, network is down, probably because the command ran
too fast. You can adjust the delay in `wallabako-run`, but really this
should be automated in the script (which should retry a few times
before giving up).
