//go:build e2e
// +build e2e

package e2e

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"k8s.io/klog/v2"
	"sigs.k8s.io/e2e-framework/klient/wait"
	"sigs.k8s.io/e2e-framework/klient/wait/conditions"
	"sigs.k8s.io/e2e-framework/pkg/env"
	"sigs.k8s.io/e2e-framework/pkg/envconf"
	"sigs.k8s.io/e2e-framework/pkg/envfuncs"
	"sigs.k8s.io/e2e-framework/third_party/helm"
)

var (
	kindHubName        = "hub-e2e-test"
	kindMemberName     = "member-e2e-test"
	registry           = os.Getenv("REGISTRY")
	hubImageName       = os.Getenv("HUB_AGENT_IMAGE_NAME")
	memberImageName    = os.Getenv("MEMBER_AGENT_IMAGE_NAME")
	hubImageVersion    = os.Getenv("HUB_AGENT_IMAGE_VERSION")
	memberImageVersion = os.Getenv("MEMBER_AGENT_IMAGE_VERSION")
	hubChartsDirectory = "charts/hub-agent"
	testEnv            env.Environment
	deployNamespace    = "fleet-system"
)

func TestMain(m *testing.M) {
	testEnv = env.NewWithConfig(envconf.New())
	// Create kind clusters
	testEnv.Setup(
		envfuncs.CreateKindClusterWithConfig(kindHubName, "kindest/node:v1.23.5", "kind-config.yaml"),
		envfuncs.LoadDockerImageToCluster(kindHubName, fmt.Sprintf("%s/%s:%s", registry, hubImageName, hubImageVersion)),
		deployHubChart(deployNamespace),
		envfuncs.CreateKindClusterWithConfig(kindMemberName, "kindest/node:v1.23.5", "kind-config.yaml"),
		envfuncs.LoadDockerImageToCluster(kindMemberName, fmt.Sprintf("%s/%s:%s", registry, memberImageName, memberImageVersion)),
	).Finish( // Cleanup KinD Cluster
		envfuncs.DestroyKindCluster(kindHubName),
		envfuncs.DestroyKindCluster(kindMemberName),
	)

	os.Exit(testEnv.Run(m))
}

func deployHubChart(namespace string) env.Func {
	return func(ctx context.Context, cfg *envconf.Config) (context.Context, error) {
		wd, err := os.Getwd()
		if err != nil {
			return ctx, err
		}

		chartsAbsolutePath, err := filepath.Abs(filepath.Join(wd, "/../../", hubChartsDirectory))
		if err != nil {
			return ctx, err
		}

		manager := helm.New(cfg.KubeconfigFile())

		args := fmt.Sprintf("--set image.repository=%s --set image.tag=%s",
			fmt.Sprintf("%s/%s", registry, hubImageName), hubImageVersion)

		if err := manager.RunInstall(helm.WithName("hub-agent"),
			helm.WithChart(chartsAbsolutePath),
			helm.WithArgs(args), helm.WithWait()); err != nil {
			klog.ErrorS(err, "failed to invoke helm install operation due to an error")
			return ctx, err
		}

		deployment := &appsv1.Deployment{
			ObjectMeta: v1.ObjectMeta{
				Name:      "hub-agent",
				Namespace: namespace,
			},
			Spec: appsv1.DeploymentSpec{},
		}

		if err := wait.For(conditions.New(cfg.Client().Resources()).DeploymentConditionMatch(deployment, appsv1.DeploymentAvailable, corev1.ConditionTrue),
			wait.WithTimeout(time.Minute*3)); err != nil {
			klog.ErrorS(err, " Failed to deploy hub agent")
			return ctx, err
		}

		return ctx, nil
	}
}
