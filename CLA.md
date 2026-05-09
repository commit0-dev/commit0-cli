# Contributor License Agreement

Thank you for your interest in contributing to this project. By submitting any contribution to this repository (a "**Contribution**"), you accept the terms of this Contributor License Agreement (the "**Agreement**") with the maintainers of the [commit0-dev](https://github.com/commit0-dev) organization (the "**Project**").

You only need to sign this Agreement once per GitHub account; the signature applies to all your present and future Contributions to any repository owned by **commit0-dev**.

## 1. Definitions

- **Contribution** — any source code, documentation, configuration, test, design, or other work of authorship that you submit to the Project, in any form (Pull Request, Issue, comment, patch, or otherwise).
- **You** / **Contributor** — the individual or legal entity submitting the Contribution. If you are submitting on behalf of an entity, "You" includes that entity, and you represent that you are authorised to bind it to this Agreement.
- **Commercial Product** — any product, service, or offering — including hosted services, proprietary extensions, and commercial editions — that the Project's maintainers may build, distribute, license, or sell, whether under the Apache License 2.0 or under different terms.

## 2. Grant of Copyright License

Subject to the terms of this Agreement, You hereby grant to the Project's maintainers and to recipients of software distributed by the Project a perpetual, worldwide, non-exclusive, no-charge, royalty-free, irrevocable copyright licence to:

(a) reproduce, prepare derivative works of, publicly display, publicly perform, sublicense, and distribute Your Contribution and such derivative works under the terms of the [Apache License 2.0](LICENSE) (or a compatible successor licence); **and**

(b) **incorporate Your Contribution into Commercial Products** of the Project's maintainers under any licence terms the maintainers choose, provided that the Apache 2.0-licensed open-source version of the Project remains publicly available.

Sub-clause (b) is the key dual-licensing carve-out that distinguishes this Agreement from a pure DCO sign-off. By signing, You consent to the Project's maintainers using Your Contribution in non-open-source commercial offerings.

## 3. Grant of Patent License

Subject to the terms of this Agreement, You hereby grant to the Project's maintainers and to recipients of software distributed by the Project a perpetual, worldwide, non-exclusive, no-charge, royalty-free, irrevocable (except as stated in this Section) patent licence to make, have made, use, offer to sell, sell, import, and otherwise transfer Your Contribution. The licence applies only to those patent claims licensable by You that are necessarily infringed by Your Contribution alone or by combination of Your Contribution with the Project to which You submitted it.

If any entity institutes patent litigation against You or any other entity (including a cross-claim or counterclaim) alleging that Your Contribution, or the work to which You contributed, constitutes direct or contributory patent infringement, then any patent licences granted to that entity under this Agreement for that Contribution or work shall terminate as of the date such litigation is filed.

## 4. Your Representations

By submitting a Contribution, You represent that:

(a) **Authorship.** You have authored 100% of the Contribution, or You have the necessary rights to submit it under the terms of this Agreement.

(b) **Employer permission.** If Your employer has any rights to intellectual property You create that includes the Contribution, You have received permission to make this Contribution on behalf of that employer, or Your employer has waived such rights.

(c) **Truthfulness.** Each Contribution accurately represents Your work, including any third-party material You have incorporated and which is licensed under terms compatible with this Agreement. You agree to identify the complete details of any third-party licence or other restriction associated with any part of Your Contribution.

(d) **Disclosure.** You will notify the Project's maintainers of any facts or circumstances of which You become aware that would make these representations inaccurate.

## 5. No Warranty

Your Contribution is provided "AS IS", without warranty of any kind, express or implied, including but not limited to warranties of merchantability, fitness for a particular purpose, or non-infringement. You are not expected to provide support for Your Contribution, except to the extent You desire to provide support.

## 6. How to Sign

This Agreement is signed automatically through [CLA Assistant](https://cla-assistant.io). When You open Your first Pull Request to a commit0-dev repository, CLA Assistant posts a comment with a link; click the link, authorise with Your GitHub account, and confirm. The signature is recorded once and applies to all Your subsequent Contributions across the organisation.

## 7. Companion Attestation: DCO

In addition to this Agreement, every commit You submit must include a `Signed-off-by:` trailer attesting to the [Developer Certificate of Origin 1.1](DCO.md). The DCO is a per-commit attestation that You have the right to submit that specific commit; this CLA is a per-contributor licence grant covering all Your Contributions. **Both are required.**

To add the trailer automatically, pass `-s` (or `--signoff`) on every `git commit`:

```bash
git commit -s -m "<your commit message>"
```

If You forget on a commit:

```bash
git commit --amend --signoff --no-edit         # last commit only
git rebase --signoff <base-branch>             # whole branch
git push --force-with-lease
```

The required `DCO` GitHub Check (provided by the [DCO GitHub App](https://github.com/apps/dco)) blocks merge until every commit on the PR carries a valid trailer.

## 8. Questions

Open an Issue on this repository for any question about this Agreement before signing. Maintainers will respond before requiring You to commit.

---

*This Agreement is licensed under [CC BY 4.0](https://creativecommons.org/licenses/by/4.0/); You may adapt it for Your own projects with attribution.*
