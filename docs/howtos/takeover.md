# How-to Guide: Enabling Drift Detection in Fleet

This guide provides an overview on how to set up Fleet's takeover experience, which allows
developers and admins to choose what will happen when Fleet encounters a pre-existing resource.
This occurs most often in the Fleet adoption scenario, where a cluster just joins into a fleet and
the system finds out that the resources to place onto the new member cluster via the CRP API has
already been running there.

A concern commonly associated with this scenario is that the running (pre-existing) set of
resources might have configuration differences from their equivalents on the hub cluster,
for example: On the hub cluster one might have a namespace `work` where it hosts a deployment
`web-server` that runs the image `rpd-stars:latest`; while on the member cluster in the same
namespace lives a deployment of the same name but with the image `umbrella-biolab:latest`.
If Fleet applies the resource template from the hub cluster, unexpected service interruptions
might occur.

To address this concern, Fleet also introduces a new field, `whenToTakeOver`, in the apply
strategy. Two options are available:

* `Always`: this is the default option 😑. With this setting, Fleet will take over a
pre-existing resource as soon as it encounters them. Fleet will apply the corresponding
resource template from the hub cluster, and any value differences in the managed fields
will be overwritten. This is consistent with the behavior before the new takeover experience is
added.
* `IfNoDiff`: this is the new option ✨ provided by the takeover mechanism. With this setting,
Fleet will check for configuration differences when it finds a pre-existing resource and
will only take over the resource (apply the resource template) if no configuration
differences are found. Consider using this option for a safer adoption journey.
* `Never`: this is another new option ✨ provided by the takeover mechanism. With this setting,
Fleet will ignore pre-existing resources and no apply op will be performed. This will be considered
as an apply error. Use this option if you would like to check for the presence of pre-existing
resources without taking any action.

> Before you begin
>
> The new takeover experience is currently in preview. 
>
> Note that the APIs for the new experience are only available in the Fleet v1beta1 API, not the v1 API. If you do not see the new APIs in command outputs, verify that you are explicitly requesting the v1beta1 API objects, as opposed to the v1 API objects (the default). 

## How Fleet can be used to safely take over pre-existing resources

The steps below explain how the takeover experience functions. The code assumes that you have
a fleet of two clusters, `member-1` and `member-2`:

* Switch to the first member cluster, and create a namespace, `work-2`, with labels:

    ```sh
    kubectl config use-context member-2-admin
    kubectl create ns work-2
    kubectl label ns work-2 app=work-2
    kubectl label ns work-2 owner=wesker
    ```

* Switch to the hub cluster, and create the same namespace, but with a slightly different set of labels:

    ```sh
    kubectl config use-context hub-admin
    kubectl create ns work-2
    kubectl label ns work-2 app=work-2
    kubectl label ns work-2 owner=redfield
    ```

* Create a CRP object that places the namespace to all member clusters:

    ```sh
    cat <<EOF | kubectl apply -f -
    # The YAML configuration of the CRP object.
    apiVersion: placement.kubernetes-fleet.io/v1beta1
    kind: ClusterResourcePlacement
    metadata:
      name: work-2
    spec:
      resourceSelectors:
        - group: ""
          kind: Namespace
          version: v1
          # Select all namespaces with the label app=work. 
          labelSelector:
            matchLabels:
              app: work-2
      policy:
        placementType: PickAll
      strategy:
        # For simplicity reasons, the CRP is configured to roll out changes to
        # all member clusters at once. This is not a setup recommended for production
        # use.      
        type: RollingUpdate
        rollingUpdate:
          maxUnavailable: 100%
          unavailablePeriodSeconds: 1
        applyStrategy:
          whenToTakeOver: Never
    EOF
    ```

* Give Fleet a few seconds to handle the placement. Check the status of the CRP object; you should see a failure there that complains about an apply error:

    ```sh
    kubectl get clusterresourceplacement.v1beta1.placement.kubernetes-fleet.io work-2 -o jsonpath='{.status.placementStatuses[?(@.clusterName=="member-2")].conditions[?(@.type=="Applied")]}' | jq
    # The command above uses JSON paths to query the drift details directly and
    # uses the jq utility to pretty print the output JSON.
    #
    # jq might not be available in your environment. You may have to install it
    # separately, or omit it from the command.
    #
    # If the output is empty, the status might have not been populated properly
    # yet. You can switch the output type from jsonpath to yaml to see the full
    # object.
    ```

    The output should look like this:

    ```json
    {
        "lastTransitionTime": "...",
        "message": "...",
        "observedGeneration": ...,
        "reason": "NotAllWorkHaveBeenApplied",
        "status": "False",
        "type": "Applied"
    }
    ```

