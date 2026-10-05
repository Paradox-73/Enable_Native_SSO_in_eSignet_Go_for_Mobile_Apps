// SPDX-License-Identifier: MIT
// Copyright (c) 2026 Team Badmosip

package client

import (
	"crypto/ecdsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// Values of OpenID Connect Native SSO for Mobile Apps 1.0 and RFC 8693.
const (
	GrantTypeTokenExchange   = "urn:ietf:params:oauth:grant-type:token-exchange"
	TokenTypeIDToken         = "urn:ietf:params:oauth:token-type:id_token"
	TokenTypeAccessToken     = "urn:ietf:params:oauth:token-type:access_token"
	TokenTypeDeviceSecret    = "urn:openid:params:token-type:device-secret"
	TokenTypeOldDeviceSecret = "urn:x-oath:params:oauth:token-type:device-secret" // draft-06 URN, refused
	ScopeDeviceSSO           = "device_sso"
)

// ExchangeRequest is one token-exchange request (RFC 8693) at the token endpoint.
type ExchangeRequest struct {
	Reg              Registration // the requesting client; authenticates with private_key_jwt
	SubjectToken     string
	SubjectTokenType string // default TokenTypeIDToken
	ActorToken       string
	ActorTokenType   string // default TokenTypeDeviceSecret; "-" sends none
	Audience         string // default the issuer; "-" sends none
	Scope            string // empty sends none
	DPoPKey          *ECKey // nil sends no proof
	DPoPProof        string // a ready proof, sent as it is (replay tests); overrides DPoPKey
}

// TokenExchange sends req and returns the answer as it is. A DPoP-Nonce challenge is answered
// once, unless a ready proof was given.
func (c *Client) TokenExchange(req ExchangeRequest) (*Response, error) {
	var lastNonce string
	for attempt := 0; ; attempt++ {
		assertion, err := ClientAssertion(req.Reg.Key, req.Reg.ClientID, c.Issuer)
		if err != nil {
			return nil, err
		}
		form := url.Values{
			"grant_type":            {GrantTypeTokenExchange},
			"client_id":             {req.Reg.ClientID},
			"client_assertion_type": {"urn:ietf:params:oauth:client-assertion-type:jwt-bearer"},
			"client_assertion":      {assertion},
			"subject_token":         {req.SubjectToken},
			"subject_token_type":    {orDefault(req.SubjectTokenType, TokenTypeIDToken)},
		}
		if req.ActorToken != "" {
			form.Set("actor_token", req.ActorToken)
		}
		if t := orDefault(req.ActorTokenType, TokenTypeDeviceSecret); t != "-" {
			form.Set("actor_token_type", t)
		}
		if a := orDefault(req.Audience, c.Issuer); a != "-" {
			form.Set("audience", a)
		}
		if req.Scope != "" {
			form.Set("scope", req.Scope)
		}
		header := http.Header{}
		switch {
		case req.DPoPProof != "":
			header.Set("DPoP", req.DPoPProof)
		case req.DPoPKey != nil:
			proof, err := DPoPProof(req.DPoPKey, http.MethodPost, c.Disc.TokenEndpoint, "", lastNonce)
			if err != nil {
				return nil, err
			}
			header.Set("DPoP", proof)
		}
		r, err := c.PostForm(c.Disc.TokenEndpoint, form, header)
		if err != nil {
			return nil, err
		}
		if n := r.Header.Get("DPoP-Nonce"); n != "" && r.Status == http.StatusBadRequest && attempt == 0 &&
			req.DPoPKey != nil && req.DPoPProof == "" {
			lastNonce = n
			continue
		}
		return r, nil
	}
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

// OAuthError is the error member of an OAuth 2.0 error answer (RFC 6749 5.2).
type OAuthError struct {
	Error       string `json:"error"`
	Description string `json:"error_description"`
}

// ErrorOf decodes an OAuth error answer; both fields are empty when the body is not one.
func ErrorOf(r *Response) OAuthError {
	var e OAuthError
	_ = json.Unmarshal(r.Body, &e)
	return e
}

// ExchangeResponse is a successful token-exchange answer.
type ExchangeResponse struct {
	AccessToken     string `json:"access_token"`
	IssuedTokenType string `json:"issued_token_type"`
	TokenType       string `json:"token_type"`
	ExpiresIn       int    `json:"expires_in"`
	Scope           string `json:"scope"`
	IDToken         string `json:"id_token"`
	RefreshToken    string `json:"refresh_token"`
	DeviceSecret    string `json:"device_secret"`
}

// JWEHeader decodes the protected header of a compact JWE (five parts).
func JWEHeader(raw string) (map[string]any, error) {
	parts := strings.Split(raw, ".")
	if len(parts) != 5 {
		return nil, fmt.Errorf("not a compact JWE: %d parts", len(parts))
	}
	b, err := b64.DecodeString(parts[0])
	if err != nil {
		return nil, err
	}
	var h map[string]any
	return h, json.Unmarshal(b, &h)
}

// DSHash is the ds_hash of a device secret: the left half of its SHA-256, base64url (Native SSO
// 3.4, built like at_hash).
func DSHash(deviceSecret string) string {
	sum := sha256.Sum256([]byte(deviceSecret))
	return B64(sum[:len(sum)/2])
}

// MarshalPrivate encodes the key as base64url PKCS #8, for handing a test session from one test
// phase to the next. Test keys only.
func (k *ECKey) MarshalPrivate() (string, error) {
	der, err := x509.MarshalPKCS8PrivateKey(k.Private)
	if err != nil {
		return "", err
	}
	return B64(der), nil
}

// ParseECKey reverses MarshalPrivate.
func ParseECKey(s string) (*ECKey, error) {
	der, err := b64.DecodeString(s)
	if err != nil {
		return nil, err
	}
	pk, err := x509.ParsePKCS8PrivateKey(der)
	if err != nil {
		return nil, err
	}
	ec, ok := pk.(*ecdsa.PrivateKey)
	if !ok {
		return nil, errors.New("not an EC key")
	}
	key := &ECKey{Private: ec}
	key.Kid = key.Thumbprint()
	return key, nil
}
