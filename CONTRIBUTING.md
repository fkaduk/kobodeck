This document outlines how to contribute to this project. It details a
code of conduct, how to submit issues, bug reports and patches.

 [GitLab project]: https://gitlab.com/anarcat/wallabako/
 [merge requests]: https://gitlab.com/anarcat/wallabako/merge_requests
 [issues]: https://gitlab.com/anarcat/wallabako/issues
 [docs-editor]: https://gitlab.com/anarcat/wallabako/edit/master/README.md
 [forum]: https://www.mobileread.com/forums/showthread.php?p=3467945

<!-- for people reusing this in their own project, you will need to -->
<!-- modify the Contacts section for the code of conduct. you will -->
<!-- also want to read more about code of conducts and community -->
<!-- guidelines before adopting it, it's not just a rubber -->
<!-- stamp. other sections, like the Design notes and Documentation -->
<!-- may need to be adapted. the above URLs will also need to be -->
<!-- changed, obviously. -->

<!-- markdown-toc start - Don't edit this section. Run M-x markdown-toc-generate-toc again -->
**Table of Contents**

- [Contributor Covenant Code of Conduct](#contributor-covenant-code-of-conduct)
- [Patches](#patches)
- [Documentation](#documentation)
- [Issues and bug reports](#issues-and-bug-reports)
- [Membership](#membership)
- [Release process](#release-process)

<!-- markdown-toc end -->

# Contributor Covenant Code of Conduct

## Our Pledge

In the interest of fostering an open and welcoming environment, we as
contributors and maintainers pledge to making participation in our project and
our community a harassment-free experience for everyone, regardless of age, body
size, disability, ethnicity, gender identity and expression, level of experience,
nationality, personal appearance, race, religion, or sexual identity and
orientation.

## Our Standards

Examples of behavior that contributes to creating a positive environment
include:

* Using welcoming and inclusive language
* Being respectful of differing viewpoints and experiences
* Gracefully accepting constructive criticism
* Focusing on what is best for the community
* Showing empathy towards other community members

Examples of unacceptable behavior by participants include:

* The use of sexualized language or imagery and unwelcome sexual attention or
advances
* Trolling, insulting/derogatory comments, and personal or political attacks
* Public or private harassment
* Publishing others' private information, such as a physical or electronic
  address, without explicit permission
* Other conduct which could reasonably be considered inappropriate in a
  professional setting

## Our Responsibilities

Project maintainers are responsible for clarifying the standards of acceptable
behavior and are expected to take appropriate and fair corrective action in
response to any instances of unacceptable behavior.

Project maintainers have the right and responsibility to remove, edit, or
reject comments, commits, code, wiki edits, issues, and other contributions
that are not aligned to this Code of Conduct, or to ban temporarily or
permanently any contributor for other behaviors that they deem inappropriate,
threatening, offensive, or harmful.

## Scope

This Code of Conduct applies both within project spaces and in public spaces
when an individual is representing the project or its community. Examples of
representing a project or community include using an official project e-mail
address, posting via an official social media account, or acting as an appointed
representative at an online or offline event. Representation of a project may be
further defined and clarified by project maintainers.

## Enforcement

Instances of abusive, harassing, or otherwise unacceptable behavior may be
reported by contacting one of the persons listed below. All
complaints will be reviewed and investigated and will result in a response that
is deemed necessary and appropriate to the circumstances. The project maintainers is
obligated to maintain confidentiality with regard to the reporter of an incident.
Further details of specific enforcement policies may be posted separately.

Project maintainers who do not follow or enforce the Code of Conduct in good
faith may face temporary or permanent repercussions as determined by other
members of the project's leadership.

Project maintainers are encouraged to follow the spirit of the
[Django Code of Conduct Enforcement Manual][enforcement] when
receiving reports.

 [enforcement]: https://www.djangoproject.com/conduct/enforcement-manual/

## Contacts

The following people have volunteered to be available to respond to
Code of Conduct reports. They have reviewed existing literature and
agree to follow the aforementioned process in good faith. They also
accept OpenPGP-encrypted email:

 * Antoine Beaupré <anarcat@debian.org>

## Attribution

This Code of Conduct is adapted from the [Contributor Covenant][homepage], version 1.4,
available at [http://contributor-covenant.org/version/1/4][version]

[homepage]: http://contributor-covenant.org
[version]: http://contributor-covenant.org/version/1/4/

Changes
-------

The Code of Conduct was modified to refer to *project maintainers*
instead of *project team* and small paragraph was added to refer to
the Django enforcement manual.

> Note: We have so far determined that writing an explicit enforcement
> policy is not necessary, considering the available literature
> already available online and the relatively small size of the
> community. This may change in the future if the community grows
> larger.

# Positive feedback

Even if you have no changes, suggestions, documentation or bug reports
to submit, even just positive feedback like "it works" goes a long
way. It shows the project is being used and gives instant
gratification to contributors. So we welcome emails that tell us of
your positive experiences with the project or just thank you
notes.

You can also send your "thanks" through
[saythanks.io](https://saythanks.io/to/anarcat).

[![Say Thanks!](https://img.shields.io/badge/Say%20Thanks-!-1EAEDB.svg)](https://saythanks.io/to/anarcat)

# Patches

Patches can be submitted through [merge requests][] on the
[GitLab project][].

Some guidelines for patches:

* A patch should be a minimal and accurate answer to exactly one
  identified and agreed problem.
* A patch must compile cleanly and pass project self-tests on all
  target platforms.
* A patch commit message must consist of a single short (less than 50
  characters) line stating a summary of the change, followed by a
  blank line and then a description of the problem being solved and
  its solution, or a reason for the change. Write more information,
  not less, in the commit log.
* Patches should be reviewed by at least one maintainer before being merged.

Project maintainers should merge their own patches only when they have been
approved by other maintainers, unless there is no response within a
reasonable timeframe (roughly one week) or there is an urgent change
to be done (e.g. security or data loss issue).

As an exception to this rule, this specific document cannot be changed
without the consensus of all administrators of the project.

> Note: Those guidelines were inspired by the
> [Collective Code Construct Contract][C4]. The document was found to
> be a little too complex and hard to read and wasn't adopted in its
> entirety. See this [discussion][] for more information.

 [C4]: https://rfc.zeromq.org/spec:42/C4/
 [discussion]: https://github.com/zeromq/rfc/issues?utf8=%E2%9C%93&q=author%3Aanarcat%20

## Patch triage

You can also review existing pull requests, by cloning the
contributor's repository and testing it. If the tests do not pass
(either locally or in the online Continuous Integration (CI) system),
if the patch is incomplete or otherwise does not respect the above
guidelines, submit a review with "changes requested" with reasoning.

# Documentation

We love documentation!

The documentation mostly in the README file and can be
[edited online][docs-editor] once you register. The
[discussion on MobileRead.com][forum] may also be a good place to get
help if you need to.

# Issues and bug reports

We want you to report issuess you find in the software. It is a
recognized and important part of contributing to this project. All
issues will be read and replied to politely and
professionnally. Issues and bug reports should be filed on the
[issue tracker][issues].

## Issue triage

Issue triage is a useful contribution as well. You can review the
[issues][] in the GitHub project and, for each issue:

-  try to reproduce the issue, if it is not reproducible, label it with
   `more-info` and explain the steps taken to reproduce
-  if information is missing, label it with `more-info` and request
   specific information
-  if the feature request is not within the scope of the project or
   should be refused for other reasons, use the `wontfix` label and
   close the issue
-  mark feature requests with the `enhancement` label, bugs with
   `bug`, duplicates with `duplicate` and so on...

Note that some of those operations are available only to project
maintainers, see below for the different statuses.

# Membership

There are three levels of membership in the project, Administrator
(also known as "Owner" in GitHub or GitLab), Maintainer (also known as
"Member" on GitHub or "Developer" on GitLab), or regular users
(everyone with or without an account). Anyone is welcome to contribute
to the project within the guidelines outlined in this document,
regardless of their status, and that includes regular users.

Maintainers can:

* do everything regular users can
* review, push and merge pull requests
* edit and close issues

Administrators can:

* do everything maintainers can
* add new maintainers
* promote maintainers to administrators

Regular users can be promoted to maintainers if they contribute to the
project, either by participating in issues, documentation or pull
requests.

Maintainers can be promoted to administrators when they have given significant
contributions for a sustained timeframe, by consensus of the current
administrators. This process should be open and decided as any other issue.

# Building

Even though wallabako is a golang program, it has a `Makefile` to
build the peculiar stuff Kobo readers expect from it. In particular,
the build should produce an ARM binary inside a specifically crafted
tar archive.

In order to build wallabako, you will need git, GCC and Golang:

    apt install golang golint git gcc-arm-linux-gnueabihf make pv

Note that Kobo readers might be running a glibc and kernel too old for
your platform, see below if you get weird error messages when building
from versions greater than Debian 9 (stretch), with glibc 2.28 or
later.

You will also need the golang dependencies:

    export GOPATH=~/go
    go get -d gitlab.com/anarcat/wallabako

The release process (below) uses the `deploy` target as part of an
ad-hoc test suite, but if you do not have a Kobo with SSH access to
test it, you can also just use this to build the right tar file and
deploy it another way:

    make -C ~/go/src/gitlab.com/anarcat/wallabako tarball

## Building on Debian versions greater than 9 (stretch)

Starting from Debian 10 "buster", things get a little harder for
wallabako because the Kobo platform is *so* old. Out of the box,
running the binary built there results in the following error:

    /lib/libc.so.6: version `GLIBC_2.28' not found (required by /usr/local/bin/wallabako)

This was fixed by adding extra flags to the linker (`-linkmode
external -extldflags "-static"`), but then it still fails with the
following:

    FATAL: kernel too old

... which is pretty dramatic and, unfortunately, kind of true: the
Kobo Glo HD, for example, runs a `3.0.35+` kernel ([released in
2012](https://lkml.org/lkml/2012/6/17/107) that was compiled in December 2016.

I have found it wasn't sufficient to build in a stretch `chroot`: the
golang build process then also expects a certain version of the
kernel. I had to use a Debian 9 stretch virtual machine (with Vagrant)
to get a working binary from Debian 10 buster, unfortunately.

# Release process

To make a release:

 1. generate and commit release notes with:

        git changelog && editor CHANGELOG.md && git commit -a

    the file header will need to be moved back up to the beginning of
    the file. also make sure to add a summary and choose a proper
    version according to [Semantic Versioning][]

 2. check source code for errors, and deploy to test host:

        make lint deploy HOST=192.168.0.22

 3. make sure everything works: test the program on a desktop and a
    Kobo reader

 4. tag the release according to [Semantic Versioning][] rules:

        git tag -s x.y.z

 5. rebuild with the new tag:

        rm -rf build && make lint sign deploy HOST=192.168.0.22

 5. push changes:

        git push

 6. edit the [tag on Gitlab][], copy-paste the changelog entry and
    attach the signed binaries

 7. if you are happy with the release, update the README file to point
    to the new tag

The latter step was adopted after the builds broke without me
noticing, which broke the download links.

 [Semantic Versioning]: http://semver.org/
 [tag on Gitlab]: https://gitlab.com/anarcat/wallabako/tags
