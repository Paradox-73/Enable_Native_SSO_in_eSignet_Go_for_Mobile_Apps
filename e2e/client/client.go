// SPDX-License-Identifier: MIT
// Copyright (c) 2026 Team Badmosip

package client

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/badmosip/esignet-native-sso/e2e/internal/uuid"
)

// Discovery is the subset of the OpenID Provider metadata the client uses.
type Discovery struct {
	Issuer                string   `json:"issuer"`
	AuthorizationEndpoint string   `json:"authorization_endpoint"`
	TokenEndpoint         string   `json:"token_endpoint"`
	UserinfoEndpoint      string   `json:"userinfo_endpoint"`
	JWKSURI               string   `json:"jwks_uri"`
	PAREndpoint           string   `json:"pushed_authorization_request_endpoint"`
	DPoPAlgs              []string `json:"dpop_signing_alg_values_supported"`
	ScopesSupported       []string `json:"scopes_supported"`
	GrantTypesSupported   []string `json:"grant_types_supported"`
	NativeSSOSupported    bool     `json:"native_sso_supported"`
	RevocationEndpoint    string   `json:"revocation_endpoint"`
}

// Client talks to one eSignet issuer.
type Client struct {
	Issuer string
	HTTP   *http.Client
	Disc   Discovery
	// Logf, when set, receives one line per request (method, URL, status).
	Logf func(format string, args ...any)
}

// New returns a client for issuer. dialOverride maps host:port pairs to other addresses
// ("localhost:38088=esignet:8080,..."), so a client inside the compose network can use URLs
// that name the public issuer without changing the issuer, htu or aud values.
// Redirects are never followed: the authorize step reads the Location header itself.
func New(issuer, dialOverride string) (*Client, error) {
	over := map[string]string{}
	for _, pair := range strings.Split(dialOverride, ",") {
		if from, to, ok := strings.Cut(strings.TrimSpace(pair), "="); ok {
			over[from] = to
		}
	}
	d := &net.Dialer{Timeout: 10 * time.Second}
	tr := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			if to, ok := over[addr]; ok {
				addr = to
			}
			return d.DialContext(ctx, network, addr)
		},
		Proxy: nil,
	}
	c := &Client{
		Issuer: strings.TrimRight(issuer, "/"),
		HTTP: &http.Client{
			Transport:     tr,
			Timeout:       30 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}
	return c, nil
}

func (c *Client) logf(format string, args ...any) {
	if c.Logf != nil {
		c.Logf(format, args...)
	}
}

// Response is a finished HTTP exchange.
type Response struct {
	Status int
	Header http.Header
	Body   []byte
}

// JSON decodes the body into v.
func (r *Response) JSON(v any) error {
	if err := json.Unmarshal(r.Body, v); err != nil {
		return fmt.Errorf("HTTP %d, body is not JSON (%w): %.300s", r.Status, err, r.Body)
	}
	return nil
}

func (c *Client) do(method, rawURL string, header http.Header, body []byte) (*Response, error) {
	req, err := http.NewRequest(method, rawURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	for k, vs := range header {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w", method, rawURL, err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	c.logf("%s %s -> %d", method, rawURL, resp.StatusCode)
	return &Response{Status: resp.StatusCode, Header: resp.Header, Body: b}, nil
}

// Get sends a GET with optional headers.
func (c *Client) Get(rawURL string, header http.Header) (*Response, error) {
	return c.do(http.MethodGet, rawURL, header, nil)
}

// PostForm sends an application/x-www-form-urlencoded POST.
func (c *Client) PostForm(rawURL string, form url.Values, header http.Header) (*Response, error) {
	h := header.Clone()
	if h == nil {
		h = http.Header{}
	}
	h.Set("Content-Type", "application/x-www-form-urlencoded")
	return c.do(http.MethodPost, rawURL, h, []byte(form.Encode()))
}

// PostJSON sends a JSON POST.
func (c *Client) PostJSON(rawURL string, v any, header http.Header) (*Response, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	h := header.Clone()
	if h == nil {
		h = http.Header{}
	}
	h.Set("Content-Type", "application/json")
	return c.do(http.MethodPost, rawURL, h, b)
}

// Discover loads /.well-known/openid-configuration and checks that the issuer matches.
func (c *Client) Discover() error {
	r, err := c.Get(c.Issuer+"/.well-known/openid-configuration", nil)
	if err != nil {
		return err
	}
	if r.Status != http.StatusOK {
		return fmt.Errorf("discovery: HTTP %d: %.300s", r.Status, r.Body)
	}
	if err := r.JSON(&c.Disc); err != nil {
		return err
	}
	if c.Disc.Issuer != c.Issuer {
		return fmt.Errorf("discovery issuer %q, want %q", c.Disc.Issuer, c.Issuer)
	}
	return nil
}

// JWKS returns the issuer's signing keys.
func (c *Client) JWKS() ([]map[string]any, error) {
	r, err := c.Get(c.Disc.JWKSURI, nil)
	if err != nil {
		return nil, err
	}
	var set struct {
		Keys []map[string]any `json:"keys"`
	}
	if err := r.JSON(&set); err != nil {
		return nil, err
	}
	return set.Keys, nil
}

// DPoPProof builds an RFC 9449 proof for one request. accessToken is empty except for
// resource requests, where the proof carries its hash (ath).
func DPoPProof(key *ECKey, method, htu, accessToken, nonce string) (string, error) {
	claims := map[string]any{
		"jti": uuid.New(),
		"htm": method,
		"htu": htu,
		"iat": time.Now().Unix(),
	}
	if accessToken != "" {
		sum := sha256.Sum256([]byte(accessToken))
		claims["ath"] = B64(sum[:])
	}
	if nonce != "" {
		claims["nonce"] = nonce
	}
	jwk := map[string]any{}
	for k, v := range key.PublicJWK() {
		jwk[k] = v
	}
	return key.SignJWT(map[string]any{"typ": "dpop+jwt", "jwk": jwk}, claims)
}

// ClientAssertion builds a private_key_jwt (RFC 7523) for clientID with audience aud. FAPI 2.0
// (Security Profile 5.3.2.1) and Thunder require aud to be the issuer itself, for every endpoint.
func ClientAssertion(key *ECKey, clientID, aud string) (string, error) {
	now := time.Now().Unix()
	return key.SignJWT(
		map[string]any{"typ": "JWT", "kid": key.Kid},
		map[string]any{"iss": clientID, "sub": clientID, "aud": aud, "jti": uuid.New(), "iat": now, "exp": now + 60},
	)
}
