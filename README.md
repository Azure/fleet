# Fleet

![GitHub release (latest by date)][1]
[![Go Report Card][2]][3]
![Build Status][4]
![GitHub go.mod Go version][5]
[![codecov][6]][7]

Fleet Join/Leave is a feature that allows a member cluster to join and leave a fleet(Hub) in the fleet control plane.

## Quick Start

---

### Prerequisites

- [Docker](https://docs.docker.com/get-docker/)
- [Helm](https://github.com/helm/helm#install) version 3.6+
- [Go](https://golang.org/) version v1.17
- [kubectl](https://kubernetes.io/docs/tasks/tools/install-kubectl/) version v1.22
- [kind](https://kind.sigs.k8s.io/) version v0.12.0

### Install

1. Clone the repo to your machine

```shell
$ git clone https://github.com/Azure/fleet
```

2. Navigate to fleet directory

```shell
$ cd fleet
```

3. Set up `hub` and `member` kind clusters

```shell
$ make create-hub-kind-cluster create-member-kind-cluster
```

4. Build and load images to kind clusters (only if you don't have access to [fleet packages](https://github.com/orgs/Azure/packages?repo_name=fleet))

```shell
 $ OUTPUT_TYPE=type=docker make docker-build-hub-agent docker-build-member-agent docker-build-refresh-token
 $ make load-hub-docker-image load-member-docker-image
```

5. Install hub and member agents helm charts

```shell
$ make install-member-agent-helm
```

### Demo

1. Get Hub api-server server

```shell
$ docker inspect hub-testing-control-plane --format='{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}'

172.19.0.2
```

2. Apply `memberCluster` to the hub cluster

```shell
$ kind export kubeconfig --name hub-testing
$ kubectl apply -f examples/fleet_v1alpha1_membercluster.yaml 
```

3. Check to make sure the `memberCluster` & `internalMemberCluster` resources status have been updated to 'Joined'

```shell
$ kind export kubeconfig --name hub-testing
$ kubectl describe memberCluster.fleet.azure.com kind-member-testing
$ kubectl describe internalMemberCluster.fleet.azure.com kind-member-testing
 ```

<details>
<summary>Result</summary>

```shell
Name:         kind-member-testing
Namespace:    
    ...
    Reason:                InternalMemberClusterHeartbeatReceived
    Status:                True
    Type:                  HeartbeatReceived
    Last Transition Time:  2022-06-27T19:26:38Z
    Message:               
    Observed Generation:   1
    Reason:                MemberClusterJoined
    Status:                True
    Type:                  Joined
Events:
  Type    Reason                        Age   From           Message
  ----    ------                        ----  ----           -------
  Normal  NamespaceCreated              77s   memberCluster  Namespace was created
  Normal  InternalMemberClusterCreated  77s   memberCluster  Internal member cluster was created
  Normal  RoleCreated                   77s   memberCluster  role was created
  Normal  RoleBindingCreated            77s   memberCluster  role binding was created
  Normal  MemberClusterJoined           17s   memberCluster  member cluster is joined

```

<summary>Result</summary>

</details><br/>

4. Change the state for `memberCluster` yaml file to be `Leave` and apply the change.

```shell
$ kind export kubeconfig --name hub-testing
$ kubectl apply -f examples/fleet_v1alpha1_membercluster.yaml 
```

5. Check to make sure the `memberCluster` & `internalMemberCluster` resources status have been updated to 'Left'

```shell
$ kind export kubeconfig --name hub-testing
$ kubectl describe memberCluster.fleet.azure.com kind-member-testing
$ kubectl describe internalMemberCluster.fleet.azure.com kind-member-testing
 ```

<details>
<summary>Result</summary>

```shell
Name:         kind-member-testing
Namespace:    
    ...
    Reason:                InternalMemberClusterHeartbeatReceived
    Status:                True
    Type:                  HeartbeatReceived
    Last Transition Time:  2022-06-27T19:26:38Z
    Message:               
    Observed Generation:   1
    Reason:                MemberClusterJoined
    Status:                False
    Type:                  Joined
Events:
  Type    Reason                        Age   From           Message
  ----    ------                        ----  ----           -------
  Normal  NamespaceCreated              77s   memberCluster  Namespace was created
  Normal  InternalMemberClusterCreated  77s   memberCluster  Internal member cluster was created
  Normal  RoleCreated                   77s   memberCluster  role was created
  Normal  RoleBindingCreated            77s   memberCluster  role binding was created
  Normal  MemberClusterJoined           17s   memberCluster  member cluster is joined
  Normal  InternalMemberClusterSpecUpdated  3m10s  memberCluster  internal member cluster spec is marked as Leave
  Normal  MemberClusterJoined           3m15s   memberCluster  member cluster is Left 

```

</details><br/>

### Cleanup

delete kind clusters setup

```shell
$ make clean-e2e-tests
```

## Code of Conduct

---

This project has adopted the [Microsoft Open Source Code of Conduct][8]. For more information, see the [Code of Conduct FAQ][9] or contact [opencode@microsoft.com][19] with any additional questions or comments.

## Contributing

---

## Support

---

Azure fleet is an open source project that is [**not** covered by the Microsoft Azure support policy][10]. [Please search open issues here][11], and if your issue isn't already represented please [open a new one][12]. The project maintainers will respond to the best of their abilities.

[1]:  https://img.shields.io/github/v/release/Azure/fleet
[2]:  https://goreportcard.com/badge/go.goms.io/fleet
[3]:  https://goreportcard.com/report/go.goms.io/fleet
[4]:  https://github.com//Azure/fleet/actions/workflows/workflow.yml/badge.svg
[5]:  https://img.shields.io/github/go-mod/go-version/Azure/fleet
[6]:  https://codecov.io/gh/Azure/fleet/branch/main/graph/badge.svg?token=D3mtbzACjC
[7]:  https://codecov.io/gh/Azure/fleet
[8]: https://opensource.microsoft.com/codeofconduct/
[9]: https://opensource.microsoft.com/codeofconduct/faq
[10]: https://support.microsoft.com/en-us/help/2941892/support-for-linux-and-open-source-technology-in-azure
[11]: https://github.com/Azure/fleet/issues
[12]: https://github.com/Azure/fleet/issues/new
