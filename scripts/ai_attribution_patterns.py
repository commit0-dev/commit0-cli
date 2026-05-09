#!/usr/bin/env python3
# pylint: disable=missing-module-docstring
"""Catalog of AI/agentic-coding attribution patterns.

Single source of truth for both the commit-msg hook
(`check-no-ai-attribution.py`) and the author-identity hook
(`check-author-not-bot.py`).

Design notes:

- Every pattern carries a stable category and a human label so violation
  reports tell developers exactly which rule fired.
- "Hard" patterns block in any context. "Soft" patterns are LLM/model
  names that this project legitimately integrates with (Claude, GPT,
  Gemini, etc.); they're blocked only when they appear in attribution
  context (Co-Authored-By:, in an email, after "by"/"via", inside
  branding brackets). The hook composes hard + context-anchored soft
  patterns into a single scan.
- The catalog is exhaustive for current (2026-Q2) market players. New
  agents/tools will appear; PRs that add to this file are encouraged.
- An external allowlist (`.commit-msg-policy.allowlist`) lets a project
  exempt specific filenames or substrings without editing this file.
"""
from __future__ import annotations

import re
from typing import Dict, List, Tuple


# A compiled rule: regex + label. Categories group rules so tests can
# assert every category is non-empty (deletion-resistant).
Rule = Tuple[re.Pattern[str], str]


# ─────────────────────────────────────────────────────────────────────────
# Hard rules — always block, any context.
# ─────────────────────────────────────────────────────────────────────────

ATTRIBUTION_TRAILERS: List[Rule] = [
    # Sole-author policy: any Co-Authored-By line is forbidden, regardless
    # of who is named. Humans included — the rule is about commit ownership
    # transparency, not just AI attribution.
    (re.compile(r"^\s*co-?authored-?by\s*:", re.IGNORECASE | re.MULTILINE),
     "Co-Authored-By trailer (any value)"),
    (re.compile(r"^\s*generated-?by\s*:", re.IGNORECASE | re.MULTILINE),
     "Generated-By trailer"),
    (re.compile(r"^\s*assisted-?by\s*:", re.IGNORECASE | re.MULTILINE),
     "Assisted-By trailer"),
    (re.compile(r"^\s*on-?behalf-?of\s*:", re.IGNORECASE | re.MULTILINE),
     "On-Behalf-Of trailer"),
]


GENERATED_BY_ATTRIBUTION: List[Rule] = [
    # Common AI-tool tagline shapes.
    (re.compile(r"\bgenerated\s+(with|by|using)\s+\S+", re.IGNORECASE),
     "'Generated with/by/using X' attribution"),
    (re.compile(r"\bcreated\s+(with|by|using)\s+(an?\s+)?ai\b", re.IGNORECASE),
     "'Created with/by AI' attribution"),
    (re.compile(r"\bwritten\s+by\s+(an?\s+)?ai\b", re.IGNORECASE),
     "'Written by AI' attribution"),
    (re.compile(r"\bauthored\s+by\s+(an?\s+)?(ai|llm|agent|bot)\b", re.IGNORECASE),
     "'Authored by AI/LLM/agent/bot' attribution"),
    (re.compile(r"\b(ai|llm)[- ]?(assisted|generated|authored|written)\b", re.IGNORECASE),
     "AI/LLM-assisted/generated marker"),
    (re.compile(r"\bpowered\s+by\s+\S+", re.IGNORECASE),
     "'Powered by X' attribution"),
]


ROBOT_EMOJI: List[Rule] = [
    # 🤖 effectively never appears in legitimate commit prose. It's the
    # canonical AI-attribution marker the harness inserts.
    (re.compile(r"\U0001F916"),
     "robot emoji 🤖"),
]


