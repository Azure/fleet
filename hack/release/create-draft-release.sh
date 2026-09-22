#!/usr/bin/env bash
# Create the GitHub Release for a tag as a draft, or reuse the draft already
# there.
#
# The release is created before any artifact exists so the producer jobs have
# somewhere to upload while the release stays invisible to consumers.
# publish-release.sh makes it public once every producer has succeeded.
#
# Environment:
#   TAG        release tag, e.g. v0.4.0                        (required)
#   PRERELEASE "true" when TAG is a release candidate          (required)
#   GH_TOKEN   token with contents: write
#   GH_REPO    owner/repo

set -euo pipefail

: "${TAG:?TAG must be set}"
: "${PRERELEASE:?PRERELEASE must be set}"

stderr="$(mktemp)"
trap 'rm -f "${stderr}"' EXIT

# Distinguish "no such release" from a transient API failure. Treating an
# outage as "the release does not exist" would take the create path and bypass
# the published-release guard below.
if is_draft="$(gh release view "${TAG}" --json isDraft --jq .isDraft 2>"${stderr}")"; then
  if [ "${is_draft}" != "true" ]; then
    echo "::error::Release ${TAG} is already published. Re-running the full workflow for a completed release is refused; see RELEASING.md for the recovery procedure."
    exit 1
  fi
  # A draft may be a re-run of this workflow, or notes a maintainer pre-staged.
  # Either way it is reused as-is; publish-release.sh reconciles the
  # pre-release flag at publish time, so a hand-created draft cannot go out
  # mislabelled.
  echo "Reusing existing draft release ${TAG}."
  exit 0
fi

if ! grep -qiE "not found|404" "${stderr}"; then
  echo "::error::Could not determine the state of release ${TAG}: $(tr '\n' ' ' <"${stderr}")"
  exit 1
fi

# --verify-tag: without it, `gh release create` happily invents the tag at the
# default branch's HEAD, which would cut a full release from whatever is on
# main under a version nobody intended.
create_args=(--title "${TAG}" --generate-notes --draft --verify-tag)
if [ "${PRERELEASE}" = "true" ]; then
  create_args+=(--prerelease)
fi

gh release create "${TAG}" "${create_args[@]}"
echo "Created draft release ${TAG}."
