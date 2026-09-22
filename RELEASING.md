# Releasing

This is the operational runbook for cutting a KubeFleet release and for
recovering when a release run fails partway through. It covers *how* a release
is produced; what the version numbers mean and how long each release is
supported are covered in [VERSIONING.md](VERSIONING.md) and
[SECURITY.md](SECURITY.md).

## What a release publishes

| Artifact | Location | Stable (`v0.4.0`) | Release candidate (`v0.4.0-rc.1`) |
| --- | --- | --- | --- |
| Agent images (`hub-agent`, `member-agent`, `refresh-token`) | `ghcr.io/kubefleet-dev/kubefleet/<image>` | `:v0.4.0` and `:0.4.0` | `:v0.4.0-rc.1` only |
| CRD bundle (`kubefleet-crds-<tag>.tgz` + `.sha256`) | GitHub Release asset | Yes | Yes |
| Helm charts (OCI) | `oci://ghcr.io/kubefleet-dev/kubefleet/charts/<chart>` | Yes | No |
| Helm charts (index) | `https://kubefleet-dev.github.io/kubefleet/charts` | Yes | No |
| GitHub Release | Releases page | Published | Published, flagged pre-release |

Release candidates deliberately get no short image alias and never enter the
public chart index: the short-tag namespace and the `helm repo` index are
reserved for releases users can safely pin to. Testers install an RC from its
full tag.

## Cutting a release

All images and charts are built from the tag itself, so everything that ships
must be merged before the tag is pushed.

