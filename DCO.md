# Developer Certificate of Origin

This project uses the [Developer Certificate of Origin 1.1](https://developercertificate.org/) instead of a Contributor License Agreement.

## How to sign

Add a `Signed-off-by:` trailer to every commit by passing `--signoff` (or `-s`) to `git commit`:

```bash
git commit --signoff -m "feat: (auth) wire OIDC middleware"
# or
git commit -s -m "..."
```

The trailer must match the email on your GitHub account. Configure once:

```bash
git config user.name "Your Name"
git config user.email "you@example.com"
```

To enable signoff by default in this repository:

```bash
git config commit.gpgsign true       # optional: also GPG-sign
git config format.signoff true
```

To retro-add the trailer to commits already on a feature branch:

```bash
# Last commit only
git commit --amend --signoff --no-edit

# All commits between this branch and main
git rebase --signoff main
git push --force-with-lease
```

## Enforcement

A required GitHub Check named `DCO` (provided by the [DCO GitHub App](https://github.com/apps/dco)) inspects every commit on every PR. A PR cannot merge until every commit carries a valid `Signed-off-by:` trailer matching a verified email on the contributor's GitHub account.

There is no signature file and no CLA workflow — the trailer in the commit message IS the signature.

## What you are certifying

Reproduced verbatim from <https://developercertificate.org/>:

```
Developer Certificate of Origin
Version 1.1

Copyright (C) 2004, 2006 The Linux Foundation and its contributors.

Everyone is permitted to copy and distribute verbatim copies of this
license document, but changing it is not allowed.


Developer's Certificate of Origin 1.1

By making a contribution to this project, I certify that:

(a) The contribution was created in whole or in part by me and I
    have the right to submit it under the open source license
    indicated in the file; or

(b) The contribution is based upon previous work that, to the best
    of my knowledge, is covered under an appropriate open source
    license and I have the right under that license to submit that
    work with modifications, whether created in whole or in part
    by me, under the same open source license (unless I am
    permitted to submit under a different license), as indicated
    in the file; or

(c) The contribution was provided directly to me by some other
    person who certified (a), (b) or (c) and I have not modified
    it.

(d) I understand and agree that this project and the contribution
    are public and that a record of the contribution (including all
    personal information I submit with it, including my sign-off) is
    maintained indefinitely and may be redistributed consistent with
    this project or the open source license(s) involved.
```

## Why DCO instead of CLA

DCO is what the Linux kernel, Docker, GitLab, etcd, Kubernetes (in spirit, via CNCF), and most projects of this size use. It establishes the same legal facts a CLA does — contributor has rights to submit, project may use the work — without external signature storage, without a SaaS dependency, and without a workflow that fights branch protection. The trailer is the contract; `git log` is the audit record.
