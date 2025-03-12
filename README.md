# KubeFleet

![GitHub release (latest by date)][1]
[![Go Report Card][2]][3]
![Build Status][4]
![GitHub go.mod Go version][5]
[![codecov][6]][7]

KubeFleet is an open source solution that works on any Kubernetes cluster. We are working towards the
vision that we will eventually be able to treat each Kubernetes cluster as [cattle](https://cloudscaling.com/blog/cloud-computing/the-history-of-pets-vs-cattle/).

* Join/Leave is a feature that allows a member cluster to join and leave a fleet by registering a custom resource on the fleet's control plane (the hub cluster).
* Workload Orchestration is a feature that allows users to create resources on the hub cluster and then selectively propagate these resources to desired member clusters in the fleet.

## Concepts

**Fleet:** A multi cluster solution that users use to manage Kubernetes clusters.

**Hub cluster:** A Kubernetes cluster that hosts the control plane of the fleet.

**Member cluster:** A Kubernetes cluster that is part of the fleet.

**Fleet-system Namespace:** A reserved namespace in all clusters for running Fleet networking controllers and putting internal resources.

## Quick Start

This section provides a tutorial which explains how to setup and make use of the capabilities provided by fleet

### Prerequisites

- [Docker](https://docs.docker.com/get-docker/)
- [Helm](https://github.com/helm/helm#install)
- [Go](https://golang.org/)
- [kubectl](https://kubernetes.io/docs/tasks/tools/install-kubectl/)
- [kind](https://kind.sigs.k8s.io/)

## Steps to run agents on Kind clusters

export this variable which specifies the number of member clusters that will be created.

```shell
export MEMBER_CLUSTER_COUNT=1
```

from the root directory of the repo run the following command, by default a hub cluster gets created which is the control plane for fleet (**The makefile uses kindest/node:v1.31.0**)

```shell
make setup-clusters
```

then switch context to the hub cluster, 

```shell
kubectl config use-context kind-hub  
```

create a member cluster CR on the hub cluster which allows the member cluster created on the setup step to join the fleet,

```
apiVersion: cluster.kubernetes-fleet.io/v1beta1
kind: MemberCluster
metadata:
  name: kind-cluster-1
spec:
  identity:
    name: fleet-member-agent-cluster-1
    kind: ServiceAccount
    namespace: fleet-system
    apiGroup: ""
```

get the membercluster to see if it has joined the fleet,

```shell
kubectl get memberclusters -A      
```

output is supposed to look like,

```shell
NAME             JOINED   AGE
kind-cluster-1   True     25m
```

Now we can go ahead and use the workload orchestration capabilities offered by fleet, please start with the [concept](https://github.com/Azure/fleet/tree/main/docs/concepts/README.md) to 
understand the details of various features offered by fleet.

## Code of Conduct

This project has adopted the [Microsoft Open Source Code of Conduct](https://opensource.microsoft.com/codeofconduct/). For more information, see the [Code of Conduct FAQ](https://opensource.microsoft.com/codeofconduct/faq) or contact [opencode@microsoft.com](mailto:opencode@microsoft.com) with any additional questions or comments.

## Contributing

The [contribution guide](CONTRIBUTING.md) covers everything you need to know about how you can contribute to KubeFleet.


## Support
For more information, see [SUPPORT.md](SUPPORT.md).

[1]:  https://img.shields.io/github/v/release/Azure/fleet
[2]:  https://goreportcard.com/badge/go.goms.io/fleet
[3]:  https://goreportcard.com/report/go.goms.io/fleet
[4]:  https://codecov.io/gh/Azure/fleet/branch/main/graph/badge.svg?token=D3mtbzACjC
[5]:  https://img.shields.io/github/go-mod/go-version/Azure/fleet
[6]: https://opensource.microsoft.com/codeofconduct/
[7]: https://opensource.microsoft.com/codeofconduct/faq
