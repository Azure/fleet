/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package options

import "strings"

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

func parseWebhookClientConnectionString(str string) (WebhookClientConnectionType, bool) {
	t, ok := capabilitiesMap[strings.ToLower(str)]
	return t, ok
}
