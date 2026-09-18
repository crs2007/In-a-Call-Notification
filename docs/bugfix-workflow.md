# Bug-fix workflow: from issue to release

A repeatable procedure for taking one bug from the tracker to a published
release. It follows this repo's conventions: GitHub Issues on
`crs2007/In-a-Call-Notification`, Go with `_test.go` files next to the code,
PRs squash-merged into `main`, and — per [CLAUDE.md](../CLAUDE.md) — **every
push to `main` is a release**. Issue `#17` is used as the running example.

---

## 1. Bug triage & fetching

1. **List open bugs and pick one.** Triage by severity × reach, then
   dependency (fix the thing others build on first).
   ```
   gh issue list --label bug --state open
   gh issue view 17 --comments        # full report + discussion
   ```
2. **Confirm it is actionable.** The report must give: expected vs actual
   behaviour, version (`callmqtt --version`), and reproduction steps. If
   any are missing, ask in the issue and label `needs-info`; don't guess.
   ```
   gh issue comment 17 --body "Which version, and how long had the process been up?"
   gh issue edit 17 --add-label needs-info
   ```
3. **Claim it** so two people don't fix it twice.
   ```
   gh issue edit 17 --add-assignee @me
   ```
4. **Duplicate / already-fixed check** before writing any code:
   ```
   gh issue list --search "log rotate" --state all
   git log --oneline -20 -- internal/<suspect-package>/
   ```

## 2. Issue analysis

