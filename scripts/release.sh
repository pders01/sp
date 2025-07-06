#!/bin/bash

# Release script for sp
# Usage: ./scripts/release.sh <version>
# Example: ./scripts/release.sh 0.1.0

set -e

if [ $# -eq 0 ]; then
    echo "Usage: $0 <version>"
    echo "Example: $0 0.1.0"
    exit 1
fi

VERSION=$1

# Validate version format
if [[ ! $VERSION =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
    echo "Error: Version must be in format X.Y.Z (e.g., 0.1.0)"
    exit 1
fi

echo "Releasing version $VERSION..."

# Check if we're on main branch
BRANCH=$(git branch --show-current)
if [ "$BRANCH" != "main" ]; then
    echo "Error: Must be on main branch to release"
    exit 1
fi

# Check if working directory is clean
if [ -n "$(git status --porcelain)" ]; then
    echo "Error: Working directory is not clean. Please commit or stash changes."
    exit 1
fi

# Check if tag already exists
if git tag -l | grep -q "v$VERSION"; then
    echo "Error: Tag v$VERSION already exists"
    exit 1
fi

# Update version in go.mod if needed (optional)
# sed -i '' "s/^go 1\.[0-9]*$/go 1.24/" go.mod

# Create and push tag
echo "Creating tag v$VERSION..."
git tag -a "v$VERSION" -m "Release v$VERSION"

echo "Pushing tag..."
git push origin "v$VERSION"

echo "Release v$VERSION has been tagged and pushed!"
echo ""
echo "Next steps:"
echo "1. GitHub Actions will automatically build and release the binaries"
echo "2. The Homebrew tap will be updated automatically"
echo "3. Users can install with: brew tap pders01/sp && brew install sp"
echo ""
echo "To manually update the Homebrew formula SHA256 hashes:"
echo "1. Wait for the GitHub release to complete"
echo "2. Download the release assets"
echo "3. Calculate SHA256 hashes: shasum -a 256 <filename>"
echo "4. Update ../homebrew-sp/Formula/sp.rb with the correct hashes" 