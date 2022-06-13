/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package utils

import (
	"context"
	"crypto/rand"
	"flag"
	"log"
	"math/big"
	"path/filepath"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
)

const (
	NamespaceNameFormat   = "fleet-%s"
	RoleNameFormat        = "fleet-role-%s"
	RoleBindingNameFormat = "fleet-rolebinding-%s"
)

// Condition types.
const (
	// ConditionTypeReady resources are believed to be ready to handle work.
	ConditionTypeReady string = "Ready"

	// ConditionTypeSynced resources are believed to be in sync with the
	// Kubernetes resources that manage their lifecycle.
	ConditionTypeSynced string = "Synced"
)

// Reasons a resource is or is not synced.
const (
	ReasonReconcileSuccess string = "ReconcileSuccess"
	ReasonReconcileError   string = "ReconcileError"
)

// ReconcileErrorCondition returns a condition indicating that we encountered an
// error while reconciling the resource.
func ReconcileErrorCondition(err error) metav1.Condition {
	return metav1.Condition{
		Type:    ConditionTypeSynced,
		Status:  metav1.ConditionFalse,
		Reason:  ReasonReconcileError,
		Message: err.Error(),
	}
}

// ReconcileSuccessCondition returns a condition indicating that we successfully reconciled the resource
func ReconcileSuccessCondition() metav1.Condition {
	return metav1.Condition{
		Type:   ConditionTypeSynced,
		Status: metav1.ConditionTrue,
		Reason: ReasonReconcileSuccess,
	}
}

func GetKubeConfigOfCurrentCluster() (rest.Config, error) {
	var kubeConfig *string
	if home := homedir.HomeDir(); home != "" {
		kubeConfig = flag.String("kubeConfig", filepath.Join(home, ".kube", "config"), "(optional) absolute path to the kubeConfig file")
	} else {
		kubeConfig = flag.String("kubeConfig", "", "absolute path to the kubeConfig file")
	}
	flag.Parse()
	cf, err := clientcmd.BuildConfigFromFlags("", *(kubeConfig))
	return *(cf), err
}

func GetEventWatcherForCurrentCluster(ctx context.Context, namespace string) (watch.Interface, error) {
	config, err := GetKubeConfigOfCurrentCluster()
	if err != nil {
		log.Fatal(err.Error())
	}
	clientSet, err := kubernetes.NewForConfig(&config)
	if err != nil {
		log.Fatal(err.Error())
	}

	return clientSet.CoreV1().Events(namespace).Watch(ctx, metav1.ListOptions{})
}

func RandStr() string {
	const length = 10 // specific size to avoid user passes in unreasonably large size, causing runtime error
	const letters = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz-"
	ret := make([]byte, length)
	for i := 0; i < length; i++ {
		num, err := rand.Int(rand.Reader, big.NewInt(int64(len(letters))))
		if err != nil {
			return ""
		}
		ret[i] = letters[num.Int64()]
	}

	return string(ret)
}
