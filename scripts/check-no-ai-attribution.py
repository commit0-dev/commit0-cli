#!/usr/bin/env python3
# pylint: disable=missing-module-docstring
"""commit-msg hook: block AI/agentic-coding attribution in commit messages.

Policy source: CLAUDE.md (project + global).
  > "All commits must appear as sole-author from the committer."
  > "Do NOT add any Co-Authored-By line."

Threat model: any commit-message artifact that attributes the work to a
non-human agent — or to any co-author at all, per the sole-author rule.
The hook is broad: Claude is one of many. Catalog lives in
`scripts/ai_attribution_patterns.py` and covers:

  - Attribution trailers (Co-Authored-By, Generated-By, Assisted-By, ...)
  - "Generated with/by/using X", "Powered by X", "AI-assisted" markers
  - Robot emoji \U0001F916
  - Branding brackets — [Claude Code], [Cursor], [Cline], [Aider],
    [Devin], [Sweep], [CodeRabbit], [Continue], [Cody], [Copilot],
    [Codeium], [Windsurf], [Tabnine], [v0.dev], [Bolt.new], [Replit AI],
    [Amazon Q], [CodeWhisperer], [Augment Code]
  - Vendor noreply / @-domain emails for every major provider
  - LLM/model names (Claude, GPT, Gemini, Llama, Mistral, Grok,
    DeepSeek, Qwen, Cohere, etc.) — but ONLY when adjacent to an
    attribution context anchor; bare prose mentions are allowed
    because this codebase legitimately integrates with these vendors
  - Agent/tool names (Aider, Cursor, Cline, Devin, Copilot, Codeium,
    Windsurf, Continue, Sweep, CodeRabbit, Cody, Tabnine, Augment,
    Replit AI, Bolt.new, v0.dev, CodeWhisperer, Amazon Q, AutoGPT,
    BabyAGI, OpenInterpreter, ...)

Allowlist: optional `.commit-msg-policy.allowlist` at the repo root.
One substring per line (case-insensitive). `#` starts a comment. Any
violation whose matched line contains an allowlisted substring is
suppressed. Use sparingly.

Override: standard git escape hatch — `git commit --no-verify`. Per
CLAUDE.md, every bypass requires a follow-up Issue tracking why.
"""
from __future__ import annotations

import sys
from pathlib import Path
from typing import List, Optional, Tuple

# Make the sibling catalog importable regardless of cwd at hook fire time.
sys.path.insert(0, str(Path(__file__).resolve().parent))
import ai_attribution_patterns as catalog  # noqa: E402  pylint: disable=wrong-import-position


# A violation: (category, label, line_no [1-indexed], line_text).
Violation = Tuple[str, str, int, str]


def _strip_git_comments(raw: str) -> str:
    """Drop lines whose first non-whitespace char is `#` (git's template).

    Real commit content keeps a `#` mid-line (e.g. `Refs #15`) — only
    leading-`#` lines are dropped, mirroring git's own behaviour.
    """
    return "\n".join(
        line for line in raw.splitlines() if not line.lstrip().startswith("#")
    )


def _load_allowlist(repo_root: Path) -> List[str]:
    """Load the optional `.commit-msg-policy.allowlist` file."""
    path = repo_root / ".commit-msg-policy.allowlist"
    if not path.exists():
        return []
    out: List[str] = []
    for raw_line in path.read_text(encoding="utf-8", errors="replace").splitlines():
        line = raw_line.strip()
        if not line or line.startswith("#"):
            continue
        out.append(line.lower())
    return out


def _line_for_offset(body: str, lines: List[str], offset: int) -> Tuple[int, str]:
    line_no = body.count("\n", 0, offset) + 1
    line_text = lines[line_no - 1].rstrip() if 0 < line_no <= len(lines) else ""
    return line_no, line_text


def _line_window(body: str, lines: List[str], offset: int) -> str:
    """Two-line window (current + previous) around `offset`.

    Soft-pattern context anchoring: a soft hit is a violation only if
    its window contains an attribution anchor. Two lines covers
    `Co-Authored-By:` followed by a wrapped-name line.
    """
    line_no = body.count("\n", 0, offset) + 1
    start = max(1, line_no - 1)
    return "\n".join(lines[start - 1:line_no])


def _has_context_anchor(window: str) -> bool:
    return any(anchor.search(window) for anchor in catalog.CONTEXT_ANCHORS)


def scan(text: str, allowlist: Optional[List[str]] = None) -> List[Violation]:
    """Return every violation in `text`.

    Hard rules fire on any match. Soft rules fire only when the matched
    line (or the line above it) contains an attribution context anchor.
    Allowlist substrings suppress per-line violations.
    """
    body = _strip_git_comments(text)
    lines = body.splitlines()
    out: List[Violation] = []

    for category, rules in catalog.HARD_RULES.items():
        for pattern, label in rules:
            for match in pattern.finditer(body):
                line_no, line_text = _line_for_offset(body, lines, match.start())
                out.append((category, label, line_no, line_text))

    for category, rules in catalog.SOFT_RULES.items():
        for pattern, label in rules:
            for match in pattern.finditer(body):
                window = _line_window(body, lines, match.start())
                if not _has_context_anchor(window):
                    continue
                line_no, line_text = _line_for_offset(body, lines, match.start())
                out.append((category, label, line_no, line_text))

    if allowlist:
        out = [
            v for v in out
            if not any(needle in v[3].lower() for needle in allowlist)
        ]

    out.sort(key=lambda v: (v[2], v[0], v[1]))
    return out


def _format_report(violations: List[Violation]) -> str:
    parts: List[str] = [
        "ERROR: commit message contains forbidden AI/agentic attribution.",
        "",
        "Policy: every commit must appear as sole-author from the committer.",
        "Source: CLAUDE.md (project + global).",
        "",
        "Violations:",
    ]
    for category, label, line_no, line_text in violations:
        parts.append(f"  line {line_no} [{category}]: {label}")
        if line_text:
            parts.append(f"    > {line_text}")
    parts += [
        "",
        "Fix: edit the commit message to remove every match above, then re-commit.",
        "",
        "Allowlist: add a substring to .commit-msg-policy.allowlist if a match is",
        "  legitimate (e.g. a vendor name appearing in feature scope).",
        "",
        "Bypass (genuine emergencies only — open a follow-up Issue):",
        "  git commit --no-verify",
    ]
    return "\n".join(parts)


def _find_repo_root(start: Path) -> Path:
    for candidate in (start, *start.parents):
        if (candidate / ".git").exists():
            return candidate
    return start


def main(argv: List[str]) -> int:
    if len(argv) < 2:
        print("usage: check-no-ai-attribution.py <commit-msg-file>", file=sys.stderr)
        return 2
    msg_path = Path(argv[1]).resolve()
    try:
        raw = msg_path.read_text(encoding="utf-8", errors="replace")
    except OSError as exc:
        print(f"check-no-ai-attribution: cannot read {msg_path}: {exc}", file=sys.stderr)
        return 2
    repo_root = _find_repo_root(msg_path.parent)
    allowlist = _load_allowlist(repo_root)
    violations = scan(raw, allowlist=allowlist)
    if not violations:
        return 0
    print(_format_report(violations), file=sys.stderr)
    return 1


if __name__ == "__main__":
    sys.exit(main(sys.argv))
