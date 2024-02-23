/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

// Package resource defines common utils for working with kubernetes resources.
package resource

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
)

// HashOf returns the hash of the resource.
func HashOf(resource any) (string, error) {
	jsonBytes, err := json.Marshal(resource)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", sha256.Sum256(jsonBytes)), nil
}
