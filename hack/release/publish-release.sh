#!/usr/bin/env bash
# Publish the draft release for a tag, after checking it actually carries the
# assets it is supposed to.
#
# This is the atomic commit point of a release: everything else in the release
# workflow produces artifacts, and this is the only step that makes the release
# visible.
#
# Environment:
#   TAG        release tag, e.g. v0.4.0                        (required)
#   PRERELEASE "true" when TAG is a release candidate          (required)
#   GH_TOKEN   token with contents: write
#   GH_REPO    owner/repo

set -euo pipefail

: "${TAG:?TAG must be set}"
: "${PRERELEASE:?PRERELEASE must be set}"

bundle="kubefleet-crds-${TAG}.tgz"
checksum="${bundle}.sha256"

# Only assets GitHub finished receiving count. An upload interrupted mid-stream
# leaves an asset row with the right name in a non-"uploaded" state, which a
# name-only check would accept.
assets="$(gh release view "${TAG}" --json assets \
  --jq '.assets[] | select(.state == "uploaded" and .size > 0) | .name')"

for want in "${bundle}" "${checksum}"; do
  if ! grep -qxF -- "${want}" <<<"${assets}"; then
    echo "::error::Release ${TAG} is missing fully-uploaded asset ${want}; leaving it as a draft."
    exit 1
  fi
done

# Verify the bytes, not just the names: the bundle ships with a checksum, so
# confirming it here is the difference between "an asset with that name exists"
# and "the artifact users will download is intact".
workdir="$(mktemp -d)"
trap 'rm -rf "${workdir}"' EXIT
gh release download "${TAG}" --dir "${workdir}" --pattern "${bundle}" --pattern "${checksum}"

if command -v sha256sum >/dev/null 2>&1; then
  (cd "${workdir}" && sha256sum -c "${checksum}")
else
  (cd "${workdir}" && shasum -a 256 -c "${checksum}")
fi

# Set the pre-release flag here rather than only at creation time: a draft this
# workflow reused may have been created by hand, and GitHub defaults such
# drafts to "not a pre-release". Publishing an RC under that flag would make it
# the repository's "Latest release".
gh release edit "${TAG}" --draft=false --prerelease="${PRERELEASE}"
echo "✅ Published release ${TAG} (prerelease=${PRERELEASE})."
