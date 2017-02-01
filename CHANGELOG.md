This is a summary of changes in the published releases of
Wallabako. The format of this change may change without prior notice.

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
