# Kobodeck

A minimalist article downloader for Kobo devices.
It can

- fetch content from a **Readeck instance**
- sync some properties (read/archived/favorite) from the **Kobo device** to Readeck


```mermaid
flowchart LR
    K[Kobo Device]
    R[Readeck Instance]

    R -->|unread articles| K
    K -->|archive completed articles| R
    K -->|mark favourites| R
```

The project is forked from
[wallabako](https://gitlab.com/anarcat/wallabako).

## who is this for ?

This plugin could be useful for you if you

- do not want to use [KOReader](https://koreader.rocks/), which has a native
  Readeck/OPDS plugin or [Plato](https://github.com/baskerville/plato/), which
  includes an article fetcher
- are ok with mixing ebooks and articles in the native Kobo UI — if you want to
  keep them separate, check out [kobeck](https://github.com/Lukas0907/kobeck)
- are fine with a lack of ui: syncing happens in the background

## how to use it

When wifi is turned on, kobodeck connects to your Readeck instance in the
background, downloads new unread articles as KEPUBs with cover images, and
syncs read status back to Readeck.

If any files changed, it triggers a fake USB
connection to prompt the reader to rescan the library.
Press **Connect** to rescan
immediately, or **Cancel** — the files are already downloaded either way.

![screenshot of the connect dialog on a Kobo Glo HD reader](assets/connect-dialog.png)

## Installation or Upgrade

To install or upgrade

1. obtain the latest `KoboRoot.tgz` either by downloading the binary or
   by building from source via `make tarball`
1. save the file in the `.kobo` directory of your e-reader
1. edit the configuration file [`.kobodeck.toml`](.kobodeck.toml)
1. optionally verify your configuration with
   `kobodeck --config .kobodeck.toml --check`
1. store the `.kobodeck.toml` in the root of your kobo device
1. safely disconnect the reader - it should restart, install kobodeck and remove
   `KoboRoot.tgz`

## Uninstalling

Delete `.kobodeck.toml` and connect to wifi.

### Manual uninstall

Manual removal of the files deployed by `KoboRoot.tgz`
requires root access to the device.
The following files need to be deleted:

```text
etc/udev/rules.d/90-kobodeck.rules
usr/local/bin/kobodeck
usr/local/bin/fake-connect-usb
usr/local/bin/kobodeck-run
```

## Development

Check the Makefile for common operations on the project.

### Updating the Nickel schema

The integration tests use schema files in `testdata/` named `nickel-schema-{version}.sql`,
where `{version}` is the `DbVersion` from the `KoboReader.sqlite` database.
After a firmware update that changes the database schema, dump the new schema with:

```sh
DB=/media/$USER/KOBOeReader/.kobo/KoboReader.sqlite
VER=$(sqlite3 "$DB" "SELECT version FROM DbVersion;")
sqlite3 "$DB" ".schema" > testdata/nickel-schema-${VER}.sql
```

### Future work

- Sync highlights and annotations from the Kobo (`Bookmark` table in
  `KoboReader.sqlite`) to Readeck's annotations API
- Add sync of reading progress (current position) from the Kobo to Readeck —
  note that progress may differ between EPUB and KEPUB formats
- Add functionality to also fetch archived articles
- Add functionality to fetch favourites only
- Syncing is currently only one way, as we avoid writing to Kobo's NickelDB — reverse
  sync may still be worth exploring
- The run script does not inhibit device sleep — if the Kobo sleeps during a
  long sync, downloads may be interrupted
