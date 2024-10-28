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
)

type BufferWriterFactory struct {
	buf        *strings.Builder
	writeCount int
}

func NewBufferWriterFactory() *BufferWriterFactory {
	return &BufferWriterFactory{new(strings.Builder), 0}
}

func (f *BufferWriterFactory) Create() (io.WriteCloser, error) {
	return BufferWriter{f}, nil
}

type BufferWriter struct {
	factory *BufferWriterFactory
}

func (c BufferWriter) Write(p []byte) (int, error) {
	c.factory.writeCount++
	return c.factory.buf.Write(p)
}

func (c BufferWriter) Close() error {
	// no op
	return nil
}

func TestWriteToken(t *testing.T) {
	token := AuthToken{
		Token:     "test token",
		ExpiresOn: time.Now(),
	}

	factory := NewBufferWriterFactory()
	bufferWriter := NewWriter(factory.Create)
	err := bufferWriter.WriteToken(token)

	assert.Equal(t, nil, err, "TestWriteToken")
	assert.Equal(t, token.Token, factory.buf.String(), "TestWriteToken")
}