1. Confirm `main` (or the `release-0.Y` branch) is green and carries every
   change intended for the release, including backports — see
   [CONTRIBUTING.md](CONTRIBUTING.md#backporting-to-release-branches).
2. Tag and push. The tag must match `vMAJOR.MINOR.PATCH` or
   `vMAJOR.MINOR.PATCH-rc.N`; any other shape is rejected before anything is
   published.

   ```bash
   git tag -a v0.4.0-rc.1 -m "v0.4.0-rc.1"
   git push upstream v0.4.0-rc.1
   ```

3. Watch the `Release` workflow. It publishes the GitHub Release only after
   every artifact has been produced.
4. For a stable release, repeat with the final tag (for example `v0.4.0`) once
   the RC has soaked.

A release can also be started from the Actions tab via **Run workflow** on the
`Release` workflow, passing the tag as an input. Two preconditions apply:

- The tag must already exist. The workflow passes `--verify-tag`, so a dispatch
  naming a tag that was never pushed fails instead of inventing one at the head
  of the default branch.
- The tag must be one cut after this workflow landed. A dispatch runs the
  workflow definition from the selected branch but checks out the *tag*, and the
  jobs call scripts under `hack/release/`; against an older tag that predates
  them, the first job fails immediately with "No such file or directory".
  Nothing is published when it does. Tags on a `release-0.Y` branch that predates
  this workflow are unaffected — they carry their own contemporary workflow.

## The release pipeline

`.github/workflows/release.yml` is the single owner of a release. Its job graph:

```text
setup                       validate the tag; derive registry, version, prerelease
 └── create-draft-release    create (or reuse) the GitHub Release as a draft
      ├── publish-images     multi-arch buildx push, then verify both platforms
      │    ├── publish-charts-oci     stable only; helm push + appVersion check
      │    └── publish-charts-pages   stable only; rewrite the gh-pages index
      └── publish-crds       package the CRDs, upload the bundle to the draft

publish-release              needs ALL of the jobs above; verifies the release's
                             assets, then flips the draft to published
```

Two properties matter when something goes wrong:

- **The GitHub Release is a draft until the very end.** No release page, release
  notes, or release asset is visible until every producer has succeeded. Note
  the scope: this covers the *release*, not the registry. Images and charts are
  publicly pullable the moment their own job succeeds, so a run that fails after
  `publish-images` has left `ghcr.io/.../hub-agent:v0.4.0` reachable even though
  no release mentions it.
- **Charts wait for images.** A chart whose `appVersion` points at images that
  do not exist yet is broken on arrival, so the chart jobs run only after the
  images are pushed and verified.

The two jobs with real branching logic — `create-draft-release` and
`publish-release` — live in [`hack/release/`](hack/release) rather than inline
in the workflow, and are covered by `hack/release/test-release-scripts.sh`,
which CI runs on every change to either.

## Recovering from a failed run

The normal recovery is **Re-run failed jobs** on the workflow run. Jobs that
already succeeded are not re-run, and every job is safe to repeat: the CRD
upload uses `--clobber`, `create-draft-release` reuses the draft it created the
first time, and image pushes rewrite the same tags.

Re-running `publish-images` rebuilds from source rather than reproducing the
earlier build byte-for-byte, so the tag ends up pointing at a *new* digest. That
is harmless while the release is still a draft — nothing has been announced yet
— but it is why a re-run is not an option once a release has been published.

| Where it failed | What is already public | What to do |
| --- | --- | --- |
| `setup` | Nothing | The tag is malformed. Delete it, fix, re-tag. |
| `create-draft-release` | Nothing | See [Re-releasing an existing tag](#re-releasing-an-existing-tag) if it refused because the release is already published. |
| `publish-images` | Any images pushed before the failure (`make push` builds hub-agent, member-agent, then refresh-token in order) | Fix, then re-run failed jobs. |
| `publish-crds` | Possibly the images — it runs in parallel with `publish-images`, not after it | Fix, then re-run failed jobs. |
| `publish-charts-oci` / `publish-charts-pages` | Images; CRD bundle is attached to the still-hidden draft | Fix, then re-run failed jobs. The release stays a draft until the charts land. |
| `publish-release` | Images, charts | The asset check found the draft incomplete or its bundle failed its own checksum. Inspect `gh release view <tag>`, re-upload, re-run failed jobs. |

`publish-charts-pages` serializes across *all* releases, because the action it
uses rewrites the whole `gh-pages` branch. GitHub keeps at most one pending
entry per concurrency group, so if three stable releases overlap, the middle
one's pages job is **cancelled** rather than queued. That leaves its release as
a draft with everything else done; **Re-run failed jobs** finishes it. Cutting
stable releases one at a time avoids the situation entirely.

If the fix requires a code change, the tag must move or be replaced — see
below. Do **not** rebuild a different commit under a tag that already pushed
images.

### Re-releasing an existing tag

`create-draft-release` refuses to run against a release that is already
published. This is deliberate: consumers may already have pinned the images and
charts that release advertises, and a second run would replace them in place
with a different build.

- **If the release should not have gone out** (wrong commit, broken build):
  delete the GitHub Release and the tag, then cut the *next* tag rather than
  reusing the old one — `-rc.N+1` for a release candidate, or the next patch
  version for a stable release. Container tags that have been pulled are not
  safely reusable, and the CRD bundle checksum users recorded would change under
  them. Because the bad images stay pullable under their original tag (see
  [Abandoning a release](#abandoning-a-release)), also delete those package
  versions if the build was actually broken rather than merely superseded.
- **If only one artifact is missing** (for example a chart publish that was
  fixed after the release went public): publish that artifact manually rather
  than re-running the whole workflow. Note that the OCI registry and the Pages
  index are published by two different jobs and need two different fixes:

  ```bash
  # OCI charts
  make helm-push REGISTRY=ghcr.io/kubefleet-dev/kubefleet/charts \
    TAG=v0.4.0 CHART_VERSION=0.4.0

  # Pages index: re-run the publish-charts-pages job from the workflow run,
  # which is the only thing that rewrites the gh-pages branch.
  ```

### Abandoning a release

If a release is called off, delete the draft and the tag so the next attempt
starts clean:

```bash
gh release delete v0.4.0-rc.1 --yes
git push upstream :refs/tags/v0.4.0-rc.1
git tag -d v0.4.0-rc.1
```

That removes the release and the tag, but **not** the artifacts the producer
jobs already published. Those outlive the release and have to be cleaned up
deliberately:

- **Images.** `ghcr.io/kubefleet-dev/kubefleet/<image>:v0.4.0` — and the short
  alias `:0.4.0` for a stable tag — stay publicly pullable. The next tag is a
  different version, so it never supersedes them. Delete the package versions
  (`gh api --method DELETE /orgs/kubefleet-dev/packages/container/<pkg>/versions/<id>`)
  if the build was broken rather than merely renumbered.
- **Charts.** If `publish-charts-oci` ran, the chart is in the OCI registry and
  needs the same treatment. If `publish-charts-pages` ran, the `gh-pages` index
  already advertises the abandoned version, and the next stable release will not
  remove it — the entry has to be dropped from `charts/index.yaml` on the
  `gh-pages` branch by hand.

Abandoning a *stable* release after the chart jobs have run is therefore not
cleanly reversible. Soak on release candidates, which publish neither chart.

## After a release

- At the first RC of a new minor, cut the matching `release-0.Y` branch. From
  then on, fixes land on `main` and are backported with the `cherry-pick/0.Y`
  labels described in
  [CONTRIBUTING.md](CONTRIBUTING.md#backporting-to-release-branches).
- Verify the published release page lists the CRD bundle and its checksum, and
  that the generated notes look right — they come from the `release-note/*`
  labels on the PRs in the release.
- **One-time, at the first stable release cut by this workflow:** the `gh-pages`
  chart index carries stale `hub-agent 0.1.0` and `member-agent 0.1.0` entries
  from before the index was given the real release version. The publish step
  merges into the existing index rather than replacing it, so those entries
  survive and `helm search repo kubefleet --versions` keeps offering `0.1.0`.
  Delete them from `charts/index.yaml` on the `gh-pages` branch (and the
  matching `charts/*-0.1.0.tgz`) once a correctly-versioned entry exists.

## See also

- [VERSIONING.md](VERSIONING.md) — versioning scheme, agent skew, upgrade order.
- [SECURITY.md](SECURITY.md) — supported versions and security-patch policy.
- [CONTRIBUTING.md](CONTRIBUTING.md) — PR conventions, release-note labels, and
  backport policy.
