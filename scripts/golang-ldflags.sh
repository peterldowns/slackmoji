#!/usr/bin/env bash
# Generates the linker flags needed to embed version information in releases.
#
#   ldflags=$(./scripts/golang-ldflags.sh)
#   go build -ldflags "$ldflags" ...

VERSION=$1
COMMIT=$2
if [ -z "$VERSION" ]; then
  VERSION=$(cat ./VERSION)
fi
if [ -z "$COMMIT" ]; then
  COMMIT="$(git rev-parse --short HEAD || echo 'unknown')"
fi

package="main"
echo "-X $package.Version=$VERSION -X $package.Commit=$COMMIT"
