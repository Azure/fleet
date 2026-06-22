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
	"os"
	"path/filepath"
)

// CreateSecureFile creates or truncates a file with owner-only permissions (0600).
// The path is sanitized with filepath.Clean to resolve any relative or redundant elements.
//
//   - O_WRONLY: write-only, since callers only need to write sensitive data.
//   - O_CREATE: creates the file if it does not exist.
//   - O_TRUNC: empties the file before writing so that no leftover bytes from a
//     previous (possibly longer) write remain in the file.
//   - 0600: owner read/write only, preventing other users from accessing the file.
func CreateSecureFile(path string) (*os.File, error) {
	return os.OpenFile(filepath.Clean(path), os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
}
