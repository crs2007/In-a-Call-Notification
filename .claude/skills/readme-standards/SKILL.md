---
name: readme-standards
description: Standard structure for project READMEs plus a validator that checks compliance. Use when creating a README from scratch, restructuring or reviewing an existing README, or before merging a change that touches README.md.
---

# README Generation & Validation

Every README in this project follows one structure so a reader can find
"what is it", "how do I install it" and "how do I use it" in the same place
every time. This skill has three parts:

1. The **checklist** below — what a README must and should contain.
2. [TEMPLATE.md](TEMPLATE.md) — a blueprint to copy when starting fresh.
3. `scripts/validate_readme.go` — a validator that turns the checklist into a
   pass/fail gate.

## Workflow

**New README**

1. Copy `TEMPLATE.md` to the target location and replace every placeholder.
   Delete recommended sections that don't apply rather than leaving stubs.
2. Run the validator (below) and fix anything it reports.

**Reviewing or editing an existing README**

1. Run the validator first to see the current gaps.
2. Fix required failures before anything else; treat recommended warnings as
   suggestions, not blockers.
3. Preserve the existing voice and any project-specific sections (e.g. this
   repo's `Why` and `Privacy` sections) — the checklist adds structure, it does
   not replace content.

**Validator**

```bash
go run ./.claude/skills/readme-standards/scripts/validate_readme.go            # checks ./README.md
go run ./.claude/skills/readme-standards/scripts/validate_readme.go path/to/README.md
go run ./.claude/skills/readme-standards/scripts/validate_readme.go --strict   # warnings also fail
```

Exit code is non-zero when a required section is missing (or, with
`--strict`, when a recommended one is). The script uses only the Go standard
library, so `go run` is the only prerequisite.

## The Essential Checklist

**Required sections (must have)**

- [ ] **Project title & badges** — a single H1 as the first line; at least one
      status badge (CI, release version, license) directly under it.
- [ ] **Elevator pitch** — 1–2 sentences, before the first `##`, saying what it
      does and why it's better than the alternative.
- [ ] **Visual demonstration** — a screenshot, GIF, or fenced code block
      showing the thing in action.
- [ ] **Installation** — exact, copy-pasteable steps (download link, `go
      install`, `npm install`, …).
- [ ] **Quick Start / Usage** — the bare minimum to make it do something
      useful. One command or one config snippet, not a tutorial.
- [ ] **License** — explicit statement of usage rights (MIT, Apache 2.0, …)
      linking to the LICENSE file.

**Recommended sections (should have)**

- [ ] **Table of Contents** — only if the README exceeds ~1,000 words.
- [ ] **Configuration / API** — every variable, flag, endpoint, or config key
      a user can set, with defaults.
- [ ] **Contributing** — link to `CONTRIBUTING.md` and dev-environment setup.
- [ ] **FAQ / Troubleshooting** — the pitfalls people actually hit.

## Writing rules

- Lead with the user's problem, not the implementation. Architecture belongs
  in `docs/`, not the README.
- Installation and Quick Start must be verifiable: every command should run
  as written on a clean machine.
- Keep badges honest — no "build passing" badge that isn't wired to CI.
- Link rather than duplicate: point at `CONTRIBUTING.md`, `SECURITY.md`,
  `docs/` instead of restating them.

## Project-specific notes (In-a-Call-Notification)

- README's **Installation** section is bound to the release shape defined in
  `.goreleaser.yaml` (archive names, architectures, shipped files). See the
  release checklist in `CLAUDE.md`; any change to the release shape must update
  Installation in the same change.
- The `release-ci` agent owns README and the other user-facing docs — route
  README rewrites through it.
- The existing `Why` and `Privacy` sections are deliberate and should stay;
  they are this project's differentiators.
