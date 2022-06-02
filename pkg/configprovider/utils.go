/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/
package configprovider

import (
	"os"

	"github.com/pkg/errors"
)

func WriteTokenToFile(tokenFile string, byteToken []byte) error {
	err := os.WriteFile(tokenFile, byteToken, os.ModePerm)
	if err != nil {
		return errors.Wrap(err, "cannot write the refresh token into the file")
	}
	return nil
}
