/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/
package authtoken

import (
	"io"
	"os"

	"github.com/pkg/errors"
	"k8s.io/klog"

	"go.goms.io/fleet/pkg/interfaces"
)

type Factory struct {
	filePath string
}

func NewFactory(filePath string) Factory {
	return Factory{filePath: filePath}
}

func (w Factory) Create() (io.WriteCloser, error) {
	wc, err := os.Create(w.filePath)
	if err != nil {
		return nil, err
	}
	return wc, nil
}

type Writer struct {
	writerFactory func() (io.WriteCloser, error)
}

func NewWriter(factory func() (io.WriteCloser, error)) interfaces.AuthTokenWriter {
	return &Writer{
		writerFactory: factory,
	}
}

func (w *Writer) WriteToken(token interfaces.AuthToken) error {
	writer, err := w.writerFactory()
	if err != nil {
		return err
	}
	defer func() {
		err := writer.Close()
		if err != nil {
			klog.Error(err, "cannot close the token file")
		}
	}()
	_, err = io.WriteString(writer, token.Token)
	if err != nil {
		return errors.Wrap(err, "cannot write the refresh token")
	}
	klog.Info("token has been saved to the file successfully")
	return nil
}
