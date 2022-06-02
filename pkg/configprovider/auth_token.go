/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/
package configprovider

import (
	"encoding/json"
	"strconv"
	"time"

	"github.com/Azure/go-autorest/autorest/date"
	"github.com/golang-jwt/jwt"
	"github.com/pkg/errors"
)

type AuthToken struct {
	AccessToken string      `json:"accessToken"`
	ExpiresOn   json.Number `json:"expiresOn"`
}

func NewToken(accessToken string, exp json.Number) *AuthToken {
	return &AuthToken{
		AccessToken: accessToken,
		ExpiresOn:   exp,
	}
}

// Expires returns the time.Time when the Token expires.
func (a *AuthToken) Expires() time.Time {
	s, err := a.ExpiresOn.Float64()
	if err != nil {
		s = -3600
	}

	expiration := date.NewUnixTimeFromSeconds(s)

	return time.Time(expiration).UTC()
}

// WillExpireIn returns true if the Token will expire after the passed time.Duration interval
// from now, false otherwise.
func (a *AuthToken) WillExpireIn(d time.Duration) bool {
	return !a.Expires().After(time.Now().Add(d))
}

func EmptyToken() *AuthToken {
	return &AuthToken{
		ExpiresOn: "0",
	}
}
func getTokenClaims(accessToken string) (jwt.MapClaims, error) {
	p := &jwt.Parser{SkipClaimsValidation: true}

	token, _, err := p.ParseUnverified(accessToken, jwt.MapClaims{})
	if err != nil {
		return nil, errors.Wrap(err, "failed to parse token")
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return nil, errors.New("unexpected claim type from token")
	}

	return claims, nil
}

func GetTokenExpiration(accessToken string) (json.Number, error) {
	claims, err := getTokenClaims(accessToken)
	if err != nil {
		return "", err
	}

	switch exp := claims["exp"].(type) {
	case float64:
		return json.Number(strconv.FormatInt(int64(exp), 10)), nil
	case json.Number:
		return exp, nil
	default:
		return "", errors.New("failed to parse token expiration")
	}
}
