#!/usr/bin/env bash
# Creates a GitHub Release for the current version and commit, or returns the
# existing release's asset-upload URL. Requires gh and GH_TOKEN.

set -euo pipefail

version=$(cat ./VERSION)
commit_sha=$(git rev-parse --short HEAD || echo 'unknown')
target_commit=$(git rev-parse HEAD || echo 'main')
# https://semver.org/#spec-item-10
release_name="$version+commit.$commit_sha"
if ! upload_url=$(
  gh api --method POST 'repos/{owner}/{repo}/releases' \
    -F "tag_name=$release_name" \
    -F "name=$release_name" \
    -F "target_commitish=$target_commit" \
    --jq '.upload_url'
); then
  upload_url=$(gh api --method GET "repos/{owner}/{repo}/releases/tags/$release_name" --jq '.upload_url')
fi
# GitHub returns an RFC 6570 template ending in {?name,label}.
echo "${upload_url%\{*}"