* You can take a look at the `failedPlacements` field in the placement status for error details:

    ```sh
    kubectl get clusterresourceplacement.v1beta1.placement.kubernetes-fleet.io work-2 -o jsonpath='{.status.placementStatuses[?(@.clusterName=="member-2")].failedPlacements}' | jq
    # The command above uses JSON paths to query the drift details directly and
    # uses the jq utility to pretty print the output JSON.
    #
    # jq might not be available in your environment. You may have to install it
    # separately, or omit it from the command.
    #
    # If the output is empty, the status might have not been populated properly
    # yet. You can switch the output type from jsonpath to yaml to see the full
    # object.
    ```

    The output should look like this:

    ```json
    [
      {
        "condition": {
          "lastTransitionTime": "...",
          "message": "Failed to applied the manifest (error: no ownership of the object in the member cluster; takeover is needed)",
          "reason": "NotTakenOver",
          "status": "False",
          "type": "Applied"
        },
        "kind": "Namespace",
        "name": "work-2",
        "version": "v1"
      }
    ]
    ```

    Fleet finds out that the namespace `work-2` already exists on the member cluster, and
    it is not owned by Fleet; since the takeover policy is set to `Never`, Fleet will not assume
    ownership of the namespace; no apply will be performed and an apply error will be raised
    instead.

* Next, update the CRP object and set the `whenToTakeOver` field to `IfNoDiff`:

    ```sh
    cat <<EOF | kubectl apply -f -
    # The YAML configuration of the CRP object.
    apiVersion: placement.kubernetes-fleet.io/v1beta1
    kind: ClusterResourcePlacement
    metadata:
      name: work-2
    spec:
      resourceSelectors:
        - group: ""
          kind: Namespace
          version: v1
          # Select all namespaces with the label app=work. 
          labelSelector:
            matchLabels:
              app: work-2
      policy:
        placementType: PickAll
      strategy:
        # For simplicity reasons, the CRP is configured to roll out changes to
        # all member clusters at once. This is not a setup recommended for production
        # use.      
        type: RollingUpdate
        rollingUpdate:
          maxUnavailable: 100%
          unavailablePeriodSeconds: 1
        applyStrategy:
          whenToTakeOver: IfNoDiff
    EOF
    ```

* Give Fleet a few seconds to handle the placement. Check the status of the CRP object; you should see the apply op still fails.

    ```sh
    kubectl get clusterresourceplacement.v1beta1.placement.kubernetes-fleet.io work-2
    ```

* Verify the error details reported in the `failedPlacements` field for another time:

     ```sh
    kubectl get clusterresourceplacement.v1beta1.placement.kubernetes-fleet.io work-2 -o jsonpath='{.status.placementStatuses[?(@.clusterName=="member-2")].failedPlacements}' | jq
    # The command above uses JSON paths to query the drift details directly and
    # uses the jq utility to pretty print the output JSON.
    #
    # jq might not be available in your environment. You may have to install it
    # separately, or omit it from the command.
    #
    # If the output is empty, the status might have not been populated properly
    # yet. You can switch the output type from jsonpath to yaml to see the full
    # object.
    ```

    The output have changed:

    ```json
    [
      {
        "condition": {
          "lastTransitionTime": "...",
          "message": "Failed to applied the manifest (error: cannot take over object: configuration differences are found between the manifest object and the corresponding object in the member cluster)",
          "reason": "FailedToTakeOver",
          "status": "False",
          "type": "Applied"
        },
        "kind": "Namespace",
        "name": "work-2",
        "version": "v1"
      }
    ]
    ```

    Now, with the takeover policy set to `IfNoDiff`, Fleet can assume ownership of pre-existing
    resources; however, as a configuration difference has been found between the hub cluster
    and the member cluster, takeover is blocked.

