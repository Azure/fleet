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
	"sigs.k8s.io/e2e-framework/pkg/envconf"
	"sigs.k8s.io/e2e-framework/third_party/helm"

	"sigs.k8s.io/e2e-framework/pkg/features"
)

var (
	memberChartsDirectory = "charts/member-agent"
)

func TestJoinSucessScenairo(t *testing.T) {
	successFlow := features.New("Test join success flow").
		Setup(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			ctx, err := deployMemberChart(ctx, deployNamespace, *cfg)
			if err != nil {
				return ctx
			}
			return ctx
		}).Assess(" Internal member cluster Created", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
		if err := KubectlUseContext(cfg.KubeconfigFile(), kindHubName); err != nil {
			klog.Errorf("Error while switching cluster context", err)
			return ctx
		}
		if err := deployCustomResource(cfg.KubeconfigFile(), "default", "examples", "fleet_v1alpha1_membercluster.yaml"); err != nil {
			t.Error("Failed to deploy member cluster resource", err)
		}
		return ctx
		//TODO the join test flow not completed
	}).Feature()

	testEnv.Test(t, successFlow)
}

func deployMemberChart(ctx context.Context, namespace string, cfg envconf.Config) (context.Context, error) {
	wd, err := os.Getwd()
	if err != nil {
		return ctx, err
	}
	chartsAbsolutePath, err := filepath.Abs(filepath.Join(wd, "/../../", memberChartsDirectory))
	if err != nil {
		return ctx, err
	}

	manager := helm.New(cfg.KubeconfigFile())

	args := fmt.Sprintf("--set image.repository=%s --set image.tag=%s",
		fmt.Sprintf("%s/%s", registry, memberImageName), memberImageVersion)

	if err := manager.RunInstall(helm.WithName("member-agent"),
		helm.WithChart(chartsAbsolutePath),
		helm.WithArgs(args),
		helm.WithWait()); err != nil {
		klog.ErrorS(err, "failed to invoke helm install operation due to an error")
		return ctx, err
	}

	memberAgent := &appsv1.Deployment{
		ObjectMeta: v1.ObjectMeta{
			Name:      "member-agent",
			Namespace: cfg.KubeconfigFile(),
		},
		Spec: appsv1.DeploymentSpec{},
	}

	if err := wait.For(conditions.New(cfg.Client().Resources()).DeploymentConditionMatch(memberAgent, appsv1.DeploymentAvailable, corev1.ConditionTrue),
		wait.WithTimeout(time.Minute*1)); err != nil {
		klog.ErrorS(err, " Failed to deploy member agent")
		return ctx, err
	}
	return ctx, nil
}

// deploy placement policy config
func deployCustomResource(kubeConfig, namespace, resourcePath, fileName string) error {
	wd, err := os.Getwd()
	if err != nil {
		return err
	}

	exampleResourceAbsolutePath, err := filepath.Abs(filepath.Join(wd, "/../../", resourcePath))
	if err != nil {
		return err
	}
	return KubectlApply(kubeConfig, namespace, []string{"-f", filepath.Join(exampleResourceAbsolutePath, fileName)})
}
