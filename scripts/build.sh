#!/bin/sh

set -eu

cd "$(dirname "$0")/../"

IMAGE_NAME="${IMAGE_NAME:-itwhale/memos:latest}"
VERSION="${VERSION:-dev}"
COMMIT="${COMMIT:-unknown}"

if command -v git >/dev/null 2>&1; then
  if [ "$VERSION" = "dev" ]; then
    VERSION="$(git describe --tags --always 2>/dev/null || echo dev)"
  fi
  if [ "$COMMIT" = "unknown" ]; then
    COMMIT="$(git rev-parse --short HEAD 2>/dev/null || echo unknown)"
  fi
fi

echo "Building Docker image: $IMAGE_NAME"
echo "Version: $VERSION"
echo "Commit: $COMMIT"

docker build \
  -f scripts/Dockerfile \
  -t "$IMAGE_NAME" \
  --build-arg VERSION="$VERSION" \
  --build-arg COMMIT="$COMMIT" \
  .

echo "Docker build successful!"
echo "Built image: $IMAGE_NAME"
