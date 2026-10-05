/*
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */

package httpmiddleware

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/thunder-id/thunderid/pkg/thunderidengine/providers"

	applog "github.com/mosip/esignet/internal/log"
)

// UserinfoPath is the engine's userinfo endpoint (OpenID Connect Core 5.3).
const UserinfoPath = "/oauth2/userinfo"

// userinfoRegisteredClaims are the JWT claims of the identity system's userinfo token that are
// not user attributes. They are kept whatever the access token's scope.
var userinfoRegisteredClaims = map[string]bool{
	"iss": true, "sub": true, "aud": true, "exp": true, "iat": true, "nbf": true, "jti": true,
}

// UserinfoSigner signs the userinfo token again after claims were removed from it.
// providers.RuntimeCryptoProvider implements it; the engine signs its own tokens with the same
// provider, so the new token verifies against the keys in /oauth2/jwks.
type UserinfoSigner interface {
	GetPublicKeys(ctx context.Context, filter providers.PublicKeyFilter) ([]providers.PublicKeyInfo, error)
	Sign(ctx context.Context, keyRef providers.KeyRef, alg string, content []byte) ([]byte, error)
}

// UserinfoScopeConfig configures UserinfoScope.
type UserinfoScopeConfig struct {
	// ScopeClaims maps each OpenID Connect scope to the claims it releases (scope_claims).
	ScopeClaims map[string][]string
	// SigningKeyID is the reference id of the key the engine signs its tokens with
	// (jwt.preferred_key_id); empty means the crypto provider's default key.
	SigningKeyID string
	// Signer signs the filtered token; nil turns the filter off.
	Signer UserinfoSigner
}

// UserinfoScope limits the userinfo answer for access tokens issued by the token-exchange grant
// (OpenID Connect Native SSO for Mobile Apps 1.0, 4.4) to the claims of the scopes granted to
// that token.
//
// The identity system returns the user's attributes as one signed token (the attribute
// providers.RawJWTAttributeKey), cached at the login of the first app. The engine passes that
// token through unchanged, so without this filter a second app that was granted a narrower
// scope would read every claim the first app's login released. For a token-exchange access
// token this middleware decodes the identity system's token, keeps only the claims of the
// token's scopes (scope_claims) and the registered JWT claims, sets aud to the requesting client
// and signs the result with the engine's signing key.
//
// Access tokens of other grants are left alone: their userinfo token was built for that very
// login and also carries claims requested with the claims parameter, which no scope names. An
// answer the filter cannot read (an encrypted token) is refused with server_error rather than
// passed on unfiltered.
func UserinfoScope(cfg UserinfoScopeConfig) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != UserinfoPath || (r.Method != http.MethodGet && r.Method != http.MethodPost) {
				next.ServeHTTP(w, r)
				return
			}
			buf := newBufferedResponse()
			next.ServeHTTP(buf, r)
			if buf.status != http.StatusOK || cfg.Signer == nil {
				buf.copyTo(w, buf.body.Bytes())
				return
			}

			token, ok := accessTokenClaimsFromHeader(r.Header.Get("Authorization"))
			if !ok {
				// The engine answered 200, so it accepted the token; one this filter cannot read
				// must not get an unfiltered answer.
				writeUserinfoError(r.Context(), w, "access token claims are unreadable")
				return
			}
			if token.GrantType != string(providers.GrantTypeTokenExchange) {
				buf.copyTo(w, buf.body.Bytes())
				return
			}

			body := bytes.TrimSpace(buf.body.Bytes())
			if len(body) > 0 && body[0] == '{' {
				// A JSON answer is built by the engine from individual claims, already limited
				// to the token's scope.
				buf.copyTo(w, buf.body.Bytes())
				return
			}
			filtered, err := cfg.filter(r.Context(), string(body), token)
			if err != nil {
				writeUserinfoError(r.Context(), w, err.Error())
				return
			}
			buf.header.Del("Content-Length")
			buf.copyTo(w, []byte(filtered))
		})
	}
}

