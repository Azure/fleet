/*
Copyright 2025 The KubeFleet Authors.

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

package validation

import (
	"fmt"
	"regexp"

	"github.com/google/cel-go/cel"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"go.goms.io/fleet/pkg/utils"
)

const (
	// groupMatchPattern is the regex pattern used to extract the group from a CRD name
	groupMatchPattern = `^[^.]*\.(.*)`
)

// Export constants for use by the webhook handler
const (
	MastersGroup              = "system:masters"
	KubeadmClusterAdminsGroup = "kubeadm:cluster-admins"
)

// FleetCRDGroups defines the list of CRD groups that are part of Fleet
var FleetCRDGroups = []string{"networking.fleet.azure.com", "fleet.azure.com", "multicluster.x-k8s.io", "cluster.kubernetes-fleet.io", "placement.kubernetes-fleet.io"}

// CELEnvironment holds the initialized CEL environment and compiled expressions
type CELEnvironment struct {
	env                 *cel.Env
	checkCRDGroupExpr   cel.Program
	checkUserAccessExpr cel.Program
}

// NewCELEnvironment creates and initializes a new CEL environment for the fleet validations
func NewCELEnvironment(fleetCRDGroups []string, adminGroups []string) (*CELEnvironment, error) {
	env, err := cel.NewEnv(
		cel.Variable("group", cel.StringType),
		cel.Variable("fleetCRDGroups", cel.ListType(cel.StringType)),
		cel.Variable("username", cel.StringType),
		cel.Variable("userGroups", cel.ListType(cel.StringType)),
		cel.Variable("whiteListedUsers", cel.ListType(cel.StringType)),
		cel.Variable("adminGroups", cel.ListType(cel.StringType)),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create CEL environment: %w", err)
	}

	// Compile the expression to check CRD group
	checkCRDGroupAst, issues := env.Compile("group in fleetCRDGroups")
	if issues != nil && issues.Err() != nil {
		return nil, fmt.Errorf("failed to compile CRD group check expression: %w", issues.Err())
	}

	checkCRDGroupExpr, err := env.Program(checkCRDGroupAst)
	if err != nil {
		return nil, fmt.Errorf("failed to create CRD group check program: %w", err)
	}

	// Compile the expression to check user access
	checkUserAccessAst, issues := env.Compile("username in whiteListedUsers || userGroups.exists(g, g in adminGroups)")
	if issues != nil && issues.Err() != nil {
		return nil, fmt.Errorf("failed to compile user access check expression: %w", issues.Err())
	}

	checkUserAccessExpr, err := env.Program(checkUserAccessAst)
	if err != nil {
		return nil, fmt.Errorf("failed to create user access check program: %w", err)
	}

	return &CELEnvironment{
		env:                 env,
		checkCRDGroupExpr:   checkCRDGroupExpr,
		checkUserAccessExpr: checkUserAccessExpr,
	}, nil
}

// ValidateUserForFleetCRDWithCEL checks to see if user is not allowed to modify fleet CRDs using CEL expressions
func ValidateUserForFleetCRDWithCEL(celEnv *CELEnvironment, req admission.Request, whiteListedUsers []string, group string) admission.Response {
	namespacedName := types.NamespacedName{Name: req.Name, Namespace: req.Namespace}
	userInfo := req.UserInfo

	// First, check if this is a fleet CRD group
	val, _, err := celEnv.checkCRDGroupExpr.Eval(map[string]interface{}{
		"group":          group,
		"fleetCRDGroups": fleetCRDGroups,
	})
	if err != nil {
		klog.ErrorS(err, "Error evaluating CEL expression for checkCRDGroup",
			"user", userInfo.Username, "groups", userInfo.Groups, "group", group)
		// Fall back to the non-CEL implementation if there's an error
		return ValidateUserForFleetCRD(req, whiteListedUsers, group)
	}

	isFleetCRDGroup, ok := val.Value().(bool)
	if !ok {
		klog.ErrorS(fmt.Errorf("unexpected result type"), "CEL expression didn't return a boolean",
			"user", userInfo.Username, "groups", userInfo.Groups, "group", group)
		// Fall back to the non-CEL implementation if there's a type error
		return ValidateUserForFleetCRD(req, whiteListedUsers, group)
	}

	// If it's not a fleet CRD group, allow the request
	if !isFleetCRDGroup {
		klog.V(3).InfoS(allowedModifyResource, "user", userInfo.Username, "groups", userInfo.Groups, "operation", req.Operation, "GVK", req.RequestKind, "subResource", req.SubResource, "namespacedName", namespacedName)
		return admission.Allowed(fmt.Sprintf(ResourceAllowedFormat, userInfo.Username, utils.GenerateGroupString(userInfo.Groups), req.Operation, req.RequestKind, req.SubResource, namespacedName))
	}

	// Check if the user is allowed to modify this CRD
	adminGroups := []string{mastersGroup, kubeadmClusterAdminsGroup}
	val, _, err = celEnv.checkUserAccessExpr.Eval(map[string]interface{}{
		"username":         userInfo.Username,
		"userGroups":       userInfo.Groups,
		"whiteListedUsers": whiteListedUsers,
		"adminGroups":      adminGroups,
	})
	if err != nil {
		klog.ErrorS(err, "Error evaluating CEL expression for user access check",
			"user", userInfo.Username, "groups", userInfo.Groups)
		// Fall back to the non-CEL implementation if there's an error
		if isFleetCRDGroup && !isAdminGroupUserOrWhiteListedUser(whiteListedUsers, userInfo) {
			klog.V(2).InfoS(deniedModifyResource, "user", userInfo.Username, "groups", userInfo.Groups, "operation", req.Operation, "GVK", req.RequestKind, "subResource", req.SubResource, "namespacedName", namespacedName)
			return admission.Denied(fmt.Sprintf(ResourceDeniedFormat, userInfo.Username, utils.GenerateGroupString(userInfo.Groups), req.Operation, req.RequestKind, req.SubResource, namespacedName))
		}
	} else {
		isAllowed, ok := val.Value().(bool)
		if !ok {
			klog.ErrorS(fmt.Errorf("unexpected result type"), "CEL expression didn't return a boolean",
				"user", userInfo.Username, "groups", userInfo.Groups)
			// Fall back to the non-CEL implementation if there's a type error
			if isFleetCRDGroup && !isAdminGroupUserOrWhiteListedUser(whiteListedUsers, userInfo) {
				klog.V(2).InfoS(deniedModifyResource, "user", userInfo.Username, "groups", userInfo.Groups, "operation", req.Operation, "GVK", req.RequestKind, "subResource", req.SubResource, "namespacedName", namespacedName)
				return admission.Denied(fmt.Sprintf(ResourceDeniedFormat, userInfo.Username, utils.GenerateGroupString(userInfo.Groups), req.Operation, req.RequestKind, req.SubResource, namespacedName))
			}
		} else if !isAllowed {
			klog.V(2).InfoS(deniedModifyResource, "user", userInfo.Username, "groups", userInfo.Groups, "operation", req.Operation, "GVK", req.RequestKind, "subResource", req.SubResource, "namespacedName", namespacedName)
			return admission.Denied(fmt.Sprintf(ResourceDeniedFormat, userInfo.Username, utils.GenerateGroupString(userInfo.Groups), req.Operation, req.RequestKind, req.SubResource, namespacedName))
		}
	}

	klog.V(3).InfoS(allowedModifyResource, "user", userInfo.Username, "groups", userInfo.Groups, "operation", req.Operation, "GVK", req.RequestKind, "subResource", req.SubResource, "namespacedName", namespacedName)
	return admission.Allowed(fmt.Sprintf(ResourceAllowedFormat, userInfo.Username, utils.GenerateGroupString(userInfo.Groups), req.Operation, req.RequestKind, req.SubResource, namespacedName))
}

// ExtractCRDGroupWithCEL uses regexp to extract the CRD group from the CRD name
func ExtractCRDGroupWithCEL(name string) string {
	var group string
	match := regexp.MustCompile(groupMatchPattern).FindStringSubmatch(name)
	if len(match) > 1 {
		group = match[1]
	}
	return group
}
