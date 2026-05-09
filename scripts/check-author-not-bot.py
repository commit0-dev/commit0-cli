#!/usr/bin/env python3
# pylint: disable=missing-module-docstring
"""pre-commit hook: reject commits authored as a bot/agent identity.

Catches the case where the commit MESSAGE is clean but the configured
git author is e.g. `noreply@anthropic.com` or `name [bot]` — which
the message-only hook (`check-no-ai-attribution.py`) cannot see.

Reads the would-be author/committer identity from `git var
GIT_AUTHOR_IDENT` and `git var GIT_COMMITTER_IDENT`. These values are
what git is about to write into the commit object, regardless of
whether they came from `git config user.{name,email}`, env vars, or
`-c` overrides on the command line.

Override: standard `git commit --no-verify`. Per CLAUDE.md, every
bypass requires a follow-up Issue tracking why.
"""
from __future__ import annotations

import re
import subprocess
import sys
from pathlib import Path
from typing import List, Tuple

# Make the sibling catalog importable regardless of cwd at hook fire time.
sys.path.insert(0, str(Path(__file__).resolve().parent))
import ai_attribution_patterns as catalog  # noqa: E402  pylint: disable=wrong-import-position


# Output of `git var GIT_AUTHOR_IDENT` looks like:
#   Alice Engineer <alice@example.com> 1700000000 +0000
_IDENT_RE = re.compile(r"^(?P<name>.*?)\s*<(?P<email>[^>]+)>")


def _parse_ident(ident: str) -> Tuple[str, str]:
    match = _IDENT_RE.match(ident.strip())
    if not match:
        return "", ""
    return match.group("name").strip(), match.group("email").strip()


def _read_git_ident(var: str) -> str:
    """Run `git var <var>` and return its first line of stdout."""
    try:
        proc = subprocess.run(
            ["git", "var", var],
            check=True,
            capture_output=True,
            text=True,
        )
    except (subprocess.CalledProcessError, FileNotFoundError):
        return ""
    return proc.stdout.splitlines()[0] if proc.stdout else ""


def scan_identity(name: str, email: str) -> List[Tuple[str, str, str, str]]:
    """Return list of (kind, label, field_name, field_value) violations."""
    out: List[Tuple[str, str, str, str]] = []
    for pattern, label in catalog.BOT_NAME_PATTERNS:
        if pattern.search(name):
            out.append(("bot_name", label, "name", name))
    for pattern, label in catalog.BOT_EMAIL_PATTERNS:
        if pattern.search(email):
            out.append(("bot_email", label, "email", email))
    return out


def _format_report(
    ident_label: str,
    name: str,
    email: str,
    violations: List[Tuple[str, str, str, str]],
) -> str:
    parts = [
        f"ERROR: git {ident_label} identity matches a bot/agent pattern.",
        "",
        f"  name:  {name!r}",
        f"  email: {email!r}",
        "",
        "Violations:",
    ]
    for kind, label, field, value in violations:
        parts.append(f"  [{kind}] {field}={value!r}: {label}")
    parts += [
        "",
        "Fix: set a human identity for this repo:",
        "  git config user.name  'Your Name'",
        "  git config user.email 'you@example.com'",
        "(or globally with --global, or for this commit with -c overrides)",
        "",
        "Bypass (genuine emergencies only — open a follow-up Issue):",
        "  git commit --no-verify",
    ]
    return "\n".join(parts)


def main(_argv: List[str]) -> int:
    issues: List[str] = []
    for var, label in (("GIT_AUTHOR_IDENT", "author"),
                       ("GIT_COMMITTER_IDENT", "committer")):
        ident = _read_git_ident(var)
        if not ident:
            continue
        name, email = _parse_ident(ident)
        violations = scan_identity(name, email)
        if violations:
            issues.append(_format_report(label, name, email, violations))
    if not issues:
        return 0
    print("\n\n".join(issues), file=sys.stderr)
    return 1


if __name__ == "__main__":
    sys.exit(main(sys.argv))
