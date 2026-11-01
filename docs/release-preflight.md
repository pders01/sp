# Release preflight — 2026-11-01

**No branch push, tag, release publication or tap update has been performed.**
This is local preparation, not hosted CI or security-release sign-off.

The application remediation is documented in [security-remediation.md](security-remediation.md).
The next-version proposal is [v0.22.0-rc.1](release-notes.md), not an approved or
created tag. Windows interactive operation remains intentionally fail-closed.

## CI blockers addressed

- Ran the exact configured golangci-lint **v2.5.0**, rather than treating vet or
  the installed v1 binary as equivalent. The uncapped initial report contained
  146 findings. Error bindings, duplicate external-edit lifecycle code, unused
  code and style/test setup are corrected; `.golangci.yml` rules/exclusions were
  not relaxed. Both macOS and Linux/amd64 build-context lint report zero issues.
- Matched CI's **race + coverage** combination. It exposed two five-second test
  timeouts in maximum-size paste checks that plain race runs missed. Ordinary
  paste bytes are now collected in bounded chunks without changing limits,
  literal semantics or rejection assertions. Existing deadlines remain; new
  accepted/rejected boundary tests and repeated instrumented runs pass.
- Actionlint detected the retired runtime in `codecov-action@v3`. CI now uses v5,
  `files`, explicit report selection, and OIDC rather than a long-lived upload
  secret. Repository contents remain read-only outside release publication.
  Actual Codecov authentication/upload is not verified locally.
- `make ci` now uses the CI-pinned v2 linter, race/coverage, vulnerability scanning
  and build instead of silently substituting vet for missing lint.
- GoReleaser infers prerelease tags and skips stable Homebrew uploads for
  prereleases. Its configuration validates; actual publication is not attempted.

## Evidence

Platform: local macOS arm64, Go 1.25.13. Application/CI source through `b37c4cd`,
with release documentation in the working tree. Local acceptance binary:

```text
.privacy-adversarial/release-preflight/sp-preflight
SHA256 570da8b51b34dfa412d7a053e861b9403925fcfa08fdc6c448a6b617a1defde8
```

Final documentation commits/rebuilds can change VCS build metadata and therefore
the executable hash without changing application behavior.

Checks performed:

- Configured v2.5.0 lint: zero issues on macOS and Linux/amd64 build contexts.
- Exact CI race/coverage invocation: all seven packages pass after paste-chunk
  optimization. Normal tests also pass. Vet is not substituted for lint.
- Module checksum verification and reachable-advisory scan pass. Scanner reports
  zero detected reachable vulnerable calls, with 10 imported-package and 38
  required-module advisories without detected call paths; not dependency clearance.
- Windows/amd64 and Linux/amd64 test cross-compilation passes, not native execution.
- GoReleaser v2.12.0 configuration validates. A local snapshot builds all five
  targets and archives, writes checksums and a local Homebrew formula, without
  publishing. `brews` deprecation is a warning, not a configuration failure.
- Archive inspection confirms binary, README, LICENSE and remediation docs; no
  `TODO.md`, synthetic fixture directory or key metadata is packaged. Local arm64
  snapshot `--version` reports the snapshot version, commit and build date.
- Release-script shell syntax is checked. The script itself is not invoked:
  it creates and pushes a stable tag.

Final matching-candidate acceptance passed:

- **14 privacy PTY checks**, fixture `terminal-28a47f7f00768d3e`.
- **9 UX workflow groups**, fixture `ux-terminal-20742746af11b1dd`.
- **4 design PTYs**, fixture `design-terminal-e5e9670ec6ec8186`.
- `make ci` completed successfully. The exact race/coverage run also passed before
  that target reused matching successful test results.
- Actionlint v1.7.7, GoReleaser configuration checks, and all five snapshot archive
  builds passed. The snapshot is `0.21.1-next`, a packaging-only name inferred from
  existing tags, **not** the proposed next version or a release.
- All four CI-duration fuzz targets passed locally (about five seconds each),
  separate from native Linux execution.

The fixtures and local tool/package artifacts are under ignored
`.privacy-adversarial/`. Earlier remediation hashes and results remain historical,
not substituted for this candidate. No private-note transcripts are stored.

## Required checks outside pi / after branch approval

The sandbox **blocked the Linux container operation**. It was not inspected,
retried or worked around. Please run the intended check manually outside pi from
the prepared checkout, for example:

```sh
docker run --rm --cpus=4 --memory=6g \
  --mount "type=bind,source=$PWD,target=/work" --workdir /work \
  golang:1.25.13 bash -euc '
    go version
    go test -v -race -coverprofile=coverage.out ./...
    mkdir -p .privacy-adversarial
    go build -o .privacy-adversarial/sp-linux-ci ./cmd/sp
    python3 scripts/privacy-terminal/main.py --binary .privacy-adversarial/sp-linux-ci
  '
```

This uses synthetic stores; root containers skip the permission-injection signal
check because root bypasses directory write permissions. Image architecture is
host-selected; emulated amd64 execution is not native amd64 verification. Native
Windows CI is still required; cross-compilation does not test Windows runtime
locking/ACLs or restore the explicitly unsupported interactive console path.

After approving the coordinated history update, require all hosted jobs on the
exact pushed commit: Linux/macOS/Windows race/coverage, Linux/macOS private PTYs,
Linux fuzzing, reachable vulnerabilities, pinned lint and build/artifact jobs.
External Codecov/tap settings, tokens, remote rules and release permissions were
not inspected or proven by local checks.

## Push and release decisions

`git-filter-repo` removed `TODO.md` from local history and rewrote existing branch
and tag IDs. The file remains local and ignored via `.git/info/exclude`.

1. Review the diff, confirm the branch/history rewrite strategy and inspect the
   remote OID without fetching old history. Any approved branch rewrite must use
   an explicit expected-OID lease. Do not blindly pull or push all tags.
2. Existing published tag cleanup/replacement is a separate explicit decision,
   not an automatic step in creating the new release. Historical release assets
   have their own recorded identities.
3. Push only the approved branch update; wait for all hosted CI jobs to pass.
4. Confirm the release version, Windows restriction and remaining privacy gates.
   A release candidate is recommended over silently declaring stable support.
5. Only then create/push the single new tag. A tag push publishes immediately;
   the release workflow does not wait for an independent CI run.

Neither passing CI nor snapshot packaging completes the provider-blocked live
exercise, independent cryptographic audit, native GUI/accessibility validation,
private Neovim design, or unrestricted same-user protection. None is claimed.
