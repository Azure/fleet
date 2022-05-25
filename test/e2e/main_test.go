package e2e

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"k8s.io/klog/v2"
	"sigs.k8s.io/e2e-framework/klient/k8s"
	"sigs.k8s.io/e2e-framework/klient/wait"
	"sigs.k8s.io/e2e-framework/klient/wait/conditions"
	"sigs.k8s.io/e2e-framework/pkg/env"
	"sigs.k8s.io/e2e-framework/pkg/envconf"
	"sigs.k8s.io/e2e-framework/pkg/envfuncs"
	"sigs.k8s.io/e2e-framework/third_party/helm"
)

var (
	kindHubName     = "hub-e2e-test"
	kindMemberName  = "Member-e2e-test"
	registry        = os.Getenv("")
	hubImageName    = os.Getenv("")
	memberImageName = os.Getenv("")
	chartsDirectory = "charts/"
	//exampleDirectory = "examples/"
	hubEnv          env.Environment
	memberEnv       env.Environment
	deployNamespace = "fleet-system"
)

func TestMain(m *testing.M) {
	hubEnv = env.NewWithConfig(envconf.New())
	memberEnv = env.NewWithConfig(envconf.New())
	// Create kind clusters
	namespace := "e2e-tests"
	hubEnv.Setup(
		envfuncs.CreateKindClusterWithConfig(kindHubName, "kindest/node:v1.22.2", "kind-config.yaml"),
		envfuncs.LoadDockerImageToCluster(kindHubName, fmt.Sprintf("%s/%s", registry, hubImageName)),
		deployHubChart(deployNamespace),
	).Finish( // Cleanup KinD Cluster
		envfuncs.DeleteNamespace(namespace),
		envfuncs.DestroyKindCluster(kindHubName),
	)
	memberEnv.Setup(
		envfuncs.CreateKindClusterWithConfig(kindMemberName, "kindest/node:v1.22.2", "kind-config.yaml"),
		envfuncs.LoadDockerImageToCluster(kindMemberName, fmt.Sprintf("%s/%s", registry, memberImageName)),
		deployMemberChart(deployNamespace),
	).Finish( // Cleanup KinD Cluster
		envfuncs.DeleteNamespace(namespace),
		envfuncs.DestroyKindCluster(kindMemberName),
	)
	codeExit := hubEnv.Run(m) + memberEnv.Run(m)
	os.Exit(codeExit)
}

func deployHubChart(namespace string) env.Func {

	return func(ctx context.Context, cfg *envconf.Config) (context.Context, error) {
		wd, err := os.Getwd()
		if err != nil {
			return ctx, err
		}
		chartsAbsolutePath, err := filepath.Abs(filepath.Join(wd, "/../../", chartsDirectory))
		if err != nil {
			return ctx, err
		}

		manager := helm.New(cfg.KubeconfigFile())

		// args := fmt.Sprintf("--set scheduler.image=%s --set controller.image=%s",
		// 	fmt.Sprintf("%s/%s", registry, KubeSchedImageName),
		// 	fmt.Sprintf("%s/%s", registry, controllerImageName))

		if err := manager.RunInstall(helm.WithName("hub-agent"),
			helm.WithChart(fmt.Sprintf("%s/%s", chartsAbsolutePath, "hub-agent")),
			//helm.WithArgs(args),
			helm.WithWait(), helm.WithTimeout("1m")); err != nil {
			klog.ErrorS(err, "failed to invoke helm install operation due to an error")
		}

		deployment := &appsv1.Deployment{
			ObjectMeta: v1.ObjectMeta{
				Name:      "hub-agent",
				Namespace: namespace,
			},
			Spec: appsv1.DeploymentSpec{},
		}

		if err := wait.For(conditions.New(cfg.Client().Resources()).ResourceScaled(deployment, func(object k8s.Object) int32 {
			return object.(*appsv1.Deployment).Status.ReadyReplicas
		}, 1), wait.WithTimeout(time.Minute*1)); err != nil {

			klog.ErrorS(err, " Failed to deploy hub agent")
			return ctx, err
		}

		return ctx, nil
	}
}

func deployMemberChart(namespace string) env.Func {

	return func(ctx context.Context, cfg *envconf.Config) (context.Context, error) {
		wd, err := os.Getwd()
		if err != nil {
			return ctx, err
		}
		chartsAbsolutePath, err := filepath.Abs(filepath.Join(wd, "/../../", chartsDirectory))
		if err != nil {
			return ctx, err
		}

		manager := helm.New(cfg.KubeconfigFile())

		// args := fmt.Sprintf("--set scheduler.image=%s --set controller.image=%s",
		// 	fmt.Sprintf("%s/%s", registry, KubeSchedImageName),
		// 	fmt.Sprintf("%s/%s", registry, controllerImageName))

		if err := manager.RunInstall(helm.WithName("member-agent"),
			helm.WithChart(fmt.Sprintf("%s/%s", chartsAbsolutePath, "member-agent")),
			//helm.WithArgs(args),
			helm.WithWait(), helm.WithTimeout("1m")); err != nil {
			klog.ErrorS(err, "failed to invoke helm install operation due to an error")
		}

		deployment := &appsv1.Deployment{
			ObjectMeta: v1.ObjectMeta{
				Name:      "",
				Namespace: namespace,
			},
			Spec: appsv1.DeploymentSpec{},
		}

		if err := wait.For(conditions.New(cfg.Client().Resources()).ResourceScaled(deployment, func(object k8s.Object) int32 {
			return object.(*appsv1.Deployment).Status.ReadyReplicas
		}, 1), wait.WithTimeout(time.Minute*1)); err != nil {

			klog.ErrorS(err, " Failed to deploy member agent")
			return ctx, err
		}

		return ctx, nil
	}
}
