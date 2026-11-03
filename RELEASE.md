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
make ci                   # tidy, CI-pinned v2 lint, race + coverage, vuln, build
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
The user subsequently pushed rewritten main and `v0.22.0-rc.1`; Ubuntu CI
reported a terminal-reader cancellation race. The proposed rc.2 correction must
pass hosted CI before tagging. Do not replace rc.1. See the updated preflight;
its original local results are historical, not hosted Linux verification.

**Release gates:** Windows interactive flows currently fail closed pending a
safe console adapter. The blocked live model-driven security exercise and
independent/native-platform validation are not completed by CI. Prefer an
explicitly labelled release candidate; do not describe this as a fully validated
stable cross-platform/privacy release.

### 2. Create a Release

First push only the approved branch update and wait for **all** hosted CI jobs
to pass on that exact commit. Confirm the version and platform/security limits
before creating any tag. A tag push starts publication immediately; the release
workflow does not wait for a separate CI run.

For an approved stable version, the release script creates **and pushes** a tag:

```bash
./scripts/release.sh 0.1.0
```

This script will:
- Validate the stable version format
- Check that you're on the main branch
- Ensure the working directory is clean
- Create and push a git tag

The script intentionally accepts stable `X.Y.Z` versions only. After approval,
a release candidate can be tagged and pushed explicitly (example only; not run
by preflight):

```bash
git tag -a v0.22.0-rc.2 -m 'Release v0.22.0-rc.2'
git push origin refs/tags/v0.22.0-rc.2
```

GoReleaser detects prerelease tags automatically. Prereleases do not update the
stable Homebrew formula (`brews.skip_upload: auto`). Snapshot builds publish
nothing; they are packaging smoke tests, not validated/tagged releases.

### 3. Automated Release Process

When you push a tag, GitHub Actions will automatically:

1. **Build Binaries**: Create executables for multiple platforms:
   - macOS (amd64, arm64)
   - Linux (amd64, arm64)
   - Windows (amd64)

2. **Create GitHub Release**: Upload the binaries and create a release with changelog

3. **Update Homebrew Tap**: Automatically update the formula in `homebrew-sp` with:
   - New version number
   - Correct download URLs
   - SHA256 checksums

### 4. Manual Verification (Optional)

After the release completes, you can verify the Homebrew tap:

```bash
# Test the tap locally
brew tap pders01/sp
brew install sp

# Check the installed version
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
vulnerability scan → build`). Native matrix execution, POSIX PTYs, Linux fuzz
checks, and external coverage upload are separate jobs/steps in GitHub Actions;
local preflight does not replace those results.

## Notes

- The release process is fully automated once a tag is pushed
- GoReleaser handles all platform-specific builds and packaging
- The Homebrew tap is updated automatically with each release
- Users get the latest version by running `brew upgrade sp` 