# Release Process

This document explains how to release a new version of the `sp` application and update the Homebrew tap.

## Prerequisites

1. **GitHub Access**: Ensure you have push access to both repositories:
   - `github.com/pders01/sp` (main application)
   - `github.com/pders01/homebrew-sp` (Homebrew tap)

2. **GoReleaser**: The release process uses GoReleaser for automated builds and releases.

3. **GitHub Token**: A GitHub token with appropriate permissions for creating releases and pushing to the tap repository.

## Release Steps

### 1. Prepare for Release

Ensure your working directory is clean and you're on `main`, then run the local
CI counterparts using the toolchain selected by `go.mod`:

```bash
git status --short
git branch --show-current
make ci                   # tidy, pinned lint, race/coverage, release checks, vuln, build
goreleaser check
make privacy-terminal     # macOS/Linux; fresh synthetic stores only
make ux-terminal
make design-terminal
```

Local success is not proof that GitHub's native Linux/macOS/Windows matrix,
Codecov, publication permissions, or tap updates will pass. Use the
[preflight record](docs/release-preflight.md) and review the
[draft release notes](docs/release-notes.md). The sandbox blocked the Linux
container check; run it outside pi as described in the preflight record.

**History rewrite warning:** `TODO.md` was removed with `git-filter-repo`, which
changed branch and existing local tag IDs. Do not `git pull` old history back in,
use `git push --tags`, or force-update published tags as part of this release.
Coordinate the branch/history cleanup separately, inspect the remote's current
OID without fetching old objects, and use an explicit expected-OID lease for an
approved branch rewrite. Existing tag cleanup requires a separate decision.
The release owner subsequently reported all hosted CI green through `2f99dd4`
and the v0.22.0-rc.3 GoReleaser workflow green. Earlier Linux/Windows failures are
fixed; keep their RC tags unchanged. See the updated preflight for current versus
historical evidence. Stable v0.22.0 promotion is now approved, subject to CI on
its final preparation commit and publication verification.

**Scope limits:** Windows interactive flows currently fail closed pending a
safe console adapter. The blocked live exercise, independent audit and broader
native GUI/accessibility validation are not completed by CI. A stable release
channel does not certify these capabilities; retain the release notes' limits.

### 2. Create a Release

First push only the approved branch update and wait for **all** hosted CI jobs
to pass on that exact commit. Confirm the version and platform/security limits
before creating any tag. A tag push starts publication immediately; the release
workflow does not wait for a separate CI run.

For an approved stable version, the release script creates **and pushes** a tag:

```bash
./scripts/release.sh 0.22.0
```

This script will:
- Validate the stable version format
- Check that you're on the main branch
- Ensure the working directory is clean
- Create and push a git tag

The script intentionally accepts stable `X.Y.Z` versions only. After approval,
a stable promotion can instead be tagged and pushed explicitly after main is
pushed and its hosted CI is green (example only; not run by preflight):

```bash
git tag -a v0.22.0 -m 'Release v0.22.0'
git push origin refs/tags/v0.22.0
```

Do not run both methods. The helper refuses an existing exact stable tag but
allows promotion when only `v0.22.0-rc.*` tags exist. Local tags and commits can
be prepared in pi; sandbox-blocked remote operations must be performed manually
outside pi.

GoReleaser detects prerelease tags automatically. Prereleases do not update the
stable Homebrew formula (`brews.skip_upload: auto`). Do not merely rename an RC
release or clear its prerelease flag: that does not rebuild version metadata or
rerun the skipped tap publication. A stable `v0.22.0` tag enables
the tap upload. RC success does not validate `HOMEBREW_TAP_TOKEN`: it needs
contents-write permission on `pders01/homebrew-sp`, separately from the workflow's
application-repository `GITHUB_TOKEN`. Never paste tokens into logs or commands.
Snapshot builds publish nothing; they are packaging smoke tests, not releases.

### 3. Automated Release Process

When you push a tag, GitHub Actions will automatically:

1. **Build Binaries**: Create executables for multiple platforms:
   - macOS (amd64, arm64)
   - Linux (amd64, arm64)
   - Windows (amd64)