BRANDING_BRACKETS: List[Rule] = [
    # `[X Code]` / `[X CLI]` patterns the harness inserts.
    (re.compile(r"\[\s*claude\s*code\s*\]", re.IGNORECASE),
     "[Claude Code] branding"),
    (re.compile(r"\[\s*cursor(\s+(agent|composer|ai))?\s*\]", re.IGNORECASE),
     "[Cursor] branding"),
    (re.compile(r"\[\s*cline\s*\]", re.IGNORECASE),
     "[Cline] branding"),
    (re.compile(r"\[\s*aider\s*\]", re.IGNORECASE),
     "[Aider] branding"),
    (re.compile(r"\[\s*devin\s*\]", re.IGNORECASE),
     "[Devin] branding"),
    (re.compile(r"\[\s*sweep\s*\]", re.IGNORECASE),
     "[Sweep] branding"),
    (re.compile(r"\[\s*coderabbit\s*\]", re.IGNORECASE),
     "[CodeRabbit] branding"),
    (re.compile(r"\[\s*continue\s*\]", re.IGNORECASE),
     "[Continue] branding"),
    (re.compile(r"\[\s*cody\s*\]", re.IGNORECASE),
     "[Cody] branding"),
    (re.compile(r"\[\s*copilot\s*(workspace)?\s*\]", re.IGNORECASE),
     "[Copilot] branding"),
    (re.compile(r"\[\s*codeium\s*\]", re.IGNORECASE),
     "[Codeium] branding"),
    (re.compile(r"\[\s*windsurf\s*\]", re.IGNORECASE),
     "[Windsurf] branding"),
    (re.compile(r"\[\s*tabnine\s*\]", re.IGNORECASE),
     "[Tabnine] branding"),
    (re.compile(r"\[\s*v0\.dev\s*\]", re.IGNORECASE),
     "[v0.dev] branding"),
    (re.compile(r"\[\s*bolt\.new\s*\]", re.IGNORECASE),
     "[Bolt.new] branding"),
    (re.compile(r"\[\s*replit\s+(ai|agent)\s*\]", re.IGNORECASE),
     "[Replit AI/Agent] branding"),
    (re.compile(r"\[\s*amazon\s+q\s*\]", re.IGNORECASE),
     "[Amazon Q] branding"),
    (re.compile(r"\[\s*codewhisperer\s*\]", re.IGNORECASE),
     "[CodeWhisperer] branding"),
    (re.compile(r"\[\s*augment(\s+code)?\s*\]", re.IGNORECASE),
     "[Augment Code] branding"),
]


VENDOR_EMAILS: List[Rule] = [
    # Provider noreply / @-domain patterns. Legitimate commits don't
    # include vendor email addresses.
    (re.compile(r"@anthropic\.com\b", re.IGNORECASE),
     "@anthropic.com email"),
    (re.compile(r"@claude\.ai\b", re.IGNORECASE),
     "@claude.ai email"),
    (re.compile(r"@openai\.com\b", re.IGNORECASE),
     "@openai.com email"),
    (re.compile(r"@cursor\.(so|sh|com)\b", re.IGNORECASE),
     "Cursor email"),
    (re.compile(r"@aider\.chat\b", re.IGNORECASE),
     "Aider email"),
    (re.compile(r"@cognition\.(ai|com)\b", re.IGNORECASE),
     "Cognition (Devin) email"),
    (re.compile(r"@codeium\.com\b", re.IGNORECASE),
     "Codeium email"),
    (re.compile(r"@continue\.dev\b", re.IGNORECASE),
     "Continue.dev email"),
    (re.compile(r"@sweep\.dev\b", re.IGNORECASE),
     "Sweep.dev email"),
    (re.compile(r"@coderabbit\.ai\b", re.IGNORECASE),
     "CodeRabbit email"),
    (re.compile(r"@sourcegraph\.com\b", re.IGNORECASE),
     "Sourcegraph email"),
    (re.compile(r"@tabnine\.com\b", re.IGNORECASE),
     "Tabnine email"),
    (re.compile(r"@replit\.com\b", re.IGNORECASE),
     "Replit email"),
    (re.compile(r"@bolt\.new\b", re.IGNORECASE),
     "Bolt.new email"),
    (re.compile(r"@phind\.com\b", re.IGNORECASE),
     "Phind email"),
    # The classic GitHub bot pattern: `<name>[bot]@users.noreply.github.com`.
    (re.compile(r"\b\w+\[bot\]@users\.noreply\.github\.com\b", re.IGNORECASE),
     "GitHub bot noreply email"),
]


