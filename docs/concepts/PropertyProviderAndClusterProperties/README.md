# Property Provider and Cluster Properties

> Note
>
> Property Provider and Cluster Properties are Fleet preview features.

This document explains the concepts of property provider and cluster properties in Fleet.

Fleet allows developers to implement a property provider to expose arbitrary properties about
a member cluster, such as its node count and available resources for workload placement. Platforms
could also enable their property providers to expose platform-specific properties via Fleet.
These properties can be useful in a variety of cases: for example, administrators could monitor the
health of a member cluster using related properties; Fleet also supports making scheduling
decisions based on the property data. 

## Property provider

A property provider implements Fleet's property provider interface:

```go
// PropertyProvider is the interface that every property provider must implement.
type PropertyProvider interface {
	// Collect is called periodically by the Fleet member agent to collect properties.
	//
	// Note that this call should complete promptly. Fleet member agent will cancel the
	// context if the call does not complete in time.
	Collect(ctx context.Context) PropertyCollectionResponse
	// Start is called when the Fleet member agent starts up to initialize the property provider.
	// This call should not block.
	//
	// Note that Fleet member agent will cancel the context when it exits.
	Start(ctx context.Context, config *rest.Config) error
}
```

For the details, see the [Fleet source code](../../../pkg/propertyprovider/interface.go).

A property provider should be shipped as a part of the Fleet member agent and run alongside it.
Refer to the [Fleet source code](../../../cmd/memberagent/main.go)
for specifics on how to set it up with the Fleet member agent.
At this moment, only one property provider can be set up with the Fleet member agent at a time.
Once connected, the Fleet member agent will attempt to start it when
the agent itself initializes; the agent will then start collecting properties from the
property provider periodically.

A property provider can expose two types of properties: resource properties, and non-resource
properties. To learn about the two types, see the section below. In addition, the provider can
choose to report its status, such as any errors encountered when preparing the properties,
in the form of Kubernetes conditions.

The Fleet member agent can run with or without a property provider. If a provider is not set up, or
the given provider fails to start properly, the agent will collect limited properties about
the cluster on its own, specifically the total and allocatable CPU and memory capacities of
the host member cluster. 

## Cluster properties

A cluster property is an attribute of a member cluster. There are two types of properties:

* Resource property: the usage information of a resource in a member cluster; the
name of the resource should be in the format of
[a Kubernetes label key](https://kubernetes.io/docs/concepts/overview/working-with-objects/labels/#syntax-and-character-set),
such as `cpu` and `memory`, and the usage information should consist of:

    * the total capacity of the resource, which is the amount of the resource
    installed in the cluster;
    * the allocatable capacity of the resource, which is the maximum amount of the resource 
    that can be used for running user workloads, as some amount of the resource might be
    reserved by the OS, kubelet, etc.;
    * the available capacity of the resource, which is the amount of the resource that
    is currently free for running user workloads.

    Note that you may report a virtual resource via the property provider, if applicable.

* Non-resource property: a metric about a member cluster, in the form of a key/value
pair; the key should be in the format of
[a Kubernetes label key](https://kubernetes.io/docs/concepts/overview/working-with-objects/labels/#syntax-and-character-set),
such as `kubernetes-fleet.io/node-count`, and the value at this moment should be a sortable
numeric that can be parsed as
[a Kubernetes quantity](https://kubernetes.io/docs/reference/kubernetes-api/common-definitions/quantity/).

Eventually, all cluster properties are exposed via the Fleet `MemberCluster` API, with the
non-resource properties in the `.status.properties` field and the resource properties
`.status.resourceUsage` field:

```yaml
apiVersion: cluster.kubernetes-fleet.io/v1beta1
kind: MemberCluster
metadata: ...
spec: ...
status:
  agentStatus: ...
  conditions: ...
  properties:
    kubernetes-fleet.io/node-count:
      observationTime: "2024-04-30T14:54:24Z"
      value: "2"
    ...
  resourceUsage:
    allocatable:
      cpu: 32
      memory: "16Gi"
    available:
      cpu: 2
      memory: "800Mi"
    capacity:
      cpu: 40
      memory: "20Gi"
```

Note that conditions reported by the property provider (if any), would be available in the
`.status.conditions` array as well.
