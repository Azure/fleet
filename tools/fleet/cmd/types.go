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

package cmd

import (
	"fmt"
	"strings"
)

// Canonical kind names for approval requests.
const (
	KindClusterApprovalRequest = "clusterapprovalrequest"
	KindApprovalRequest        = "approvalrequest"
)

// Aliases for approval request kinds.
const (
	AliasClusterApprovalRequest = "careq"
	AliasApprovalRequest        = "areq"
)

// KindConfig defines the configuration for a resource kind.
// This allows commands to handle multiple resource kinds with different
// validation rules and handlers without using switch statements.
type KindConfig struct {
	// Canonical is the canonical name of the kind (e.g., "clusterapprovalrequest").
	Canonical string
	// Aliases are short aliases for the kind (e.g., "careq").
	Aliases []string
}

// ResolveKind resolves a kind string (canonical or alias) to its value from the provided map.
// The lookup is case-insensitive.
func ResolveKind[T any](kind string, kinds map[string]*T) (*T, error) {
	if kind == "" {
		return nil, fmt.Errorf("resource kind is required")
	}
	lower := strings.ToLower(kind)
	cfg, ok := kinds[lower]
	if !ok {
		return nil, fmt.Errorf("unsupported resource kind %q", kind)
	}
	return cfg, nil
}