# ─────────────────────────────────────────────────────────────────────────
# Soft rules — LLM/model/tool names. Blocked only in attribution context
# (see CONTEXT_ANCHORS below). Bare prose mentions are allowed because
# this project legitimately integrates with these vendors.
# ─────────────────────────────────────────────────────────────────────────

LLM_NAMES: List[Rule] = [
    # Anthropic — exempt the literal filename `CLAUDE.md` (the policy doc).
    (re.compile(r"\bclaude\b(?!\.md\b)", re.IGNORECASE),
     "Anthropic's Claude"),
    (re.compile(r"\banthropic\b", re.IGNORECASE),
     "Anthropic"),
    # OpenAI: GPT-3, GPT-3.5, GPT-4, GPT-4o, GPT-5, GPT-OSS, ChatGPT, Codex.
    (re.compile(r"\bgpt-?\d", re.IGNORECASE),
     "GPT-N model"),
    (re.compile(r"\bchatgpt\b", re.IGNORECASE),
     "ChatGPT"),
    (re.compile(r"\bopenai\b", re.IGNORECASE),
     "OpenAI"),
    (re.compile(r"\bcodex\b", re.IGNORECASE),
     "OpenAI Codex"),
    # Google: Gemini, Bard, PaLM. Exempt `GEMINI.md` policy doc.
    (re.compile(r"\bgemini\b(?!\.md\b)", re.IGNORECASE),
     "Google Gemini"),
    (re.compile(r"\bgoogle\s+bard\b", re.IGNORECASE),
     "Google Bard"),
    (re.compile(r"\bpalm-?2?\b", re.IGNORECASE),
     "Google PaLM"),
    # Meta Llama.
    (re.compile(r"\bllama-?[234]\b", re.IGNORECASE),
     "Meta Llama"),
    # Mistral models.
    (re.compile(r"\bmistral(-large|-small|-medium)?\b", re.IGNORECASE),
     "Mistral"),
    (re.compile(r"\bmixtral\b", re.IGNORECASE),
     "Mixtral"),
    (re.compile(r"\bcodestral\b", re.IGNORECASE),
     "Codestral"),
    # xAI.
    (re.compile(r"\bgrok-?\d?\b", re.IGNORECASE),
     "xAI Grok"),
    # DeepSeek.
    (re.compile(r"\bdeepseek\b", re.IGNORECASE),
     "DeepSeek"),
    # Alibaba Qwen.
    (re.compile(r"\bqwen-?\d?\b", re.IGNORECASE),
     "Alibaba Qwen"),
    # Cohere Command.
    (re.compile(r"\bcohere\b", re.IGNORECASE),
     "Cohere"),
    (re.compile(r"\bcommand-r(\+|-plus)?\b", re.IGNORECASE),
     "Cohere Command-R"),
    # Inflection Pi — too generic alone; require "Pi AI".
    (re.compile(r"\bpi\s+ai\b", re.IGNORECASE),
     "Inflection Pi"),
]


