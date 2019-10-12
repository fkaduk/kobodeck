Design notes
============

<!-- markdown-toc start - Don't edit this section. Run M-x markdown-toc-generate-toc again -->
**Table of Contents**

 - [File synchronisation and deletion](#file-synchronisation-and-deletion)
 - [Wifi trigger](#wifi-trigger)
 - [Autoreload](#autoreload)
 - [Launchers](#launchers)
 - [Read status and other metadata](#read-status-and-other-metadata)
 - [Writing to the database](#writing-to-the-database)
 - [Database reverse-engineering](#database-reverse-engineering)
 - [Logging](#logging)
 - [Autobuilders](#autobuilders)
 - [Other readers](#other-readers)

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

One problem with this design is that it can take too long for
wallabako to realize wifi is up, or, inversly, it can take too long
for wifi to go online. Similarly, wifi can be turned off because
Nickel thinks it's idle, cutting off a long wallabako download.

The solution for the former could be to retry more aggressively and
for a longer time. Or we could use launchers, see below for that.

A solution for the latter issue would be more aggressive timeouts. We
should also be careful to transfer files atomically: right now we
write the files directly in their final destination, which means we
might leave incomplete files in place that wallabako will not retry.

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

Launchers
---------

One way to workaround the above started and reload issues would be to
hook Wallabako into a launcher like KSM or [kfmon][]. I have
experimented with the latter and it gives interesting results. To try
it out, do the following:

[kfmon]: https://github.com/NiLuJe/kfmon

 1. install [kfmon][]

 2. install wallabako normally (you at least need to have the
    `wallabako` programs in `/usr/local/bin` and a config file
    setup). Note that the kfmon configuration installed below assumes
    a newer wrapper script is present (`wallabako-run-direct`) which
    you might not already have installed.

 3. copy the `assets/logo-white.png` image to the Kobo root, for
    example:
    
        cp assets/logo-white.png /media/anarcat/KOBOeReader/wallabako.png

 4. copy and rename to `wallabako.ini` the `kfmon.ini` configuration file into the kfmon config
    directory:
    
        cp assets/kfmon.ini /media/anarcat/KOBOeReader/.adds/kfmon/config/wallabako.ini

 5. unmount the Kobo

If all goes well, the Wallabako logo should show up in your list of
books. When you tap it, wallabako should start automatically. This is
very nice because it allows you to control exactly *when* wallabako
will start. You might, for example, make sure wifi is properly started
and working before you start it.

The advantage of this approach is the user has total control on when
wallabako is started. The downside is it requires installing yet
another piece of software on your Kobo, which you might not be
familiar with.

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

Writing to the database
-----------------------

In order to fix problems with the [wifi trigger](#wifi-trigger), which we (ab)use
to make the reader detect our articles, we looked at writing to the
database directly. This would also allow us to [add books to
shelves/collections](https://gitlab.com/anarcat/wallabako/issues/20), which is more relevant now that [real authors
are propagated in the metadata](https://github.com/wallabag/wallabag/pull/3266) (so we can't find Wallabag articles
just by looking at at the author).

So we need to figure out:

 1. how to insert books
 2. how to remove books
 3. add books to shelves

To see if we could write to the SQLite database directly, we need to
run arbitrary SQL commands on the Kobo. And doing this through
Wallabako is a pain: the compile/deploy loop is rather slow. It would
be easier to have an actual SQL shell on the device, so we looked at 
compiling SQLite statically, but that turned out to be much harder
than expected, and didn't work at all. The next step was to create a
[custom sqlite shell using the golang bindings](https://github.com/mattn/go-sqlite3/pull/538) and that
worked. But in the end that work wasn't necessary, as there's is
already a nice commandline sql client called [usql](https://github.com/xo/usql) that operates
with many different databases.

It can be built for the Kobo with:

    env CGO_ENABLED=1 GOARCH=arm CC="arm-linux-gnueabihf-gcc-6" GOOS=linux go build -o usql.arm

Then we can list tables using this weird SQLite query:

    SELECT name FROM sqlite_master WHERE type='table';

Table schemas can be inspected with this `PRAGMA`:

    PRAGMA table_info(content);

But in the end, it's easier to just copy the SQLite database on a real
computer and inspect it directly. The client can still be used to
manipulate the tables, but that comes later. One thing that would be
useful would be a way to dump the database, but [usql doesn't support
that](https://github.com/xo/usql/issues/39) so I copied the database over my local workstation and
operated from there.

Then I dumped the database content's using:

    sqlite3 test.db .dump > dump.sql

The table schemas can similarly be extracted with the `.schema`
command. Then we make a modification, redo a copy + dump and check the
diff. Unfortunately, after thorough analysis of those diffs, it seems
that the GUI doesn't checkpoint into the SQLite database
reliably. Adding a collection, for example, doesn't show up in the
on-disk database until a reboot or `fake-usb-connect`. Same for books:
inserting a new entry in the `content` table has no effect on the
GUI. This makes sense from a power-saving perspective: all changes are
done in memory to save power since writing to the flash card might
need more current than writing to RAM. 

This all makes it impossible for us to use the database to effect
changes without going through, again, the `fake-usb-connect`
app. Collections/shelves will be basically impossible to implement as
we'd need to make Nickel stop holding on to the database while at the
same time making it *not* unmount `/mnt/onboard`, which seems
impossible.

One thing we *could* do would be to kill the `nickel` process and
force it to restart, but this would probably be even worse UI than the
current pop-up approach. This makes saner projects like [okreader](https://github.com/lgeek/okreader/)
much more interesting...

Database reverse-engineering
----------------------------

Nevertheless, I still did a short analysis using [sqlitebrowser](http://sqlitebrowser.org/) to
inspect the tables that seemed relevant. Diffs actually show changes
in `content`, `volume_shortcovers`, `AnalyticsEvents`, `Activity` and
`Event` when a book is added or removed, for what that's worth.

### `Shelf` table

```
CreationDate: ISO timestamp (TEXT)
Id: a63fb80d-f81c-419e-a5e1-0609a39b0371 (TEXT, uuid? primary key)
InternalName: label chosen by user (TEXT)
LastModified: ISO...
Name: = InternalName
Type: UserTag / SystemTag (TEXT)
_IsDeleted: false (BOOL)
_IsVisible: true (BOOL)
_IsSynced: true (BOOL)
_SyncTime: ISO...
LastAccessed: ISO...
```

### `ShelfContent` table

```
ShelfName: InternalName?
ContentID: = content.ContentID
DateModified: ISO...
_IsDeleted: false
_IsSynced: true
```

### `content` table

```
ContentID: file:///path or e.g. file:///path#(0)OEBPS/Cover2.html...
ContentType: 6 (main book entry) or 9 (chapters?)
MimeType: text/plain (.txt files) or application/epub+zip
BookID: NULL or ContentID for chapter entries
BookTitle: NULL or Title of main
ImageId: NULL or file____mnt_onboard_wallabako_1273_epub?
Title: book title or chapter title
Attribution: author? ("wallabag" or varies for later)
Description: Some articles saved on my wallabag
ShortCoverKey: NULL
adobe_location: OEBPS/Cover2.html
Publisher: NULL
IsEncrypted: false
DateLastRead: NULL or ISO when started
FirstTimeReading: true or false when started
ChapterIDBookmarked: NULL or "" for chapters or file...# path for started books
ParagraphBookmarked: 0
BookmarkWordOffset: 0
NumShortcovers: 1, 3, NULL...?
VolumeIndex: 0 for main book entry, or 0-based list of chapters
___NumPages: 0/-1?
ReadStatus: 0/1/2
___SyncTime: ISO
___UserID: adobe_user or localDocument for .txt file
PublicationId: NULL
___FileOffset: NULL or 0 for chapters
___FileSize: byte count or 0 for chapters?
___PercentRead: integer...
___ExpirationStatus: 0 or NULL for chapters
FavouritesIndex: -1
Accessibility: -1
ContentURL: "" or NULL?
Language: en or NULL?
BookshelfTags: "" or NULL?
IsDownloaded: true or 1??
FeedbackType: 0
AverageRating: 0
Depth: 0
PageProgressDirection: default for books, NULL for chapters
InWishlist: FALSE or false
ISBN: NULL or title??
WishlistedDate: all zero ISO date or NULL??
```

full dump of the table, interspersed with some of the values:

```
sq:/mnt/onboard/.kobo/KoboReader.sqlite=> PRAGMA table_info(content);
  cid |          name           |  type   | notnull |        dflt_value         | pk
+-----+-------------------------+---------+---------+---------------------------+----+
    0 | ContentID               | TEXT    |       1 | <nil>                     |  1
    1 | ContentType             | TEXT    |       1 | <nil>                     |  0
    2 | MimeType                | TEXT    |       1 | <nil>                     |  0
    3 | BookID                  | TEXT    |       0 | <nil>                     |  0
    4 | BookTitle               | TEXT    |       0 | <nil>                     |  0
    5 | ImageId                 | TEXT    |       0 | <nil>                     |  0
    6 | Title                   | TEXT    |       0 | <nil>                     |  0
    7 | Attribution             | TEXT    |       0 | <nil>                     |  0
    8 | Description             | TEXT    |       0 | <nil>                     |  0
    9 | DateCreated             | TEXT    |       0 | <nil>                     |  0
   10 | ShortCoverKey           | TEXT    |       0 | <nil>                     |  0
   11 | adobe_location          | TEXT    |       0 | <nil>                     |  0
   12 | Publisher               | TEXT    |       0 | <nil>                     |  0
   13 | IsEncrypted             | BOOL    |       0 | <nil>                     |  0
   14 | DateLastRead            | TEXT    |       0 | <nil>                     |  0
   15 | FirstTimeReading        | BOOL    |       0 | <nil>                     |  0
   16 | ChapterIDBookmarked     | TEXT    |       0 | <nil>                     |  0
   17 | ParagraphBookmarked     | INTEGER |       0 | <nil>                     |  0
   18 | BookmarkWordOffset      | INTEGER |       0 | <nil>                     |  0
   19 | NumShortcovers          | INTEGER |       0 | <nil>                     |  0
   20 | VolumeIndex             | INTEGER |       0 | <nil>                     |  0
   21 | ___NumPages             | INTEGER |       0 | <nil>                     |  0
   22 | ReadStatus              | INTEGER |       0 | <nil>                     |  0
   23 | ___SyncTime             | TEXT    |       0 | <nil>                     |  0
   24 | ___UserID               | TEXT    |       1 | <nil>                     |  0
   25 | PublicationId           | TEXT    |       0 | <nil>                     |  0
   26 | ___FileOffset           | INTEGER |       0 | <nil>                     |  0
   27 | ___FileSize             | INTEGER |       0 | <nil>                     |  0
   28 | ___PercentRead          | INTEGER |       0 | <nil>                     |  0
   29 | ___ExpirationStatus     | INTEGER |       0 | <nil>                     |  0
   30 | FavouritesIndex         |         |       1 |                        -1 |  0
   31 | Accessibility           | INTEGER |       0 |                         1 |  0
   32 | ContentURL              | TEXT    |       0 | <nil>                     |  0
   33 | Language                | TEXT    |       0 | <nil>                     |  0
   34 | BookshelfTags           | TEXT    |       0 | <nil>                     |  0
   35 | IsDownloaded            | BIT     |       1 |                         1 |  0
   36 | FeedbackType            | INTEGER |       0 |                         0 |  0
   37 | AverageRating           | INTEGER |       0 |                         0 |  0
   38 | Depth                   | INTEGER |       0 | <nil>                     |  0
   39 | PageProgressDirection   | TEXT    |       0 | <nil>                     |  0
   40 | InWishlist              | BOOL    |       1 | FALSE                     |  0
   41 | ISBN                    | TEXT    |       0 | <nil>                     |  0
   42 | WishlistedDate          | TEXT    |       0 | "0000-00-00T00:00:00.000" |  0
   43 | FeedbackTypeSynced      | INTEGER |       0 |                         0 |  0
0
   44 | IsSocialEnabled         | BOOL    |       1 | TRUE                      |  0
true
   45 | EpubType                | INT     |       1 |                        -1 |  0
-1
   46 | Monetization            | INTEGER |       0 |                         2 |  0
2
   47 | ExternalId              | TEXT    |       0 | <nil>                     |  0
NULL
   48 | Series                  | TEXT    |       0 | <nil>                     |  0
NULL
   49 | SeriesNumber            | TEXT    |       0 | <nil>                     |  0
NULL
   50 | Subtitle                | TEXT    |       0 | <nil>                     |  0
NULL
   51 | WordCount               | INTEGER |       0 |                        -1 |  0
-1
   52 | Fallback                | TEXT    |       0 | <nil>                     |  0
NULL
   53 | RestOfBookEstimate      | INTEGER |       0 | <nil>                     |  0
0 or NULL for chapters?
   54 | CurrentChapterEstimate  | INTEGER |       0 | <nil>                     |  0
0 or NULL for chapters?
   55 | CurrentChapterProgress  | FLOAT   |       0 | <nil>                     |  0
0.0 or NULL for chapters?
   56 | PocketStatus            | INTEGER |       0 |                         0 |  0
0
   57 | UnsyncedPocketChanges   | TEXT    |       0 | <nil>                     |  0
"" or NULL for chapters?
   58 | ImageUrl                | TEXT    |       0 | <nil>                     |  0
NULL or "", seemingly random
   59 | DateAdded               | TEXT    |       0 | <nil>                     |  0
NULL (!!!)
   60 | WorkId                  | TEXT    |       0 | <nil>                     |  0
NULL or "", seemingly random
   61 | Properties              | TEXT    |       0 | <nil>                     |  0
NULL or "", seemingly random, like WorkId
   62 | RenditionSpread         | TEXT    |       0 | <nil>                     |  0
NULL or "", seemingly random
   63 | RatingCount             | INTEGER |       0 |                         0 |  0
0
   64 | ReviewsSyncDate         | TEXT    |       0 | <nil>                     |  0
NULL
   65 | MediaOverlay            | TEXT    |       0 | <nil>                     |  0
NULL
   66 | MediaOverlayType        | TEXT    |       0 | <nil>                     |  0
NULL
   67 | RedirectPreviewUrl      | TEXT    |       0 | <nil>                     |  0
false or NULL for chapters?
   68 | PreviewFileSize         | INTEGER |       0 | <nil>                     |  0
0 or NULL for chapters?
   69 | EntitlementId           | TEXT    |       0 | <nil>                     |  0
"" or NULL for chapters?
   70 | CrossRevisionId         | TEXT    |       0 | <nil>                     |  0
NULL?
   71 | DownloadUrl             | TEXT    |       0 | <nil>                     |  0
false or NULL for chapters?
   72 | ReadStateSynced         | BIT     |       1 | false                     |  0
true or false for chapters
   73 | TimesStartedReading     | INTEGER |       0 | <nil>                     |  0
0 or NULL for chapters
   74 | TimeSpentReading        | INTEGER |       0 | <nil>                     |  0
0 or NULL for chapters
   75 | LastTimeStartedReading  | TEXT    |       0 | <nil>                     |  0
NULL
   76 | LastTimeFinishedReading | TEXT    |       0 | <nil>                     |  0
NULL
   77 | ApplicableSubscriptions | TEXT    |       0 | <nil>                     |  0
NULL
   78 | ExternalIds             | TEXT    |       0 | <nil>                     |  0
NULL
   79 | PurchaseRevisionId      | TEXT    |       0 | <nil>                     |  0
NULL
   80 | SeriesID                | TEXT    |       0 | <nil>                     |  0
NULL
   81 | SeriesNumberFloat       | REAL    |       0 | <nil>                     |  0
0.0 or NULL for chapters
   82 | AdobeLoanExpiration     | TEXT    |       0 | <nil>                     |  0
NULL
   83 | HideFromHomePage        | bit     |       0 | <nil>                     |  0
false or NULL for chapters
   84 | IsInternetArchive       | BOOL    |       1 | FALSE                     |  0
false or FALSE for chapters (!)
   85 | titleKana               | TEXT    |       0 | <nil>                     |  0
NULL
   86 | subtitleKana            | TEXT    |       0 | <nil>                     |  0
NULL
   87 | seriesKana              | TEXT    |       0 | <nil>                     |  0
NULL
   88 | attributionKana         | TEXT    |       0 | <nil>                     |  0
NULL
   89 | publisherKana           | TEXT    |       0 | <nil>
NULL
```

### `content_keys` table

```
volumeId: uuid?
elementId: part of a book?
elementKey: base64-encoded stuff?
```

### `content_settings` table

not relevant, as applied only to some books

### `volume_shortcovers` table 

may be relevant, has entries for all books...

### `volume_tabs` table

also mysterious

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

Other readers
-------------

Wallabako was designed mainly for the Nickel reader, or at least that
is how I have been using it so far. However, some other readers exist
for the Kobo machines. I have been particularly impressed by the
[koreader][] and [plato][] readers so I have tried to implement hooks
to recognize when a book is read in those platforms.

There is an [issue in the koreader tracker][] detailing the
possibility of hooking up wallabako directly in a koreader menu, but
this was abandoned. There's also a stub `readKoreaderStatus` function
in the code that should eventually read from the koreader state
files. The problem is parsing their silly Lua code, which I haven't
got around to.

There's also a `readPlatoStatus` function, split into its own
`plato.go` source file which should handle plato status changes. It
parses the Plato JSON file for state changes and will mark items as
"read" in wallabako (with all that implies, including removing files
if so configured) when the state and pattern matches. This has been
only slightly tested.

The first reader sending a "read" status wins, and they are read in
order (Plato, Koreader, Nickel). This implies that if a book is marked
as read in Plato but not Nickel, it might still be deleted from
Nickel, which is something to keep in mind. If it's marked as unread
in Plato but read in Nickel, it will also be deleted.

[issue in the koreader tracker]: https://github.com/koreader/koreader/issues/2621
[plato]: https://github.com/baskerville/plato/
[koreader]: https://github.com/koreader/koreader/

Static linking
--------------

Wallabako is, as much as possible, statically linked. That allows it
to get away with running on the Kobo with very minimal
dependencies. Unfortunately, since Debian 10 ("buster"), this
broke. The details of that debacle are documented in the CONTRIBUTING
document, but it certainly looks like it is going to become more and
more difficult to maintain Wallabako in the future.
