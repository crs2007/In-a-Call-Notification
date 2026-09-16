# CLAUDE.md

## Release process

- **Version source of truth:** `cmd/callmqtt/main.go`'s `var version = "dev"`,
  overridden only via goreleaser's `-X main.version={{.Version}}` ldflag on a
  tag-triggered build. There is no separate VERSION file.
- **How a release is cut:** push a `v*` git tag →
  `.github/workflows/release.yml` runs `goreleaser release --clean` on
  `windows-latest` → produces `callmqtt_<version>_windows_amd64.zip`,
  `callmqtt_<version>_windows_arm64.zip`, and `checksums.txt` as GitHub
  Release assets on `crs2007/In-a-Call-Notification`.
- **Checklist after cutting a release, or whenever `.goreleaser.yaml`
  changes:**
  - Confirm the release's actually-published assets match what README's
    Installation section describes (filenames, architectures, archive
    contents like `configs/example.yaml`).
  - If the goreleaser config's build targets, archive naming pattern, or
    shipped files change (e.g. a new OS/arch is added, or the archive format
    changes), update README's Installation section to match, in the same
    change.
  - README links to `/releases/latest`, so a plain version bump needs no
    README edit — only a change in the *shape* of the release (new platform,
    renamed binary, different archive format, etc.) requires touching README.
  - Use the `release-ci` subagent for this documentation work — it owns
    `.github/workflows/`, `.goreleaser.yaml`, and the user-facing docs
    (README, CONTRIBUTING, SECURITY, docs/).

## README standards

- Any change to `README.md` (or a new README anywhere in the repo) follows the
  `readme-standards` skill in `.claude/skills/readme-standards/` — checklist,
  template, and validator.
- Run the validator before merging a README change:
  `go run ./.claude/skills/readme-standards/scripts/validate_readme.go`
  (add `--strict` to also fail on recommended sections).