AGENT_NAMES: List[Rule] = [
    # Specific agent/tool names. Only blocked in attribution context.
    (re.compile(r"\baider\b", re.IGNORECASE),
     "Aider"),
    (re.compile(r"\bcursor\s+(agent|composer|ai)\b", re.IGNORECASE),
     "Cursor Agent/Composer/AI"),
    (re.compile(r"\bcline\b", re.IGNORECASE),
     "Cline"),
    (re.compile(r"\broo\s+code\b", re.IGNORECASE),
     "Roo Code"),
    (re.compile(r"\bgithub\s+copilot\b", re.IGNORECASE),
     "GitHub Copilot"),
    (re.compile(r"\bcopilot\s+workspace\b", re.IGNORECASE),
     "Copilot Workspace"),
    (re.compile(r"\bcodeium\b", re.IGNORECASE),
     "Codeium"),
    (re.compile(r"\bwindsurf\b", re.IGNORECASE),
     "Windsurf"),
    (re.compile(r"\bcontinue\.dev\b", re.IGNORECASE),
     "Continue.dev"),
    (re.compile(r"\bdevin\b", re.IGNORECASE),
     "Cognition Devin"),
    (re.compile(r"\bsweep\.dev\b", re.IGNORECASE),
     "Sweep.dev"),
    (re.compile(r"\bcoderabbit\b", re.IGNORECASE),
     "CodeRabbit"),
    (re.compile(r"\bsourcegraph\s+cody\b", re.IGNORECASE),
     "Sourcegraph Cody"),
    (re.compile(r"\btabnine\b", re.IGNORECASE),
     "Tabnine"),
    (re.compile(r"\baugment\s+code\b", re.IGNORECASE),
     "Augment Code"),
    (re.compile(r"\breplit\s+(ai|agent)\b", re.IGNORECASE),
     "Replit AI/Agent"),
    (re.compile(r"\bbolt\.new\b", re.IGNORECASE),
     "Bolt.new"),
    (re.compile(r"\bv0\.dev\b", re.IGNORECASE),
     "v0.dev"),
    (re.compile(r"\bcodewhisperer\b", re.IGNORECASE),
     "Amazon CodeWhisperer"),
    (re.compile(r"\bamazon\s+q\b", re.IGNORECASE),
     "Amazon Q"),
    (re.compile(r"\bphind\b", re.IGNORECASE),
     "Phind"),
    (re.compile(r"\bzed\s+ai\b", re.IGNORECASE),
     "Zed AI"),
    (re.compile(r"\bwarp\s+ai\b", re.IGNORECASE),
     "Warp AI"),
    (re.compile(r"\bautogpt\b", re.IGNORECASE),
     "AutoGPT"),
    (re.compile(r"\bbabyagi\b", re.IGNORECASE),
     "BabyAGI"),
    (re.compile(r"\bopen\s*interpreter\b", re.IGNORECASE),
     "OpenInterpreter"),
    (re.compile(r"\bsmol(\s*ai)?\b", re.IGNORECASE),
     "Smol/Smol AI"),
    (re.compile(r"\bcrew\s*ai\b", re.IGNORECASE),
     "CrewAI"),
    (re.compile(r"\bauto\s*gen\b", re.IGNORECASE),
     "AutoGen"),
]


# Context anchors that turn a soft pattern into a hard violation. If a
# soft-pattern match is within a window adjacent to one of these anchors,
# it counts. Otherwise it's allowed (legitimate prose / code reference).
#
# Implementation: for each soft match at offset O, look at the line it's
# on AND the previous line. If any anchor regex matches that 2-line
# window, flag the soft hit.
CONTEXT_ANCHORS: List[re.Pattern[str]] = [
    # Trailer keys at the start of a line — the canonical attribution shape.
    re.compile(r"^\s*co-?authored-?by\s*:", re.IGNORECASE | re.MULTILINE),
    re.compile(r"^\s*signed-?off-?by\s*:", re.IGNORECASE | re.MULTILINE),
    re.compile(r"^\s*generated-?by\s*:", re.IGNORECASE | re.MULTILINE),
    re.compile(r"^\s*assisted-?by\s*:", re.IGNORECASE | re.MULTILINE),
    # Verb-phrase attribution markers. Anchored on the verb so prose
    # like "support for Gemini" or "(via the Anthropic API)" stays clean.
    re.compile(r"\bgenerated\s+(with|by|using)\b", re.IGNORECASE),
    re.compile(r"\bauthored\s+by\b", re.IGNORECASE),
    re.compile(r"\bwritten\s+by\b", re.IGNORECASE),
    re.compile(r"\bcreated\s+by\b", re.IGNORECASE),
    re.compile(r"\bpowered\s+by\b", re.IGNORECASE),
    # The robot emoji effectively marks AI attribution on its own.
    re.compile(r"\U0001F916"),
    # An angle-bracketed email is the trailer convention — anchor on
    # the bracket pair so prose containing an email URL stays clean.
    re.compile(r"<[^>\s]+@[^>\s]+>"),
]


