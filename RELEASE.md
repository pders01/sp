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

Ensure your working directory is clean and you're on the main branch:

```bash
git status
git checkout main
git pull origin main
```

### 2. Create a Release

Use the release script to create and push a new version tag:

```bash
./scripts/release.sh 0.1.0
```

This script will:
- Validate the version format
- Check that you're on the main branch
- Ensure the working directory is clean
- Create and push a git tag

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
   shasum -a 256 sp_0.1.0_darwin_amd64.tar.gz
   shasum -a 256 sp_0.1.0_darwin_arm64.tar.gz
   shasum -a 256 sp_0.1.0_linux_amd64.tar.gz
   shasum -a 256 sp_0.1.0_linux_arm64.tar.gz
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

For development builds, you can build locally:

```bash
# Build for current platform
go build -o sp cmd/sp/main.go

# Build for specific platform
GOOS=darwin GOARCH=amd64 go build -o sp-darwin-amd64 cmd/sp/main.go
```

## Notes

- The release process is fully automated once a tag is pushed
- GoReleaser handles all platform-specific builds and packaging
- The Homebrew tap is updated automatically with each release
- Users get the latest version by running `brew upgrade sp` 