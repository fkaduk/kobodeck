This is a summary of changes in the published releases of
Wallabako. The format of this change may change without prior notice.

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
   default (usually 30 articles), can be overriden with the `-count`
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
