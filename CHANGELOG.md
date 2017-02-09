This is a summary of changes in the published releases of
Wallabako. The format of this change may change without prior notice.

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
