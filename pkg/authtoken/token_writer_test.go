/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/
package authtoken

import (
	"io"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"go.goms.io/fleet/pkg/interfaces"
)

type BufferWriterFactory struct {
	buf *strings.Builder
}

func (b BufferWriterFactory) Create() (io.WriteCloser, error) {
	return CloseableBuilder(b), nil
}

type CloseableBuilder struct {
	buf *strings.Builder
}

func (c CloseableBuilder) Write(p []byte) (n int, err error) {
	return c.buf.Write(p)
}

func (c CloseableBuilder) Close() error {
	// no-op
	return nil
}

func TestWriteToken(t *testing.T) {
	buf := new(strings.Builder)
	token := interfaces.AuthToken{
		Token:     "test token",
		ExpiresOn: time.Now(),
	}

	bufferWriter := NewWriter(BufferWriterFactory{buf}.Create)
	err := bufferWriter.WriteToken(token)

	assert.Equal(t, nil, err, "TestWriteToken")
	assert.Equal(t, token.Token, buf.String(), "TestWriteToken")
}
