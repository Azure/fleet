Here is how to run e2e locally. Make sure that you have installed Docker and Kind.

1. Add the ArgoCD Helm
```shell
export KUBECONFIG=~/.kube/fleet-hub
helm repo add argo https://argoproj.github.io/argo-helm
helm search repo argo
```

2. Create the CRP on the hub
```shell
kubectl apply -f argoCD_install.yaml 
```

3. Install the argoCD
 ```shell
helm install --create-namespace -n argocd-system aks-demo argo/argo-cd
```

4. Verify on one member cluster that the argoCD is installed
```shell
kubectl get crds -l app.kubernetes.io/part-of=argocd
kubectl -n argocd get secret argocd-initial-admin-secret -o jsonpath="{.data.password}" | base64 -d; echo
kubectl port-forward svc/aks-demo-argocd-server -n argocd-system 8080:443
```
Check the argoCD web on cluster1

5.  Apply the Argo application on the hub
```shell
kubectl  apply -f argo-app.yaml
```

Use a local prometheus to draw graphs. Download prometheus binary for your local machine. Start the prometheus.
```shell
prometheus --config.file=test/e2e/prometheus.yml 
```

6.uninstall the resources
```shell
helm delete aks-demo -n argocd-system
kubectl delete crds -l app.kubernetes.io/part-of=argocd
kubectl delete ns argocd-system
```