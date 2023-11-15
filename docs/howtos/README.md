# Fleet How-To Guides

The Fleet documentation provides a number of how-to guides that help you get familiar with
specific Fleet tasks, such as how to use `ClusterResourcePlacement`, a Fleet API, to place
resources across different clusters.

> Note
>
> If you are just getting started with Fleet, it is recommended that you refer to the
> [Fleet Getting Started Guide](../../README.md) for how to create a fleet. an overview of Fleet
> features and capabilities.

Below is a walkthrough of all the how-to guides currently available, categorized by their
domains:

## `ClusterResourcePlacement` API

* [Using the `ClusterResourcePlacement` API to place resources](crp.md)

    This how-to guide explains the specifics of the `ClusterResourcePlacement` API, including its
    resource selectors, scheduling policy, rollout strategy, and more. `ClusterResourcePlacement`
    is a core Fleet API that allows easy and flexible distribution of resources to clusters. 

* [Using Affinity to Pick Clusters](affinities.md)

    This how-to guide explains in depth the concept and usage of affinities terms in the
    `ClusterResourcePlacement` API, which you can leverage to place resources on specific
    clusters or specify a preference.

* [Using Topology Spread Constraint to Pick Clusters](topology-spread-constraints.md)

    This how-to guide explains in depth the concept and usage of topology spread constraints
    in the `ClusterResourcePlacement API`, which you can leverage to spread resources evenly
    across different groups of clusters, so as to achieve, for example, high availability and
    elimination of resource usage hotspots.

* [Understanding the `ClusterResourcePlacement` Status](crp-status.md)

    This how-to guide explains in depth the status Fleet reports in `ClusterResourcePlacement`
    API objects, which you can read about to track which clusters Fleet has picked for a
    resource placement and whether a placement has been successfully completed.
