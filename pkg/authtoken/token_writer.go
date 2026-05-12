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
package authtoken

import (
	"fmt"
	"io"

	"k8s.io/klog/v2"

	"go.goms.io/fleet/pkg/utils/writefile"
)

type Factory struct {
	filePath string
}

func NewFactory(filePath string) Factory {
	return Factory{filePath: filePath}
}

// Create opens the token file for writing using CreateSecureFile.
// The member-agent container runs as the same UID and reads this file via client-go's
// BearerTokenFile (O_RDONLY), so owner permission is sufficient for both writer and reader.
func (w Factory) Create() (io.WriteCloser, error) {
	wc, err := writefile.CreateSecureFile(w.filePath)
	if err != nil {
		return nil, err
	}
	return wc, nil
}

type TokenWriter struct {
	writerFactory func() (io.WriteCloser, error)
}

func NewWriter(factory func() (io.WriteCloser, error)) Writer {
	return &TokenWriter{
		writerFactory: factory,
	}
}

func (w *TokenWriter) WriteToken(token AuthToken) error {
	writer, err := w.writerFactory()
	if err != nil {
		return err
	}
	defer func() {
		err := writer.Close()
		if err != nil {
			klog.ErrorS(err, "cannot close the token file")
		}
	}()
	_, err = io.WriteString(writer, token.Token)
	if err != nil {
		return fmt.Errorf("cannot write the refresh token: %w", err)
	}
	klog.V(2).InfoS("token has been saved to the file successfully")
	return nil
}