// filter rebuilds the identity system's signed userinfo token with only the claims allowed by
// the access token's scopes.
func (cfg UserinfoScopeConfig) filter(ctx context.Context, compact string, token accessTokenClaims) (string, error) {
	parts := strings.Split(compact, ".")
	if len(parts) != 3 {
		return "", fmt.Errorf("userinfo is not a compact JWS (%d parts), so it cannot be limited to the token's scope",
			len(parts))
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", errors.New("userinfo payload is not base64url")
	}
	var claims map[string]any
	if err := json.Unmarshal(payload, &claims); err != nil {
		return "", errors.New("userinfo payload is not a JSON object")
	}

	allowed := map[string]bool{}
	for _, scope := range strings.Fields(token.Scope) {
		for _, claim := range cfg.ScopeClaims[scope] {
			allowed[claim] = true
		}
	}
	for name := range claims {
		if userinfoRegisteredClaims[name] {
			continue
		}
		// Language variants such as name#eng belong to their base claim.
		base, _, _ := strings.Cut(name, "#")
		if !allowed[base] {
			delete(claims, name)
		}
	}
	if _, ok := claims["aud"]; ok && token.ClientID != "" {
		claims["aud"] = token.ClientID
	}
	return cfg.sign(ctx, claims)
}

// sign signs claims the way the engine signs its own tokens: the first key the crypto provider
// returns for the signing key id gives alg and kid (its thumbprint, as in /oauth2/jwks).
func (cfg UserinfoScopeConfig) sign(ctx context.Context, claims map[string]any) (string, error) {
	keys, err := cfg.Signer.GetPublicKeys(ctx, providers.PublicKeyFilter{KeyID: cfg.SigningKeyID})
	if err != nil || len(keys) == 0 {
		return "", errors.New("no userinfo signing key")
	}
	header, err := json.Marshal(map[string]string{"alg": keys[0].Algorithm, "typ": "JWT", "kid": keys[0].Thumbprint})
	if err != nil {
		return "", errors.New("userinfo header cannot be encoded")
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", errors.New("userinfo claims cannot be encoded")
	}
	signingInput := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(payload)
	sig, err := cfg.Signer.Sign(ctx, providers.KeyRef{KeyID: cfg.SigningKeyID}, keys[0].Algorithm, []byte(signingInput))
	if err != nil {
		return "", errors.New("userinfo cannot be signed")
	}
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}

// accessTokenClaims are the access token claims the filter reads.
type accessTokenClaims struct {
	GrantType string `json:"grant_type"`
	Scope     string `json:"scope"`
	ClientID  string `json:"client_id"`
}

// accessTokenClaimsFromHeader reads the claims of the access token in an Authorization header
// of the DPoP or Bearer scheme (the only places the engine's userinfo endpoint takes a token
// from). The signature is not checked here: the filter runs only after the engine accepted the
// token and answered 200.
func accessTokenClaimsFromHeader(authorization string) (accessTokenClaims, bool) {
	scheme, token, ok := strings.Cut(strings.TrimSpace(authorization), " ")
	if !ok || (!strings.EqualFold(scheme, "DPoP") && !strings.EqualFold(scheme, "Bearer")) {
		return accessTokenClaims{}, false
	}
	parts := strings.Split(strings.TrimSpace(token), ".")
	if len(parts) != 3 {
		return accessTokenClaims{}, false
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return accessTokenClaims{}, false
	}
	var claims accessTokenClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return accessTokenClaims{}, false
	}
	return claims, true
}

func writeUserinfoError(ctx context.Context, w http.ResponseWriter, reason string) {
	applog.GetLogger().Error(ctx, "userinfo not limited to the access token's scope", applog.String("reason", reason))
	body, _ := json.Marshal(map[string]string{
		"error":             "server_error",
		"error_description": "userinfo cannot be limited to the access token's scope",
	})
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusInternalServerError)
	_, _ = w.Write(body)
}

// bufferedResponse holds a handler's answer so that it can be changed before it is sent.
type bufferedResponse struct {
	header      http.Header
	status      int
	wroteHeader bool
	body        bytes.Buffer
}

func newBufferedResponse() *bufferedResponse {
	return &bufferedResponse{header: http.Header{}, status: http.StatusOK}
}

func (b *bufferedResponse) Header() http.Header { return b.header }

func (b *bufferedResponse) WriteHeader(code int) {
	if b.wroteHeader {
		return
	}
	b.wroteHeader = true
	b.status = code
}

func (b *bufferedResponse) Write(p []byte) (int, error) {
	b.WriteHeader(http.StatusOK)
	return b.body.Write(p)
}

func (b *bufferedResponse) copyTo(w http.ResponseWriter, body []byte) {
	for k, v := range b.header {
		w.Header()[k] = v
	}
	if w.Header().Get("Content-Length") == "" && len(body) > 0 {
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	}
	w.WriteHeader(b.status)
	_, _ = w.Write(body)
}
