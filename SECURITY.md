# Security

<!--
TODO: The "Supported versions", "Response SLO", and "Coordinated disclosure" sections
below are provisional pending maintainer agreement on kubefleet-dev/kubefleet#693 (Q3)
and the discussion on PR #713. Update once those decisions land.
-->

The KubeFleet maintainers takes the security of the project very seriously; we greatly welcomes
and appreciates any responsible disclosures of security vulnerabilities.

If you believe you have found a security vulnerability in the repository, please follow the steps
below to report it to the KubeFleet team.

## Supported versions

KubeFleet is pre-1.0 and *targets* an `N`/`N-1` support window: the latest minor release and
the one immediately preceding it receive security patches. The project has maintained a roughly
2–3 month minor-release cadence since `v0.2`, giving approximately four to six months of patch
coverage from the GA of any given minor. Minor cadence slippage is possible while we are pre-1.0.

| Version | Supported |
| --- | --- |
| Latest minor (e.g. `v0.Y.x`) | Yes |
| Previous minor (e.g. `v0.Y-1.x`) | Yes |
| Older minors | No |

"Supported" here refers to security patch backports only. As a pre-1.0 project, KubeFleet does
not guarantee API stability across minor releases. Users on unsupported minors should upgrade to
a supported minor following the project's upgrade documentation; patches are not backported to
EOL releases.

## Response SLO

We commit to the following response targets, measured from the time a report is acknowledged by
the maintainers to the time a patched release is published across all supported minors:

| Severity (CVSS v3.1) | Target time-to-patch |
| --- | --- |
| Critical (9.0+) | 14 days |
| High (7.0–8.9) | 45 days |
| Medium / Low | Best-effort, no committed SLO |

These targets are aspirational while we ramp up to consistent release cadence; we will revisit
them after one full quarterly cycle.

## Coordinated disclosure

KubeFleet follows responsible-disclosure norms but the operational specifics below are still
being finalized:

- **Embargo window: TBD.** We have not yet committed to a fixed number of days between
  vulnerability acknowledgement and public disclosure. At minimum, reporters will be notified
  before public disclosure. The intent is to follow standard CNCF coordinated disclosure
  practice (typically 1–7 days for downstream coordination).
- **Vendor advance notification: TBD.** Projects with downstream consumers commonly operate
  a distributors mailing list for embargo coordination with packagers and downstream forks
  (see the [CNCF TAG-Security `SECURITY.md` template](https://github.com/cncf/tag-security/blob/main/project-resources/templates/SECURITY.md)
  for the conventional `cncf-<project>-distributors-announce@lists.cncf.io` form). Whether
  KubeFleet stands one up depends on demonstrated downstream demand.
- **GitHub private vulnerability reporting:** to be enabled on this repository as the
  preferred reporting channel; the maintainer mailing list (see below) remains the fallback
  until it is.

This section will be updated as each item is decided.

## Reporting Security Issues

**Please do not report security vulnerabilities through public GitHub issues.** Instead, 
report them to the [KubeFleet maintainers](mailto:kubefleet-maintainers@googlegroups.com).
We prefer all communications to be in English.

You should receive a response as soon as possible. If for some reason you do not, please
follow up via email to ensure we received your original message.

Please include the requested information listed below (as much as you can provide) to help
us better understand the nature and scope of the possible issue:

    * Type of issue (e.g. buffer overflow, SQL injection, cross-site scripting, etc.)
    * Full paths of source file(s) related to the manifestation of the issue
    * The location of the affected source code (tag/branch/commit or direct URL)
    * Any special configuration required to reproduce the issue
    * Step-by-step instructions to reproduce the issue
    * Proof-of-concept or exploit code (if possible)
    * Impact of the issue, including how an attacker might exploit the issue

This information will help us process your report more quickly.

Thanks for helping KubeFleet to become more secure!