* Similar to the drift detection mechanism, Fleet will report details about the found
configuration differences as well:

    ```sh
    kubectl get clusterresourceplacement.v1beta1.placement.kubernetes-fleet.io work-2 -o jsonpath='{.status.placementStatuses[?(@.clusterName=="member-2")].diffedPlacements}' | jq
    # The command above uses JSON paths to query the drift details directly and
    # uses the jq utility to pretty print the output JSON.
    #
    # jq might not be available in your environment. You may have to install it
    # separately, or omit it from the command.
    #
    # If the output is empty, the status might have not been populated properly
    # yet. You can switch the output type from jsonpath to yaml to see the full
    # object.
    ```

    ```json
    [
        {
            "firstDiffedObservedTime": "...",
            "group": "",
            "version": "v1",
            "kind": "Namespace",    
            "name": "work-2",
            "observationTime": "...",
            "observedDiffs": [
            {
                "path": "/metadata/labels/owner",
                "valueInHub": "redfield",
                "valueInMember": "wesker"
            }
            ],
            "targetClusterObservedGeneration": 0    
        }
    ]
    ```

    Fleet will report the following information about a configuration difference:

    * `group`, `kind`, `version` and `name`: the resource that has configuration differences.
    * `observationTime`: the timestamp where the current diff detail is collected.
    * `firstDiffedObservedTime`: the timestamp where the current diff is first observed.
    * `observedDiffs`: the diff details, specifically:
        * `path`: A JSON path (RFC 6901) that points to the diff'd field;
        * `valueInHub`: the value at the JSON path as seen from the hub cluster resource template
        (the desired state). If this value is absent, the field does not exist in the resource template.
        * `valueInMember`: the value at the JSON path as seen from the member cluster resource
        (the current state). If this value is absent, the field does not exist in the current state.
    * `targetClusterObservedGeneration`: the generation of the member cluster resource.

* To fix the configuration difference, consider one of the following options:

    * Switch the `whenToTakeOver` setting back to `Always`, which will instruct Fleet to take over the resource right away and overwrite all configuration differences; or
    * Edit the diff'd field directly on the member cluster side, so that the value is consistent with that on the hub cluster; Fleet will periodically re-evaluate diffs and should take over the resource soon after.
    * Delete the resource from the member cluster. Fleet will then re-apply the resource template and re-create the resource.

    Here the guide will take the first option available, setting the `whenToTakeOver` field to
    `Always`:

    ```sh
    cat <<EOF | kubectl apply -f -
    # The YAML configuration of the CRP object.
    apiVersion: placement.kubernetes-fleet.io/v1beta1
    kind: ClusterResourcePlacement
    metadata:
      name: work-2
    spec:
      resourceSelectors:
        - group: ""
          kind: Namespace
          version: v1
          # Select all namespaces with the label app=work. 
          labelSelector:
            matchLabels:
              app: work-2
      policy:
        placementType: PickAll
      strategy:
        # For simplicity reasons, the CRP is configured to roll out changes to
        # all member clusters at once. This is not a setup recommended for production
        # use.      
        type: RollingUpdate
        rollingUpdate:
          maxUnavailable: 100%
          unavailablePeriodSeconds: 1
        applyStrategy:
          whenToTakeOver: Always
    EOF
    ```

* Check the CRP status; in a few seconds, Fleet will report that all objects have been applied.

    ```sh
    kubectl get clusterresourceplacement.v1beta1.placement.kubernetes-fleet.io work-2
    ```

    If you switch to the member cluster `member-2` now, you should see that the object looks
    exactly the same as the resource template kept on the hub cluster; the owner label has been
    over-written.

> Important
>
> When Fleet fails to take over an object, the pre-existing resource will not be put under Fleet's management: any change one makes on the hub cluster side will have no effect on the pre-existing resource. If you choose to delete
the resource template, or remove the CRP object, Fleet will not attempt to delete the pre-existing resource.

## Takeover and comparison options

Fleet provides a `comparisonOptions` setting that allows you to fine-tune how Fleet calculate
configuration differences between a resource template created on the hub cluster and the
corresponding pre-existing resource on a member cluster.

> Note
>
> The `comparisonOptions` setting controls also how Fleet detect drifts. See the how-to guide on drift detection for more information.

If `partialComparison` is used, Fleet will only report configuration differences in the managed
fields, i.e., fields that are explictly specified in the resource template; the presence of additional
fields on the member cluster side will not stop Fleet from taking
over the pre-existing resource; on the contrary, with `fullComparsion`, Fleet will only take over
a pre-existing resource if it looks exactly the same as its hub cluster counterpart.

Below is the synergy table that summarizes the combos and their respective effects:

| `whenToTakeOver` setting | `comparisonOption` setting | Configuration difference scenario | Outcome
| -------- | ------- | -------- | ------- |
| `IfNoDiff` | `partialComparison` | There exists a value difference in a managed field between a pre-existing resource on a member cluster and the hub cluster resource template. | Fleet will report an apply error in the status, plus the diff details. |
| `IfNoDiff` | `partialComparison` | The pre-existing resource has a field that is absent on the hub cluster resource template. | Fleet will take over the resource; the configuration difference in the unmanaged field will be left untouched. |
| `IfNoDiff` | `fullComparison` | **Difference has been found on a field, managed or not.** | Fleet will report an apply error in the status, plus the diff details. |
| `Always` | any option | Difference has been found on a field, managed or not. | Fleet will take over the resource; configuration differences in unmanaged fields will be left untouched. |