2. **Create GitHub Release**: Upload the binaries and publish `docs/release-notes.md`, including compatibility and privacy limitations

3. **Update Homebrew Tap**: Automatically update the formula in `homebrew-sp` with:
   - New version number
   - Correct download URLs
   - SHA256 checksums

### 4. Publication and Homebrew Verification

Confirm the stable release is not marked prerelease; check all five archives,
checksums, licenses and binary version/commit metadata. Then verify that the tap
formula names v0.22.0, its four supported-platform URLs and their matching SHA256
hashes. No native Windows interactive support is implied by its Windows archive.

The workflow can create the GitHub release before failing to upload the tap.
Inspect both outcomes; do not move the stable tag to repair a tap-only failure.
After the release completes, verify the Homebrew tap outside pi:

```bash
brew update
brew tap pders01/sp
brew install pders01/sp/sp   # or: brew upgrade pders01/sp/sp
brew test sp
sp --version
```

## Homebrew Tap Structure

The Homebrew tap is located at `../homebrew-sp/` and contains:

```
homebrew-sp/
├── Formula/
│   └── sp.rb          # Homebrew formula
└── README.md          # Tap documentation
```

### Formula Details

The formula supports:
- **macOS**: Intel (amd64) and Apple Silicon (arm64)
- **Linux**: Intel (amd64) and ARM (arm64)
- **Automatic Updates**: GoReleaser automatically updates URLs and checksums

## Troubleshooting

### Release Fails

If the GitHub Actions release fails:

1. Check the Actions tab in GitHub for error details
2. Ensure the GitHub token has the necessary permissions
3. Verify that both repositories are accessible

### Homebrew Formula Issues

If the Homebrew formula isn't updated automatically:

1. Check that the `brews` section in `.goreleaser.yml` is correct
2. Verify the tap repository exists and is accessible
3. Manually update the formula if needed (see below)

### Manual Formula Update

If you need to manually update the Homebrew formula:

1. Download the release assets from GitHub
2. Calculate SHA256 hashes:
   ```bash
   shasum -a 256 sp_Darwin_x86_64.tar.gz
   shasum -a 256 sp_Darwin_arm64.tar.gz
   shasum -a 256 sp_Linux_x86_64.tar.gz
   shasum -a 256 sp_Linux_arm64.tar.gz
   ```
3. Update `../homebrew-sp/Formula/sp.rb` with the correct hashes
4. Commit and push the changes

## Version Management

### Semantic Versioning

Follow semantic versioning (MAJOR.MINOR.PATCH):
- **MAJOR**: Breaking changes
- **MINOR**: New features (backward compatible)
- **PATCH**: Bug fixes (backward compatible)

### Version Variables

The application includes version information that's set during build:
- `version`: The semantic version (e.g., "0.1.0")
- `commit`: Git commit hash
- `date`: Build date

These are accessible via `sp --version`.

## User Installation

Once released, users can install the application with:

```bash
# Add the tap
brew tap pders01/sp

# Install the application
brew install sp

# Verify installation
sp --version
```

## Development

For development builds, use the Makefile or `go build` directly. The
build target is the package directory (`./cmd/sp`), not the entry
file:

```bash
# Make targets
make build              # ./sp
make release-snapshot   # GoReleaser snapshot to ./dist

# Manual go build
go build -o sp ./cmd/sp

# Cross-compile
GOOS=darwin GOARCH=amd64 go build -o sp-darwin-amd64 ./cmd/sp
```

`make ci` runs the local counterparts (`deps → pinned lint → race/coverage →
hermetic release-helper tests → vulnerability scan → build`). Native matrix execution, POSIX PTYs, Linux fuzz
checks, and external coverage upload are separate jobs/steps in GitHub Actions;
local preflight does not replace those results.

## Notes

- The release process is fully automated once a tag is pushed
- GoReleaser handles all platform-specific builds and packaging
- The Homebrew tap is updated automatically with stable releases, not RCs
- Users get the latest version by running `brew upgrade sp` 