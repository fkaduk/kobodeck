# 1.6.0 / 2024-10-24: Here's your future

Lots of small changes that piled up in the last year.

Features:

  * readd the -logfile parameter
  * report the number of unread entries near the end of the log too
  * stop yelling about missing nickel or plato configurations
  * trigger the fake-connect-usb script only if nickel is running
  * disable fbink by default

Bug fixes:

  * reduce default concurrency drastically
  * README: fix typo in configuration file sample

Chores:

  * Added renovate-bot to the pipeline, which generates lots of
    changes and noises in the Git history, mostly redacted here
  * Port to wallabago 9
  * Update all dependencies to latest

# 1.5.1 / 2024-03-26: Everything in its right place

Maintenance release. Uncertain attempt at fixing `-config` command
line flag, feedback welcome.

Bug fixes:

  * fix -config parameter (#29)
  * fix on-screen display when called automatically

# 1.5.0 / 2023-06-10: Here it goes again

First release in over a year, and a major feature release! We have on
screen display now!

If you have [fbink][] installed on your Kobo reader, you'll get visual
information displayed as wallabako is doing its thing. A billion
thanks to [NiLuJe][] for this awesome program.

Features:

 * on screen display
 * regrouped version information, timing information, and counters
   (downloaded/deleted/read/etc) in a single line
 * improved timing output (millisecond precision instead of too
   verbose nanosecond)

Bug fixes:

 * fix spelling errors found with codespell, hooked in CI to avoid
   future problems
 * don't fail on strip failures during build

Documentation:

 * document other concurrency solutions
 * tweak release process to remove futile step of keeping the latest
   tag in the README links, we're just linking to the latest code now,
   if you want the last release, just go to the release page already

[NiLuJe]: https://github.com/NiLuJe/FBInk
[fbink]: https://github.com/NiLuJe/FBInk

# 1.4.1 / 2022-05-06: Hot rats

Hotfix release to strip binaries that have grown beyond the GitLab.com
10MB limit. No other change, should be functionally similar to the
previous release.

# 1.4.0 / 2022-05-06: The Message

Ship a lot of fixes that have accumulated in the 3 years since the
last release.

Features:

  * add timestamp and git version to build artifacts
  * cleanup and improve debugging output
  * switch to pure go sqlite implementation, which helps 
  * update all module dependencies
  * port to wallabago v6
  * support Plato library changes from 0.8.5+
  * support reading koreader progress/read status
  * Allow containerized builds, use gomod and avoid GOPATH hell
  * overhaul Dockerfile
  * switch to `go mod`

Documentation changes:

  * remove instability warning: this works well enough
  * README: replace branch name master by main in links
  * tweak mention of libreoffice to clarify concern
  * replace "kobo" references by "nickel" where appropriate
  * make a section about related projects
  * mention NickelMenu
  * quick review of the koreader implementation

Bugfixes:

  * handle errors in http request creation
  * Use OutputDir configuration instead of hardcoded wallabako paths
  * do not noisily fail if there's no entry for book in plato
  * regression: properly detect read status again after koreader (or
    plato?) support was added

# 1.3.1 / 2019-10-04: Agnus dei

Bugfix release to make read but untouched files be deleted again.

  * add Vagrantfile to build on older Debian releases more easily
  * simplify plato status check logic
  * support "in reading" state in plato, hopefully
  * fix go dep in CI

# 1.3.0 / 2019-09-26: Our Collective Consciousness' Psychogenic Fugue

Minor release to ship patches accumulated in the last year. This
release is the first shipping with support for other readers than the
builtin "Nickel" reader, mainly Plato for now, but work has also been
done to support Koreader. Some tests have been done to support kfmon
and other launchers as well.

  * abstract Kobo logic from read status and prepare support for others
  * add Plato metadata parsing code
  * add kfmon.ini configuration and white-background logo of right size
  * fix compilation in golang 1.11 and 1.12
  * do not hardcode the gcc version, in the futile hope it still works
  * documentation improvements

# 1.2.1 / 2018-04-12: Never flush a tampon

Patch release to fix problem that would make any Wallabag annotation
crash wallabako.

  * update to wallabago v4 to fix change in wallabag annotations

# 1.2.0 / 2018-03-13: Giving up on writing

Minor release to ship patches accumulated in the last 9 months.

  * add incomplete uninstall instructions
  * add tag filtering support, thanks to Bogdan Cordier
  * add a fully-populated config file to README
  * add "say thanks", go report card and pipeline badges
  * basic port to wallabago 2.0 library
  * follow latest golang releases
  * start using godep for dependency management
  * fix linting in CI
  * research the database format, abandon writing to the database
    which means no collection/shelf support will be possible on Kobo
    readers

# 1.1.1 / 2017-06-20: A piece of strange

Merge changes from stable branch, including:

  * fix build with latest wallabago API changes

# 1.1.0 / 2017-03-07: Lost somewhere in time

This minor release was shipped to tag a bunch of changes that have
been piling up since the last release, 4 months ago. Mostly
documentation fixes, but also a small fix to better support the 2.2
API and allow for betting debugging output.

 * documentation improvements:
  * add TOC in troubleshooting section
  * document the database warning errors
  * document configuration file issues
  * move design notes to a separate document
  * add table of contents
  * add contribution guidelines
  * move known issues to the gitlab issue queue
  * add note about hardcoded settings
 * add -debug flag and configuration option
 * Dynamic path in CI script to work with forks
 * preliminary 2.2 API support:
  * Make proper JSON requests to wallabag server

Thanks to Martin Trigaux for his contributions in this release!

# 1.0.2 / 2017-06-20: L'ombre sur la mesure

  * fix build with latest wallabago API changes

# 1.0.1 / 2017-06-20: La rumeur

Small bugfix release to help with 2.2 API without breaking backwards
compatibility.

  * Make JSON requests to wallabag server

# 1.0.0: Finally somewhere

This major release features complete configuration file
support. Settings like `LogFile` can now be written directly into the
`JSON` configuration file. Logs, by default, are now disabled as they
do not seem as useful anymore since things generally work well, hence
the 1.0 release.

This release fixes a bunch of issues:

 * extended configuration file support: logfiles, deletion,
   parallelism can now all be configured in the configuration file,
   see README for details
 * do not delete articles by default: it causes spurious
   triggers. this can be enabled again by adding the `Delete` setting
   with a `true` value (note: no quotes) in the configuration file
 * do not write a logfile by default: this takes up too much space and
   doesn't seem very necessary anymore. this can be re-enabled by
   using the `LogFile` parameter in the configuration file, set, for
   example, to `/mnt/onboad/wallabako.log.txt`.
 * drop support for the `-logfile` commandline flag, use shell
   redirection instead
 * slow builds are now fixed now that the docker images have been
   updated

# 0.9: Run forest

Lots of attempts to fix sync that was becoming increasingly unreliable.

 * re-enable background processing which was disabled by mistake in
   0.4
 * increase delay to 15 seconds to try and fix sync issues
 * try to remount internal drive if it's not remounted when we finish
 * display human-readable elapsed time
 * close database properly when completed
 * write logs in /root/wallabako.log instead of storage
 * output on console as well as logfile
 * logfile rotation

# 0.8: Stop don't do it

 * trigger wallabako only when the interface goes back up
 * make version number less verbose for released versions
 * make sizes human-readable
 * handle download errors better

# 0.7: Call of Chtulu

 * add the `.txt` extension to logfiles so that they are
visible from the e-reader to improve debugging
 * improve documentation significantly
 * deal with corrupt JSON files better
 * add -version flag to show version
 * show version when we exit normally as well

This release is the direct result of hands-on usability testing with a
non-technical user that gave great feedback. Thanks!

# 0.6: Look out honey, cause I'm using technology!

This feature release now will propagate read status to the Wallabag
instance: your books marked as read on the e-reader will be marked as
read on Wallabag as well! We also improve on the CI build time by
using the [new upstream stretch images][] which also means we're now
running with the cutting-edge Go 1.8 version.

[new upstream stretch images]: https://github.com/docker-library/official-images/issues/2639

# 0.5: safety and liberty

This feature release starts looking into the Kobo database to see if a
book is being read. If it is being read, it will not delete it.

The next step is obviously to propagate the read status to the
Wallabag instance, which is not done yet.

# 0.4: perfection is the ennemy of good

This is a small bugfix and documentation improvements release. This
release should deal better with variable connection delays, as it can
wait up to about 30 seconds.

# 0.3: practice makes perfect

Important bugfix release to deploy the correct binary but also changes
location of files. Previously installed file will *not* be
erased. This should have limited impact as the files were taking only
5MB on the system partition.

But if you want to clean up those files, you will need to hack your
Kobo reader and run the following command:

    rm -r /usr/local/wallabako

Detailed changes:

 * use standard locations for programs (`/usr/local/bin`) instead of
   our custom path (`/usr/local/wallabako`)
 * deploy the ARM binary properly (0.2 was deploying the x86_64 binary)
 * logfile support, should be visible in `wallabako.log` in the top
   level directory of the reader
 * information improvements: notify the user we are sleeping, etc
 * delete old files by default: to get back to the old behaviour, you
   need to edit `wallabag-run` to remove the `-delete` flag
 * do not limit ourselves to 10 entries, but instead rely on the site
   default (usually 30 articles), can be overridden with the `-count`
   flag in the `wallabag-run` file

# 0.2: don't delete that file

This is a small bugfix release. It turns out that -delete was always
enabled, even if the flag was not specified. Oops. Deleted files also
didn't trigger a reload of the database, so now we count the number of
deleted files, show the user, and properly execute the notify hook
when files are deleted.

# 0.1: while my server gently weeps

This release, exceptionally [performed on Github][] because of a [major
outage at Gitlab][] is the first release of Wallabago. It ships with a
tentative KoboRoot.tgz that is still untested.

[major outage at Gitlab]: https://twitter.com/gitlabstatus/status/826591961444384768
[performed on Github]: https://github.com/anarcat/wallabako/releases/tag/0.1

More information in the README file.
