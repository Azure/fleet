/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package options

type WebhookClientConnectionType string

const (
	URL     WebhookClientConnectionType = "url"
	Service WebhookClientConnectionType = "service"
)
