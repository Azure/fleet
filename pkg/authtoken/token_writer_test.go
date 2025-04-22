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
