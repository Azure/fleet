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

package writefile

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestCreateSecureFile(t *testing.T) {
	tests := []struct {
		name         string
		oldContent   string
		writeContent string
	}{
		{
			name:         "creates new file with restricted permissions",
			writeContent: "sensitive-data",
		},
		{
			name:         "truncates longer existing content",
			oldContent:   "this-is-a-very-long-existing-content-that-should-be-truncated",
			writeContent: "short",
		},
		{
			name:         "overwrites shorter existing content",
			oldContent:   "short",
			writeContent: "a-much-longer-replacement-content",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			filePath := filepath.Join(dir, "secure-file")

			writeAndClose := func(content string) {
				f, err := CreateSecureFile(filePath)
				if err != nil {
					t.Fatalf("CreateSecureFile(%q) returned error: %v", filePath, err)
				}
				if _, err := io.WriteString(f, content); err != nil {
					t.Fatalf("io.WriteString() returned error: %v", err)
				}
				f.Close()
			}

			if tc.oldContent != "" {
				writeAndClose(tc.oldContent)
			}

			writeAndClose(tc.writeContent)

			got, err := os.ReadFile(filePath)
			if err != nil {
				t.Fatalf("os.ReadFile(%q) returned error: %v", filePath, err)
			}
			if diff := cmp.Diff(tc.writeContent, string(got)); diff != "" {
				t.Errorf("CreateSecureFile() file content mismatch (-want +got):\n%s", diff)
			}

			info, err := os.Stat(filePath)
			if err != nil {
				t.Fatalf("os.Stat(%q) returned error: %v", filePath, err)
			}
			if gotPerm := info.Mode().Perm(); gotPerm != 0600 {
				t.Errorf("CreateSecureFile() file permission = %o, want %o", gotPerm, 0600)
			}
		})
	}
}
