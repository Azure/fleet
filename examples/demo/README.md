Here is how to run e2e locally. Make sure that you have installed Docker and Kind.

1. Install mongo db
```shell
helm install ryan-mongodb -n mongodb-system --create-namespace  azure-marketplace/mongodb-sharded --set service.type=LoadBalancer,configsvr.pdb.create=true,configsvr.persistence.storageClass=managed-csi

kubectl apply -f mongo_install.yaml
```

2. Verify on the member cluster
```shell
helm list -n mongodb-system

export MONGODB_ROOT_PASSWORD=$(kubectl get secret --namespace mongodb-system ryan-mongodb-mongodb-sharded -o jsonpath="{.data.mongodb-root-password}" | base64 -d)
kubectl run --namespace mongodb-system ryan-mongodb-mongodb-sharded-client --rm --tty -i --restart='Never' --image marketplace.azurecr.io/bitnami/mongodb-sharded:6.0.1-debian-11-r1 --command -- mongosh admin --host ryan-mongodb-mongodb-sharded --authenticationDatabase admin -u root -p $MONGODB_ROOT_PASSWORD

export SERVICE_IP=$(kubectl get svc --namespace mongodb-system ryan-mongodb-mongodb-sharded --include "{{ range (index .status.loadBalancer.ingress 0) }}{{ . }}{{ end }}")
mongosh --host $SERVICE_IP --port 27017 --authenticationDatabase admin -p $MONGODB_ROOT_PASSWORD
```
 
3. Add the ArgoCD Helm
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
kubectl -n argocd-system get secret argocd-initial-admin-secret -o jsonpath="{.data.password}" | base64 -d; echo
kubectl port-forward svc/aks-demo-argocd-server -n argocd-system 8080:443
```
Check the argoCD web on cluster1

5.  Apply the Argo application on the hub
```shell
kubectl apply -f argo-app.yaml
```

6. Check the member cluster1 and cluster2 web

7. Change the Git Repo

8. Delete the argo application
```shell
kubectl delete -f argo-app.yaml
```

9.uninstall the resources
```shell
helm delete ryan-mongodb -n mongodb-system 
helm delete aks-demo -n argocd-system
kubectl delete crds -l app.kubernetes.io/part-of=argocd
kubectl delete ns argocd-system
```