# ─────────────────────────────────────────────────────────────────────────
# Author / committer identity patterns (used by check-author-not-bot.py).
# ─────────────────────────────────────────────────────────────────────────

# git config user.name patterns that smell like a bot.
BOT_NAME_PATTERNS: List[Rule] = [
    (re.compile(r"\[bot\]\s*$", re.IGNORECASE),
     "name ends in [bot]"),
    (re.compile(r"^bot[\s\-_]", re.IGNORECASE),
     "name starts with 'bot'"),
    (re.compile(r"[\s\-_]bot$", re.IGNORECASE),
     "name ends in '-bot' / '_bot' / ' bot'"),
    (re.compile(r"^claude\b", re.IGNORECASE),
     "name starts with 'Claude'"),
    (re.compile(r"^gpt\b", re.IGNORECASE),
     "name starts with 'GPT'"),
    (re.compile(r"^gemini\b", re.IGNORECASE),
     "name starts with 'Gemini'"),
    (re.compile(r"^cursor\b", re.IGNORECASE),
     "name starts with 'Cursor'"),
    (re.compile(r"^aider\b", re.IGNORECASE),
     "name starts with 'Aider'"),
    (re.compile(r"^devin\b", re.IGNORECASE),
     "name starts with 'Devin'"),
    (re.compile(r"^copilot\b", re.IGNORECASE),
     "name starts with 'Copilot'"),
]

# git config user.email patterns that smell like a bot.
# Reuses VENDOR_EMAILS plus a few generic noreply forms.
BOT_EMAIL_PATTERNS: List[Rule] = list(VENDOR_EMAILS) + [
    (re.compile(r"^noreply@", re.IGNORECASE),
     "noreply@ sender"),
    (re.compile(r"\bbot@", re.IGNORECASE),
     "bot@ sender"),
    (re.compile(r"@bot\.\w+\b", re.IGNORECASE),
     "@bot.* sender"),
    (re.compile(r"\+ai@", re.IGNORECASE),
     "+ai sub-address"),
    (re.compile(r"\+bot@", re.IGNORECASE),
     "+bot sub-address"),
]


# ─────────────────────────────────────────────────────────────────────────
# Composite groups — exposed for tests + the entrypoint script.
# ─────────────────────────────────────────────────────────────────────────

HARD_RULES: Dict[str, List[Rule]] = {
    "attribution_trailers": ATTRIBUTION_TRAILERS,
    "generated_by_attribution": GENERATED_BY_ATTRIBUTION,
    "robot_emoji": ROBOT_EMOJI,
    "branding_brackets": BRANDING_BRACKETS,
    "vendor_emails": VENDOR_EMAILS,
}


SOFT_RULES: Dict[str, List[Rule]] = {
    "llm_names": LLM_NAMES,
    "agent_names": AGENT_NAMES,
}


AUTHOR_RULES: Dict[str, List[Rule]] = {
    "bot_names": BOT_NAME_PATTERNS,
    "bot_emails": BOT_EMAIL_PATTERNS,
}


__all__ = [
    "Rule",
    "HARD_RULES",
    "SOFT_RULES",
    "AUTHOR_RULES",
    "ATTRIBUTION_TRAILERS",
    "GENERATED_BY_ATTRIBUTION",
    "ROBOT_EMOJI",
    "BRANDING_BRACKETS",
    "VENDOR_EMAILS",
    "LLM_NAMES",
    "AGENT_NAMES",
    "CONTEXT_ANCHORS",
    "BOT_NAME_PATTERNS",
    "BOT_EMAIL_PATTERNS",
]
