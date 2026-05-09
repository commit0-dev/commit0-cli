<!--
Append this section to the project's CONTRIBUTING.md (or create one).
The skill bootstrap copies this verbatim — no placeholders to substitute.
-->

## Signing your work

Every commit must carry a `Signed-off-by:` trailer. The trailer is your attestation to the [Developer Certificate of Origin](DCO.md) — by adding it, you certify you have the right to submit the contribution under the project's open-source license.

```bash
git commit -s -m "<your commit message>"
```

The `-s` flag appends the trailer using your configured `user.name` and `user.email`. The email must be verified on your GitHub account.

If you forgot:

```bash
git commit --amend --signoff --no-edit         # last commit only
git rebase --signoff main && git push --force-with-lease   # whole branch
```

The required `DCO` check will block merge until every commit on the PR carries a valid trailer.