5. **Reproduce by hand first** (or confirm the reporter's repro). If you
   cannot reproduce, you cannot verify a fix. Note the exact
   inputs/environment in the issue.
6. **Locate the affected component(s).** Search from the symptom inward —
   log messages, config keys and error strings are the fastest anchors:
   ```
   git grep -n "connect failed"            # symptom string
   git grep -n "func Rotate"               # suspect function
   git log -S "rotate" --oneline           # when did this logic last change?
   git blame -L 40,60 internal/app/log.go  # who/why for the suspect lines
   ```
7. **Find the root cause, not the first plausible cause.** Write one
   sentence of the form *"X happens because Y, which was introduced/allowed
   by Z."* If you can't fill in all three, keep digging. Read the calling
   code path end to end rather than the single function.
8. **Assess impact and blast radius** — record it in the issue; it drives
   priority and the size of the fix:
   - Who is affected (all users / a config subset / one platform)?
   - Is data lost or corrupted, or is it cosmetic?
   - What else calls the code you intend to change?
     `git grep -n "FuncName("` and read every call site.
9. **Decide the fix scope** and write it down before coding: minimal fix
   for this bug **plus** a regression test. Refactors go in a separate PR.
   Post the analysis to the issue so the reasoning survives:
   ```
   gh issue comment 17 --body-file analysis.md
   ```

## 3. Test creation & reproduction

10. **Write the failing test *before* the fix**, next to the code it
    guards (`internal/<pkg>/<file>_test.go`), in the package's existing
    style — table-driven tests here; detector bugs use the fixture-driven
    tests under [internal/rules/](../internal/rules/).
    - Unit test for pure logic (`internal/…`).
    - Integration-style test when the bug is in an interaction (e.g. the
      engine driving the detector with real rules, as
      [state_test.go](../internal/detection/state_test.go) does for #2).
    - Name it after the issue so it's discoverable:
      `TestRotate_RetryWarningsDoNotGrowUnbounded_Issue17`.
11. **Confirm it fails for the right reason.** A test that fails with a
    compile error or a panic proves nothing.
    ```
    go test -race -run 'Issue17' ./internal/<pkg>/ -v
    ```
    The failure message should describe the bug ("got 1 rotation, want ≥2").
12. **Commit the test on its own** (see §4 for the branch first) so a
    reviewer can check out that commit and watch it fail:
    ```
    git commit -m "Add failing regression test for unbounded log growth (#17)"
    ```

## 4. Branching & fixing

13. **Branch from an up-to-date `main`.** Naming is `fix/issue-<N>-<slug>`.
    ```
    git switch main
    git pull --ff-only origin main
    git switch -c fix/issue-17-log-rotation
    ```
14. **Implement the minimal fix.**
    - Touch only the files you listed in step 9; if the fix wants to spill
      elsewhere, stop and rethink the approach.
    - Fix the cause, not the symptom (don't clamp the output; stop
      producing the bad input).
    - Match surrounding code style; no drive-by reformatting.
    - Reuse existing helpers before adding new ones.
15. **Run the quality gates locally** — the same ones CI runs
    ([ci.yml](../.github/workflows/ci.yml)), so a red PR is never a surprise:
    ```
    gofmt -l .                     # must print nothing
    go vet ./...
    go build ./... && go build -tags tray ./...
    go test -race ./...
    ```
16. **Commit the fix** with a message that explains *why*, not just what.
    Subject ≤ 72 chars, imperative mood; body = cause → fix → how it was
    verified; footer closes the issue. Commit `53137ee` is the house model.
    ```
    git add -p                     # stage hunks deliberately, review each
    git commit
    ```
    ```
    Rotate the log on size during the run, not only at startup

    Rotation was checked once in main(); a long-lived process that keeps
    failing to connect appends a warning per retry forever. Check size on
    every write via a size-aware writer and rotate in place.

    Regression test: 10k retry warnings against a 1 KiB limit -> file
    stays under limit, older bytes land in the .1 backup.

    Closes #17
    ```

## 5. Verification

17. **Prove the fix with the regression test**, then the full suite:
    ```
    go test -race -run 'Issue17' ./internal/<pkg>/ -v   # now passes
    go test -race ./...                                 # nothing else broke
    go test -count=3 -race ./internal/<pkg>/            # flakiness check for timing bugs
    ```
18. **Check the test actually guards the bug:** stash the fix, confirm
    the test goes red again, restore.
    ```
    git stash push -- <fixed-file>.go
    go test -run 'Issue17' ./internal/<pkg>/    # expect FAIL
    git stash pop
    ```
19. **Manual regression on the real binary** for anything users see —
    run the reporter's original steps and the adjacent features.
    ```
    go build -o callmqtt.exe ./cmd/callmqtt && .\callmqtt.exe --debug
    ```
20. **Review your own diff as a stranger** before asking anyone else:
    ```
    git diff main...HEAD
    ```
    Look for leftover debug prints, TODOs, unrelated changes, missing docs.
    If the change alters user-visible behaviour or what ships, update
    `README.md` in the same branch and run
    `go run ./.claude/skills/readme-standards/scripts/validate_readme.go`.

## 6. Closing the issue & check-in

21. **Tidy the branch history** so the PR reads test → fix, or squash to
    one commit (this repo squash-merges anyway):
    ```
    git reset --soft $(git merge-base main HEAD) && git commit   # reuse the §16 message
    ```
22. **Decide the release marker now.** The squash-merge subject becomes
    the `main` commit subject, and the Release workflow derives the version
    bump from it: default = patch; `[minor]` for new behaviour;
    `[skip release]` for docs/CI-only. A bug fix is normally a plain patch
    with no marker.
23. **Push the branch and open the PR** against `main`. Body: problem,
    cause, fix, how verified, `Closes #17` (GitHub closes the issue on
    merge).
    ```
    git push -u origin fix/issue-17-log-rotation
    gh pr create --base main --title "Rotate the log on size during the run, not only at startup" \
      --body-file pr.md
    ```
24. **Wait for CI to be green** and address review. Push follow-ups as
    new commits (reviewers can diff them); squash at merge.
    ```
    gh pr checks --watch
    gh pr view --comments
    ```
25. **Merge via the PR, never by hand.** Squash keeps `main` one commit
    per fix, which is what the release workflow expects. Confirm the
    squash subject still carries the `(#N)` suffix and any marker.
    ```
    gh pr merge --squash --delete-branch
    ```
    Merging closes the issue via `Closes #17`. If it didn't (keyword typo,
    merged to a non-default branch), close it by hand with a pointer:
    ```
    gh issue close 17 --comment "Fixed in #<pr> (commit <sha>)."
    ```

## 7. Final push

26. **Sync your local `main` and clean up.** With PR-based merging the
    push to `origin/main` already happened on the server at step 25;
    locally you fast-forward and drop the branch.
    ```
    git switch main
    git pull --ff-only origin main
    git branch -d fix/issue-17-log-rotation
    git fetch --prune
    ```
    Do not `git push origin main` directly. If you ever must, re-run
    `go vet ./... && go test -race ./...` on the merged tree first — a red
    release job leaves a pushed tag with no assets.
27. **Watch the release the merge triggered.**
    ```
    gh run list --workflow release.yml --limit 1
    gh run watch                        # follow it
    gh release view --web               # confirm assets: two zips + checksums.txt
    ```
    If the release job fails after the tag was pushed, delete the tag
    before retrying:
    ```
    git push origin :refs/tags/v0.1.5 && git tag -d v0.1.5
    ```
28. **Close the loop with the reporter** — a one-line comment on the
    (now closed) issue naming the release version:
    ```
    gh issue comment 17 --body "Shipped in v0.1.5: https://github.com/crs2007/In-a-Call-Notification/releases/tag/v0.1.5"
    ```
    Tick the item in [TODO.md](../TODO.md) if it's tracked there.
