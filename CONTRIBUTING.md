# Contributing to commit0-cli

Thanks for your interest in contributing to commit0. Please read the rules below before opening a Pull Request — they are HARD requirements enforced by automation, not guidelines.

For the full conventions (branch prefixes, PR title format, commit message format, identifier naming, ROADMAP discipline, pre-merge gate), see [docs/CONVENTIONS.md](docs/CONVENTIONS.md).

## Signing your work

Every commit must carry a `Signed-off-by:` trailer. The trailer is your attestation to the [Developer Certificate of Origin](DCO.md) — by adding it, you certify you have the right to submit the contribution under the project's open-source license.

```bash
git commit -s -m "<your commit message>"
```

The `-s` flag appends the trailer using your configured `user.name` and `user.email`. The email must be verified on your GitHub account.

If you forgot:

```bash
git commit --amend --signoff --no-edit                     # last commit only
git rebase --signoff main && git push --force-with-lease   # whole branch
```

The required `DCO` check will block merge until every commit on the PR carries a valid trailer.

## Contributor License Agreement (CLA)

In addition to the per-commit DCO attestation above, every contributor must sign the project's [Contributor License Agreement](CLA.md) once per GitHub account.

The CLA grants commit0-dev's maintainers the rights needed to incorporate your contributions into both the open-source distribution **and** any commercial products the maintainers may build (hosted service, enterprise edition, proprietary extensions). DCO alone does not grant those rights — it only certifies that you have the right to submit the contribution. Open-core projects need both.

By signing, you certify that:

- You have authored 100% of the contribution, or have the necessary rights to submit it.
- You grant **commit0-dev** a perpetual, worldwide, royalty-free licence to use your contribution under [Apache License 2.0](LICENSE), **including in commercial products** the maintainers may build.
- If your employer has rights over your work, you have obtained their permission.

**Signing is automatic.** When you open a Pull Request, [CLA Assistant](https://cla-assistant.io) posts a comment with a sign-in link. Click it, authorise with your GitHub account, and you're done — the signature is recorded once and applies to all your future Contributions across every commit0-dev repository.

The required `license/cla` GitHub Check blocks merge until you've signed.

> **Both checks are required.** `DCO` (per-commit `Signed-off-by:` trailer) **and** `license/cla` (per-contributor signature on this Agreement) — if either is red, the PR cannot merge.
