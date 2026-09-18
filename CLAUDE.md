# CLAUDE.md

## Release process

- **Version source of truth:** `cmd/callmqtt/main.go`'s `var version = "dev"`,
  overridden only via goreleaser's `-X main.version={{.Version}}` ldflag in the
  release build. There is no separate VERSION file and nothing to bump by
  hand — the version comes from the git tag the workflow creates.
- **How a release is cut — every push to `main` is a release:**
  `.github/workflows/release.yml` runs on `push` to `main`. Its `version` job
  computes the next tag from the latest existing `v*` tag, pushes it, and the
  `goreleaser` job (`windows-latest`, gated by `go vet` + `go test -race`)
  runs `goreleaser release --clean` for that tag. Published assets:
  `callmqtt_<version>_windows_amd64.zip`,
  `callmqtt_<version>_windows_arm64.zip`, and `checksums.txt` on
  `crs2007/In-a-Call-Notification`. There is no manual version bump step and
  no version file to edit — the tag *is* the version.
- **Controlling the bump from the commit message** (head commit of the push):
  - default → patch bump (`v0.1.3` → `v0.1.4`); a pre-release is promoted to
    its base version (`v0.1.0-alpha.1` → `v0.1.0`).
  - `[minor]` → `v0.1.3` → `v0.2.0`; `[major]` → `v0.1.3` → `v1.0.0`.
  - `[skip release]` / `[no release]` → push nothing (docs-only or WIP
    commits). Use this deliberately; the default is to ship.
  - Pushing a `v*` tag by hand still releases that exact tag — that is how a
    `-alpha.N` / `-rc.N` pre-release is cut. A branch push whose HEAD is
    already tagged skips its own release so the two runs don't collide.
  - `workflow_dispatch` on the Release workflow offers a `bump` input
    (patch/minor/major) for an on-demand release without a new commit.
- **Before pushing to `main`, always:** run `go vet ./...` and
  `go test -race ./...` locally (a failure in the release job means the tag
  has already been pushed but nothing was published — delete the tag before
  retrying); decide whether the commit message needs a `[minor]`, `[major]`
  or `[skip release]` marker; and if the change alters what ships (see the
  checklist below), update README in the same push so the release and its
  docs match.
- **Squash or batch work before pushing** — each push to `main` is one
  release, so ten small pushes are ten tags. Prefer one push per coherent
  change.
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
