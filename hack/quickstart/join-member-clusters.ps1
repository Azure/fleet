# Note: you must have at least one hub cluster and one member cluster.
#
# The reason we require the hub cluster API URL as an argument is that, in some environments (e.g. kind), the URL cannot be derived from kubeconfig and needs to be explicitly provided by users.
# 
# For example, using Docker, you can get the right IP address for member clusters to use:
#    docker inspect local-hub-01-control-plane --format='{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}'
#
#    You can assume port 6443 unless you have explicitly changed the API server port for your hub cluster. Don't use the mapped Docker port (i.e. 50063)
#
# Example usage:
#   ./join-member-clusters.ps1 0.2.2 demo-hub-01 https://172.18.0.2:6443 member-cluster-1 member-cluster-2

[CmdletBinding()]
param(
    [Parameter(Position = 0)]
    [string]$KubefleetVersion,

    [Parameter(Position = 1)]
    [string]$HubClusterName,

    [Parameter(Position = 2)]
    [string]$HubControlPlaneURL,

    [Parameter(Position = 3, ValueFromRemainingArguments = $true)]
    [string[]]$MemberClusterNames,

    [switch]$Help
)

function Show-Usage {
    @'
Usage:
    ./join-member-clusters.ps1 <kubefleet-version> <hub-cluster-name> <hub-control-plane-url> <member-cluster-name-1> [<member-cluster-name-2> ...]

Example:
    ./join-member-clusters.ps1 0.2.2 demo-hub-01 https://172.18.0.2:6443 member-cluster-1 member-cluster-2

Requirements:
    - PowerShell 7, kubectl, and helm must be installed
    - hub and member cluster names must exist in your kubeconfig
'@
}

function Fail-WithHelp {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Message
    )

    Write-Error $Message
    Write-Host ""
    Show-Usage
    exit 1
}

function Test-CommandExists {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Name
    )

    return $null -ne (Get-Command $Name -ErrorAction SilentlyContinue)
}

function Test-ClusterExists {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Name
    )

    $clusters = @(kubectl config get-clusters 2>$null)
    if ($LASTEXITCODE -ne 0) {
        return $false
    }

    return $clusters | Select-Object -Skip 1 | Where-Object { $_.Trim() -eq $Name } | ForEach-Object { $true } | Select-Object -First 1
}

if ($Help) {
    Show-Usage
    exit 0
}

if ([string]::IsNullOrWhiteSpace($KubefleetVersion) -or [string]::IsNullOrWhiteSpace($HubClusterName) -or [string]::IsNullOrWhiteSpace($HubControlPlaneURL) -or $null -eq $MemberClusterNames -or $MemberClusterNames.Count -lt 1) {
    $argumentCount = 0
    if (-not [string]::IsNullOrWhiteSpace($KubefleetVersion)) {
        $argumentCount++
    }
    if (-not [string]::IsNullOrWhiteSpace($HubClusterName)) {
        $argumentCount++
    }
    if (-not [string]::IsNullOrWhiteSpace($HubControlPlaneURL)) {
        $argumentCount++
    }
    if ($null -ne $MemberClusterNames) {
        $argumentCount += $MemberClusterNames.Count
    }

    Fail-WithHelp "expected at least 4 arguments, got $argumentCount"
}

if (-not (Test-CommandExists -Name kubectl)) {
    Fail-WithHelp "kubectl is not installed or not available in PATH"
}

if (-not (Test-CommandExists -Name helm)) {
    Fail-WithHelp "helm is not installed or not available in PATH"
}

if (-not (Test-ClusterExists -Name $HubClusterName)) {
    Fail-WithHelp "hub cluster '$HubClusterName' was not found in kubeconfig"
}

[System.Uri]$parsedHubURL = $null
if (-not [System.Uri]::TryCreate($HubControlPlaneURL, [System.UriKind]::Absolute, [ref]$parsedHubURL)) {
    Fail-WithHelp "hub control plane URL '$HubControlPlaneURL' is not a valid absolute URL"
}

