#!/usr/bin/env bash
# Exercise the release scripts against a stubbed gh CLI.
#
# These scripts decide whether a release goes public, so their failure modes
# matter more than most: the cases below are the ones where getting it wrong
# publishes something wrong rather than just failing the run.
#
# Run directly: ./hack/release/test-release-scripts.sh
# Requires: bash, jq. No test framework.

set -uo pipefail

cd "$(dirname "$0")" || exit 1
here="${PWD}"
export PATH="${here}/testdata:${PATH}"

create_script="${here}/create-draft-release.sh"
publish_script="${here}/publish-release.sh"

passed=0
failed=0

# Every case runs one script with a fresh stub log, then asserts on its exit
# code, its output, and the gh commands it issued.
run_case() {
  FAKE_GH_LOG="$(mktemp)"
  export FAKE_GH_LOG
  output=""
  rc=0
  output="$(env "$@" 2>&1)" || rc=$?
  log="$(cat "${FAKE_GH_LOG}")"
  rm -f "${FAKE_GH_LOG}"
}

ok() {
  echo "  PASS  $1"
  passed=$((passed + 1))
}

bad() {
  echo "  FAIL  $1"
  echo "        rc=${rc}"
  echo "        output: ${output}"
  echo "        gh calls: $(tr '\n' '|' <<<"${log}")"
  failed=$((failed + 1))
}

expect_rc() { # expect_rc <want> <description>
  if [ "${rc}" = "$1" ]; then ok "$2 (rc=${rc})"; else bad "$2 - want rc=$1"; fi
}

expect_gh() { # expect_gh <substring> <description>
  if grep -qF -- "$1" <<<"${log}"; then ok "$2"; else bad "$2 - no gh call matching '$1'"; fi
}

expect_no_gh() { # expect_no_gh <substring> <description>
  if grep -qF -- "$1" <<<"${log}"; then bad "$2 - unexpected gh call '$1'"; else ok "$2"; fi
}

expect_output() { # expect_output <substring> <description>
  if grep -qF -- "$1" <<<"${output}"; then ok "$2"; else bad "$2 - output lacks '$1'"; fi
}

echo "== create-draft-release.sh =="

run_case FAKE_GH_STATE=absent TAG=v0.4.0 PRERELEASE=false bash "${create_script}"
expect_rc 0 "no existing release, stable: succeeds"
expect_gh "gh release create v0.4.0 --title v0.4.0 --generate-notes --draft --verify-tag" \
  "no existing release, stable: creates a verified draft"
expect_no_gh "--prerelease" "stable release is not flagged as a pre-release"

run_case FAKE_GH_STATE=absent TAG=v0.4.0-rc.1 PRERELEASE=true bash "${create_script}"
expect_rc 0 "no existing release, RC: succeeds"
expect_gh "--draft --verify-tag --prerelease" "RC is created as a pre-release"

run_case FAKE_GH_STATE=draft TAG=v0.4.0 PRERELEASE=false bash "${create_script}"
expect_rc 0 "existing draft: succeeds"
expect_no_gh "release create" "existing draft is reused, not recreated"

run_case FAKE_GH_STATE=published TAG=v0.4.0 PRERELEASE=false bash "${create_script}"
expect_rc 1 "already-published release: refuses"
expect_output "::error::" "already-published release: annotates the failure"
expect_no_gh "release create" "already-published release: creates nothing"

# A GitHub outage must not be read as "the release does not exist" - that would
# take the create path and step over the published-release guard above.
run_case FAKE_GH_STATE=absent FAKE_GH_ERROR="HTTP 503: Service unavailable" \
  TAG=v0.4.0 PRERELEASE=false bash "${create_script}"
expect_rc 1 "API error that is not a 404: fails closed"
expect_no_gh "release create" "API error: creates nothing"

echo "== publish-release.sh =="

both_uploaded="$(printf 'kubefleet-crds-v0.4.0.tgz;uploaded;4096\nkubefleet-crds-v0.4.0.tgz.sha256;uploaded;98')"

run_case FAKE_GH_STATE=draft FAKE_GH_ASSETS="${both_uploaded}" TAG=v0.4.0 PRERELEASE=false \
  bash "${publish_script}"
expect_rc 0 "complete draft, stable: publishes"
expect_gh "gh release edit v0.4.0 --draft=false --prerelease=false" \
  "stable release is published without the pre-release flag"

run_case FAKE_GH_STATE=draft \
  FAKE_GH_ASSETS="$(printf 'kubefleet-crds-v0.4.0-rc.1.tgz;uploaded;4096\nkubefleet-crds-v0.4.0-rc.1.tgz.sha256;uploaded;98')" \
  TAG=v0.4.0-rc.1 PRERELEASE=true bash "${publish_script}"
expect_rc 0 "complete draft, RC: publishes"
# A draft created by hand defaults to prerelease=false, so the flag has to be
# set at publish time or an RC becomes the repository's "Latest release".
expect_gh "--draft=false --prerelease=true" "RC is published flagged as a pre-release"

run_case FAKE_GH_STATE=draft FAKE_GH_ASSETS="kubefleet-crds-v0.4.0.tgz;uploaded;4096" \
  TAG=v0.4.0 PRERELEASE=false bash "${publish_script}"
expect_rc 1 "missing checksum asset: refuses to publish"
expect_no_gh "release edit" "missing checksum asset: release stays a draft"

run_case FAKE_GH_STATE=draft FAKE_GH_ASSETS="" TAG=v0.4.0 PRERELEASE=false bash "${publish_script}"
expect_rc 1 "no assets at all: refuses to publish"

# GitHub keeps an asset row for an upload that never finished; it is present by
# name but not in the "uploaded" state.
run_case FAKE_GH_STATE=draft \
  FAKE_GH_ASSETS="$(printf 'kubefleet-crds-v0.4.0.tgz;new;0\nkubefleet-crds-v0.4.0.tgz.sha256;uploaded;98')" \
  TAG=v0.4.0 PRERELEASE=false bash "${publish_script}"
expect_rc 1 "interrupted upload (state != uploaded): refuses to publish"
expect_no_gh "release edit" "interrupted upload: release stays a draft"

run_case FAKE_GH_STATE=draft \
  FAKE_GH_ASSETS="$(printf 'kubefleet-crds-v0.4.0.tgz;uploaded;0\nkubefleet-crds-v0.4.0.tgz.sha256;uploaded;98')" \
  TAG=v0.4.0 PRERELEASE=false bash "${publish_script}"
expect_rc 1 "zero-byte asset: refuses to publish"

# Names alone are not proof; the bundle ships a checksum, so it gets checked.
run_case FAKE_GH_STATE=draft FAKE_GH_ASSETS="${both_uploaded}" FAKE_GH_DOWNLOAD=corrupt \
  TAG=v0.4.0 PRERELEASE=false bash "${publish_script}"
expect_rc 1 "bundle that fails its own checksum: refuses to publish"
expect_no_gh "release edit" "failed checksum: release stays a draft"

# An asset whose name only looks right must not satisfy the check.
run_case FAKE_GH_STATE=draft \
  FAKE_GH_ASSETS="$(printf 'kubefleet-crds-v0.4.0.tgz.sha256;uploaded;98\nkubefleet-crds-v0.4.0.tgz.asc;uploaded;800')" \
  TAG=v0.4.0 PRERELEASE=false bash "${publish_script}"
expect_rc 1 "similar-but-wrong asset names: refuses to publish"

echo
echo "passed=${passed} failed=${failed}"
[ "${failed}" -eq 0 ]
