# Readeck Article Downloader for Kobo Devices

**kobodeck** is a minimalist article downloader for Kobo devices.

It can

- fetch content from a **Readeck instance**
- sync its status (read/archived) from the **Kobo device** to the Readeck server

The code is forked from
[wallabako](https://gitlab.com/anarcat/wallabako).

## who is this for ?

This plugin could be useful for you if you

- do not want to use [KOReader](https://koreader.rocks/), which has a native
  Readeck/OPDS plugin or [Plato](https://github.com/baskerville/plato/), which
  includes an article fetcher
- are ok with mixing ebooks and articles in the native Kobo UI — if you want to
  keep them separate, check out [kobeck](https://github.com/Lukas0907/kobeck)
- are fine with a lack of ui: content fetching and downloads happen in the
  background

## how to use it

When wifi is turned on, kobodeck connects to your Readeck instance in the
background, downloads new unread articles as EPUBs, and syncs read status
back to Readeck.

If any files changed, it triggers a fake USB
connection to prompt Nickel to rescan the library.
Press **Connect** to rescan
immediately, or **Cancel** — the files are already downloaded either way.

![screenshot of the connect dialog on a Kobo Glo HD reader](assets/connect-dialog.png)

## Installation or Upgrade

To install or upgrade,

1. obtain the latest `KoboRoot.tgz` either by downloading the binary or
   by building from source via `make tarkball`
1. save the file in the `.kobo` directory of your e-reader
1. copy and edit the configuration file [`.kobodeck.toml`](root/etc/kobodeck.toml)
1. optionally verify your configuration with
   `kobodeck --config .kobodeck.toml --check`
1. store the `.kobodeck.toml` in the root of your kobo device
1. safely disconnect the reader; it should restart, install kobodeck and remove
   `KoboRoot.tgz`

## Uninstalling

Set `Uninstall = true` in `.kobodeck.toml` and connect to wifi. kobodeck will
remove itself and exit.

### Manual uninstall

Manual removal of the files deployed by `KoboRoot.tgz`
requires root access to the device, for example via
[niluje's usbnet](https://www.mobileread.com/forums/showthread.php?t=254214),
which provides SSH over USB.

Once you have access, remove:

```text
etc/udev/rules.d/90-kobodeck.rules
etc/kobodeck.toml
usr/local/bin/kobodeck
usr/local/bin/fake-connect-usb
usr/local/bin/kobodeck-run
```

Removing `/etc/udev/rules.d/90-kobodeck.rules` is enough to prevent kobodeck
from running automatically on network changes.

## Development

Check the Makefile for common operations on the project.

### Known issues

- The integration test creates the Nickel SQLite database with a
  minimal 3-column schema. It should instead use the full real Kobo
  schema (stored as `testdata/nickel-schema.sql`) so that tests fail
  if the code makes assumptions that break on a schema change after a
  firmware update.
- Sync favourite/starred status from the Kobo to Readeck (in addition to
  read status).
- Sync highlights and annotations from the Kobo (`Bookmark` table in
  `KoboReader.sqlite`) to Readeck's annotations API.
- Sync reading progress (current position) from the Kobo to Readeck,
  once Readeck exposes a progress field in its API.
- Preview images are not displayed in EPUBs — this is a Readeck issue
  and needs to be fixed upstream.
