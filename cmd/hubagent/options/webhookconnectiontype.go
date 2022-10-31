/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package options

import (
	"errors"
	"strings"
)

type WebhookClientConnectionType string

const (
	URL     WebhookClientConnectionType = "url"
	Service WebhookClientConnectionType = "service"
)

var (
	capabilitiesMap = map[string]WebhookClientConnectionType{
		"service": Service,
		"url":     URL,
	}
)

func parseWebhookClientConnectionString(str string) (WebhookClientConnectionType, error) {
	t, ok := capabilitiesMap[strings.ToLower(str)]
	if !ok {
		return "", errors.New("must be \"service\" or \"url\"")
	}
	return t, nil
}
