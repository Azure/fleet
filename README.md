# KubeFleet

![GitHub release (latest by date)][1]
[![Go Report Card][2]][3]
![Build Status][4]
![GitHub go.mod Go version][5]
[![Slack](https://img.shields.io/badge/slack-join-brightgreen)](https://slack.cncf.io)

![cncf_logo](screenshots/cncf-logo.png)

KubeFleet is a sandbox project of the [Cloud Native Computing Foundation](https://cncf.io/) (CNCF) that works on any Kubernetes cluster.
We are working towards the vision where we will eventually be able to treat each Kubernetes cluster as a [cattle](https://cloudscaling.com/blog/cloud-computing/the-history-of-pets-vs-cattle/).

## What is KubeFleet?
KubeFleet contains a set of Kubernetes controllers and CRDs which provide an advanced cloud-native solution for multi-cluster application management.

Use KubeFleet to schedule workloads smartly, roll out changes progressively, and perform administrative tasks easily, across a group of Kubernetes clusters on any cloud or on-premises clusters.


## Quickstart

* [Get started here](https://kubefleet-dev.github.io/website/docs/getting-started/kind/)

## Key benefits and capabilities

### Centralized policy-driven fleet governance
KubeFleet utilizes a hub-spoke architecture that creates a single control plane for the fleet. It allows fleet administrators to apply uniform cloud native policies on every member cluster, whether they reside in public clouds, private data centers, or edge locations. This greatly simplifies governance across large, geographically distributed fleets spanning hybrid and multi-cloud environments.

### Progressive Rollouts with Safeguards
KubeFleet provides a cloud native progressive rollout plans sequence updates across the entire fleet with health verification at each step. The application owner can pause or rollback to any previous versions when they observe failures, limiting blast radius. This keeps multi-cluster application deployments reliable and predictable spanning edge, on-premises, and cloud environments.

### Powerful Multi-Cluster Scheduling
KubeFleet's scheduler evaluates member cluster properties, available capacity, and declarative placement policies to select optimal destinations for workloads. It supports cluster affinity and anti-affinity rules, topology spread constraints to distribute workloads across failure domains, and resource-based placement to ensure sufficient compute, memory, and storage. The scheduler continuously reconciles as fleet conditions change, automatically adapting to cluster additions, removals, or capacity shifts across edge, on-premises, and cloud environments. For more details, please refer to the [KubeFleet website](https://kubefleet.dev/docs/).

## Documentation

To learn more about KubeFleet go to the [KubeFleet documentation](https://kubefleet-dev.github.io/website/).

## Community

You can reach the KubeFleet community and developers via the following channels:

* Q & A: [GitHub Discussions](https://github.com/kubefleet-dev/kubefleet/discussions)
* Slack: [The #KubeFleet Slack channel](https://cloud-native.slack.com/archives/C08KR7589R8) 
* Mailing list: [mailing list](https://groups.google.com/g/kubefleet-dev)

## Community Meetings

We host bi-weekly community meetings that alternate between US/EU and APAC friendly time. In these sessions the community will showcase demos and discuss the current and future state of the project.

Please refer to the [calendar](https://zoom-lfx.platform.linuxfoundation.org/meetings/kubefleet?view=month) for the latest schedule:
* Wednesdays at 09:30am PT [US/EU](https://zoom-lfx.platform.linuxfoundation.org/meeting/93624014488?password=27667a5c-9238-4b5a-b4d8-96daadaa9fa4) (weekly). [Convert to your timezone](https://dateful.com/convert/pacific-time-pt?t=930am).
* Thursday at 9:00am CST [APAC](https://zoom-lfx.platform.linuxfoundation.org/meeting/98901589453?password=9ab588fd-1214-40c3-84c2-757c124e984f) (biweekly).  [Convert to your timezone](https://dateful.com/convert/beijing-china?t=9am).

For more meeting information, minutes and recordings, please see the [KubeFleet community meeting doc](https://docs.google.com/document/d/1iMcHn11fPlb9ZGoMHiGEBvdIc44W1CjZvsPH3eBg6pA/edit?usp=sharing).

## Code of Conduct
Participation in KubeFleet is governed by the [CNCF Code of Conduct](https://github.com/cncf/foundation/blob/master/code-of-conduct.md). See the [Code of Conduct](CODE_OF_CONDUCT.md) for more information.

## Contributing

The [contribution guide](CONTRIBUTING.md) covers everything you need to know about how you can contribute to KubeFleet.

## Support
For more information, see [SUPPORT](SUPPORT.md).


[1]:  https://img.shields.io/github/v/release/kubefleet-dev/kubefleet
[2]:  https://goreportcard.com/badge/go.goms.io/fleet
[3]:  https://goreportcard.com/report/go.goms.io/fleet
[4]:  https://codecov.io/gh/Azure/fleet/branch/main/graph/badge.svg?token=D3mtbzACjC
[5]:  https://img.shields.io/github/go-mod/go-version/kubefleet-dev/kubefleet

Copyright The KubeFleet Authors.
The Linux Foundation® (TLF) has registered trademarks and uses trademarks. For a list of TLF trademarks, see [Trademark Usage](https://www.linuxfoundation.org/trademark-usage/).
