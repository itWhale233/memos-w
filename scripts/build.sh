#!/bin/sh

set -eu

# Change to repo root.
cd "$(dirname "$0")/../"

IMAGE_NAME="${IMAGE_NAME:-whale/memos}"
IMAGE_TAG="${IMAGE_TAG:-latest}"
VERSION="${VERSION:-dev}"
COMMIT="${COMMIT:-$(git rev-parse --short HEAD 2>/dev/null || printf 'unknown')}"
PLATFORM="${PLATFORM:-}"

usage() {
  cat <<'EOF'
Usage:
  scripts/build.sh binary
  scripts/build.sh docker

Environment for docker builds:
  IMAGE_NAME   Docker image name. Default: memos-custom
  IMAGE_TAG    Docker image tag. Default: latest
  VERSION      App version passed into the binary. Default: dev
  COMMIT       Commit sha passed into the binary. Default: current git short sha
  PLATFORM     Optional docker build platform, for example linux/amd64
EOF
}

build_binary() {
  OS="$(uname -s)"

  case "$OS" in
    *CYGWIN*|*MINGW*|*MSYS*)
      OUTPUT="./build/memos.exe"
      ;;
    *)
      OUTPUT="./build/memos"
      ;;
  esac

  printf 'Building binary for %s...\n' "$OS"

  mkdir -p ./build/.gocache ./build/.gomodcache
  export GOCACHE="$(pwd)/build/.gocache"
  export GOMODCACHE="$(pwd)/build/.gomodcache"

  go build -o "$OUTPUT" ./cmd/memos

  printf 'Build successful.\n'
  printf 'Run: %s\n' "$OUTPUT"
}

build_docker() {
  IMAGE_REF="${IMAGE_NAME}:${IMAGE_TAG}"

  if [ -n "$PLATFORM" ]; then
    docker build \
      --platform "$PLATFORM" \
      --build-arg VERSION="$VERSION" \
      --build-arg COMMIT="$COMMIT" \
      -f ./scripts/Dockerfile \
      -t "$IMAGE_REF" \
      .
  else
    docker build \
      --build-arg VERSION="$VERSION" \
      --build-arg COMMIT="$COMMIT" \
      -f ./scripts/Dockerfile \
      -t "$IMAGE_REF" \
      .
  fi

  printf 'Docker image built: %s\n' "$IMAGE_REF"
}

MODE="${1:-docker}"

case "$MODE" in
  binary)
    build_binary
    ;;
  docker)
    build_docker
    ;;
  -h|--help|help)
    usage
    ;;
  *)
    printf 'Unknown mode: %s\n' "$MODE" >&2
    usage >&2
    exit 1
    ;;
esac