if ($parsedHubURL.Scheme -ne "https") {
    Fail-WithHelp "hub control plane URL must use https"
}

# Extract the hub cluster CA for secure TLS verification
$jsonpath = "{.clusters[?(@.name==""$HubClusterName"")].cluster.certificate-authority-data}"
$HubCA = kubectl config view --raw -o "jsonpath=$jsonpath"
if ([string]::IsNullOrWhiteSpace($HubCA)) {
    Write-Error "Failed to extract certificate authority data from hub cluster '$HubClusterName'"
    exit 1
}

foreach ($memberClusterName in $MemberClusterNames) {
    if ([string]::IsNullOrWhiteSpace($memberClusterName)) {
        Fail-WithHelp "member cluster name cannot be empty"
    }

    if ($memberClusterName -eq $HubClusterName) {
        Fail-WithHelp "member cluster '$memberClusterName' cannot be the same as hub cluster"
    }

    if (-not (Test-ClusterExists -Name $memberClusterName)) {
        Fail-WithHelp "member cluster '$memberClusterName' was not found in kubeconfig"
    }
}

foreach ($memberClusterName in $MemberClusterNames) {
    # Note that Fleet will recognize your cluster with this name once it joins.
    $serviceAccount = "$memberClusterName-hub-cluster-access"
    $serviceAccountSecret = "$memberClusterName-hub-cluster-access-token"

    Write-Host "Switching into hub cluster context..."
    kubectl config use-context $HubClusterName

    # The service account can, in theory, be created in any namespace; for simplicity reasons,
    # here you will use the namespace reserved by Fleet installation, `fleet-system`.
    #
    # Note that if you choose a different value, commands in some steps below need to be
    # modified accordingly.
    Write-Host "Creating member service account..."
    kubectl create serviceaccount $serviceAccount -n fleet-system

    Write-Host "Creating member service account secret..."
    @"
apiVersion: v1
kind: Secret
metadata:
    name: $serviceAccountSecret
    namespace: fleet-system
    annotations:
        kubernetes.io/service-account.name: $serviceAccount
type: kubernetes.io/service-account-token
"@ | kubectl apply -f -

    Write-Host "Creating member cluster custom resource on hub cluster..."
    $encodedToken = kubectl get secret $serviceAccountSecret -n fleet-system -o "jsonpath={.data.token}"
    $token = [System.Text.Encoding]::UTF8.GetString([System.Convert]::FromBase64String($encodedToken))

    @"
apiVersion: cluster.kubernetes-fleet.io/v1
kind: MemberCluster
metadata:
    name: $memberClusterName
spec:
    identity:
        name: $memberClusterName-hub-cluster-access
        kind: ServiceAccount
        namespace: fleet-system
        apiGroup: ""
    heartbeatPeriodSeconds: 15
"@ | kubectl apply -f -

    # Install the member agent helm chart on the member cluster.
    Write-Host "Switching to member cluster context..."
    kubectl config use-context $memberClusterName

    # Create the secret with the token extracted previously for member agent to use.
    Write-Host "Creating secret..."
    kubectl delete secret hub-kubeconfig-secret
    kubectl create secret generic hub-kubeconfig-secret --from-literal="token=$token"

    Write-Host "Uninstalling any existing member-agent instances..."
    helm uninstall member-agent -n fleet-system --wait

    Write-Host "Installing member-agent..."
    helm install member-agent oci://ghcr.io/kubefleet-dev/kubefleet/charts/member-agent `
        --version $KubefleetVersion `
        --set "config.hubURL=$HubControlPlaneURL" `
        --set "config.hubCA=$HubCA" `
        --set "config.memberClusterName=$memberClusterName" `
        --set logFileMaxSize=100000 `
        --namespace fleet-system `
        --create-namespace

    kubectl get pods -A
    kubectl config use-context $HubClusterName
    kubectl get membercluster $memberClusterName
}
