# Fleet Tutorials

Welcome to the Fleet Tutorials! This guide will help you understand how Fleet can seamlessly integrate with your development and operations workflows. 
Follow the instructions provided to get the most out of Fleet's features.

> Note
>
> If you are just getting started with Fleet, it is recommended that you refer to the
> [Fleet Getting Started Guide](../../README.md) for how to create a fleet and [Fleet Concepts](../concepts/README.md)
> for an overview of Fleet features and capabilities.


Below is a walkthrough of all the how-to guides currently available:
* [Migrating Applications to Another Cluster When a Cluster Goes Down ](ClusterMigrationDR.md)

  This tutorial guides you through migrating application resources across regions using Fleet. 
  If a region experiences an outage, you can update the `ClusterResourcePlacement` (CRP) to redeploy your Kubernetes Service 
  and Deployment in another region, ensuring continued availability and resilience.

* [Migrating Application Resources to Clusters with More Availability](MigrationWithOverrideDR.md)

  This tutorial will guide you through migrating your application resources to clusters with higher availability using Fleet. 
  This process not only ensures your application is deployed in clusters with better resource availability but also scales up the number of replicas to enhance performance and reliability.