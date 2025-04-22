# KubeFleet Roadmap

## Project Website
- Setup the project website

## Support more cluster properties so that user can pick the right cluster for their workload
- Support node level SKU as properties, e.g. CPU, GPU, Memory, etc 
  - The application admin can choose clusters that have nodes with H100 GPU.
  - The application admin can choose clusters that have nodes with 128GB memory.
- Support network topology
  - The application admin can choose the clusters with requires infiniband, or 100Gbps network.

## Support scheduling for namespaced resources (heterogeneous namespace)
- Support independent scheduling policy for namespaced resources
  - e.g. The application admin can pick one workload in a namespace to cluster A while the other workload in the same namespace to cluster B.
  
## Support Job dispatching
- Support the use case to use the fleet as a super computer to run hyper scale applications

## Dynamic scheduling
- De-scheduler for the fleet
  - The de-scheduler would move the workload to the right cluster if the cluster is not the best fit for the workload anymore.
- Rebalance the workload
  - The application admin can rebalance the workload to make sure the workload is spread evenly across the clusters.

## Support anti-affinity for workload
- Support affinity/anti-affinity for their workload.
    - The application admin can specify that their workload A needs to be placed on the same clusters that workload B runs.
    - The application admin can specify that their workload A cannot be placed on the same clusters that workload B runs.

## Support Customized health check for workload
- Support user specified health check for their workload.
    - The application admin can provide a customized health check for their workload.

## Support Spread mode for workload
- The application admin can specify a spread mode for their workload.
    - The move between clusters would follow the max-unavailable/min-available pods rule.

## Support identity federation
- The member agent can assume the identity of the operator on the hub cluster and not using admin privilege when applying the resources.
