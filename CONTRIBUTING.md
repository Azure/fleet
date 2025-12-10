# Contributing

KubeFleet welcomes contributions and suggestions!

## Terms

All contributions to the repository must be submitted under the terms of the [Apache Public License 2.0](https://www.apache.org/licenses/LICENSE-2.0).

## Certificate of Origin

By contributing to this project, you agree to the Developer Certificate of Origin (DCO). This document was created by the Linux Kernel community and is a simple statement that you, as a contributor, have the legal right to make the contribution. See the [DCO](DCO) file for details.

## DCO Sign Off

You must sign off your commit to state that you certify the [DCO](DCO). To certify your commit for DCO, add a line like the following at the end of your commit message:

```
Signed-off-by: John Smith <john@example.com>
```

This can be done with the `--signoff` option to `git commit`. See the [Git documentation](https://git-scm.com/docs/git-commit#Documentation/git-commit.txt--s) for details.

## Code of Conduct

The KubeFleet project has adopted the CNCF Code of Conduct. Refer to our [Community Code of Conduct](CODE_OF_CONDUCT.md) for details.

## Contributing a patch

1. Submit an issue describing your proposed change to the repository in question. The repository owners will respond to your issue promptly.
2. Fork the desired repository, then develop and test your code changes.
3. Submit a pull request.

## Issue and pull request management

Anyone can comment on issues and submit reviews for pull requests. In order to be assigned an issue or pull request, you can leave a `/assign <your Github ID>` comment on the issue or pull request.

## Automated Workflows

### Backport from Upstream

The repository includes an automated backport workflow that syncs changes from the upstream repository (kubefleet-dev/kubefleet).

**Triggering the workflow:**
- **Manual trigger**: Go to Actions → Backport from Upstream → Run workflow
- **Scheduled**: Runs automatically every Monday at 00:00 UTC

**What it does:**
1. Fetches changes from `cncf/main` (kubefleet-dev/kubefleet)
2. Merges using `-X theirs` strategy (upstream takes precedence on conflicts)
3. Updates module paths from `github.com/kubefleet-dev/kubefleet` to `go.goms.io/fleet`
4. Runs `make reviewable` for formatting and linting
5. Creates a PR with a list of backported commits

**Note**: If merge conflicts occur that cannot be resolved automatically, the workflow will fail with instructions for manual resolution.
