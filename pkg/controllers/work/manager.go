/*
Copyright 2021 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	"context"
	"os"

	"github.com/go-logr/logr"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

const (
	workFinalizer = "fleet.azure.com/work-cleanup"

	configMapNameAnnotation = "fleet.azure.com/config-map-name"

	manifestHashAnnotation = "fleet.azure.com/spec-hash"
	manifestHashKey        = "fleet-spec-hash"

	lastAppliedConfigAnnotation = "fleet.azure.com/last-applied-configuration"
	lastAppliedConfigKey        = "fleet-last-applied-configuration"

	ConditionTypeApplied   = "Applied"
	ConditionTypeAvailable = "Available"

	// number of concurrent reconcile loop for work
	maxWorkConcurrency = 5
)

// CreateControllers create the controllers with the supplied config
func CreateControllers(ctx context.Context, hubCfg, spokeCfg *rest.Config, setupLog logr.Logger, opts ctrl.Options) (manager.Manager, *ApplyWorkReconciler, error) {
	hubMgr, err := ctrl.NewManager(hubCfg, opts)
	if err != nil {
		setupLog.Error(err, "unable to create hub manager")
		os.Exit(1)
	}

	spokeDynamicClient, err := dynamic.NewForConfig(spokeCfg)
	if err != nil {
		setupLog.Error(err, "unable to create spoke dynamic client")
		os.Exit(1)
	}

	restMapper, err := apiutil.NewDynamicRESTMapper(spokeCfg, apiutil.WithLazyDiscovery)
	if err != nil {
		setupLog.Error(err, "unable to create spoke rest mapper")
		os.Exit(1)
	}

	spokeClient, err := client.New(spokeCfg, client.Options{
		Scheme: opts.Scheme, Mapper: restMapper,
	})
	if err != nil {
		setupLog.Error(err, "unable to create spoke client")
		os.Exit(1)
	}

	workController := NewApplyWorkReconciler(
		hubMgr.GetClient(),
		spokeDynamicClient,
		spokeClient,
		restMapper,
		hubMgr.GetEventRecorderFor("work_controller"),
		maxWorkConcurrency,
		opts.Namespace,
	)

	if err = workController.SetupWithManager(hubMgr); err != nil {
		setupLog.Error(err, "unable to create the controller", "controller", "Work")
		return nil, nil, err
	}

	if err = workController.Join(ctx); err != nil {
		setupLog.Error(err, "unable to mark the controller joined", "controller", "Work")
		return nil, nil, err
	}

	return hubMgr, workController, nil
}
