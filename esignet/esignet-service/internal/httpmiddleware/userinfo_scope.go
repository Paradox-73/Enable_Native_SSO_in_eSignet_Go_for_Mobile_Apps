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

	jose "github.com/go-jose/go-jose/v4"
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

// UserinfoSigner signs the userinfo token again with the engine's key.
// providers.RuntimeCryptoProvider implements it; the engine signs its own tokens with the same
// provider, so the new token verifies against the keys in /oauth2/jwks. GetPublicKeys returns the
// engine's signing key first, then the identity system's keys.
type UserinfoSigner interface {
	GetPublicKeys(ctx context.Context, filter providers.PublicKeyFilter) ([]providers.PublicKeyInfo, error)
	Sign(ctx context.Context, keyRef providers.KeyRef, alg string, content []byte) ([]byte, error)
}

// IdentitySystemKeyRefresher is implemented by a UserinfoSigner that can fetch the identity
// system's keys again, bypassing its cache. When the identity system's token names a key that
// GetPublicKeys did not return, the middleware asks once more before refusing the token.
type IdentitySystemKeyRefresher interface {
	RefreshIdentitySystemKeys(ctx context.Context) []providers.PublicKeyInfo
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

// identitySystemAlgorithms are the algorithms accepted on the identity system's userinfo token
// when this middleware checks it before signing it again. The MOSIP mock identity system signs
// with RS256; what reaches the client is always signed with the engine's key.
var identitySystemAlgorithms = []jose.SignatureAlgorithm{jose.RS256, jose.PS256, jose.ES256, jose.EdDSA}

// UserinfoScope makes the signed userinfo answer a token of the engine's own key, and for access
// tokens issued by the token-exchange grant (OpenID Connect Native SSO for Mobile Apps 1.0, 4.4)
// limits it to the claims of the scopes granted to that token.
//
// The identity system returns the user's attributes as one signed token (the attribute
// providers.RawJWTAttributeKey), cached at the login of the first app, and the engine passes
// that token through unchanged. Two problems follow, and this middleware fixes both:
//
//   - The identity system signs with RS256, which FAPI 2.0 does not allow, while /oauth2/jwks
//     publishes its key with alg PS256; a client that requires the token's alg to equal the
//     published alg rejects the token. For every grant, a userinfo token not already signed by
//     the engine's key is checked against the identity system's key (by kid) and signed again
//     with the engine's key, using the alg and kid /oauth2/jwks publishes for that key.
//   - A second app that was granted a narrower scope would read every claim the first app's
//     login released. For a token-exchange access token the claims are limited to the token's
//     scopes (scope_claims) and the registered JWT claims, and aud is set to the requesting
//     client.
//
// The claims of access tokens of other grants are kept: their userinfo token was built for that
// very login and also carries claims requested with the claims parameter, which no scope names.
// An encrypted answer (a JWE for a client that registered an encryption key) is passed on for
// other grants and refused with server_error for a token-exchange access token, as it cannot be
// filtered. Any other answer the middleware cannot check or sign is refused with server_error
// rather than passed on.
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
			exchange := token.GrantType == string(providers.GrantTypeTokenExchange)
			body := bytes.TrimSpace(buf.body.Bytes())
			if len(body) > 0 && body[0] == '{' {
				// A JSON answer is built by the engine from individual claims, already limited
				// to the token's scope.
				buf.copyTo(w, buf.body.Bytes())
				return
			}
			if !exchange && strings.Count(string(body), ".") == 4 {
				// Encrypted for the client; only the client can read it.
				buf.copyTo(w, buf.body.Bytes())
				return
			}
			rebuilt, err := cfg.rebuild(r.Context(), string(body), token, exchange)
			if err != nil {
				writeUserinfoError(r.Context(), w, err.Error())
				return
			}
			if rebuilt == "" {
				buf.copyTo(w, buf.body.Bytes())
				return
			}
			buf.header.Del("Content-Length")
			buf.copyTo(w, []byte(rebuilt))
		})
	}
}

// rebuild returns the userinfo token to send: the identity system's token, checked against its
// key, limited to the access token's scopes when exchange is set, and signed with the engine's
// key. It returns "" when the token is already signed by the engine's key with the published alg
// and needs no filtering (the engine built it itself).
func (cfg UserinfoScopeConfig) rebuild(ctx context.Context, compact string, token accessTokenClaims, exchange bool) (string, error) {
	parts := strings.Split(compact, ".")
	if len(parts) != 3 {
		return "", fmt.Errorf("userinfo is not a compact JWS (%d parts), so it cannot be checked and signed",
			len(parts))
	}
	headerJSON, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return "", errors.New("userinfo header is not base64url")
	}
	var header struct {
		Alg string `json:"alg"`
		Kid string `json:"kid"`
	}
	if err := json.Unmarshal(headerJSON, &header); err != nil {
		return "", errors.New("userinfo header is not a JSON object")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", errors.New("userinfo payload is not base64url")
	}
	var claims map[string]any
	if err := json.Unmarshal(payload, &claims); err != nil {
		return "", errors.New("userinfo payload is not a JSON object")
	}

	keys, err := cfg.Signer.GetPublicKeys(ctx, providers.PublicKeyFilter{KeyID: cfg.SigningKeyID})
	if err != nil || len(keys) == 0 || keys[0].KeyID == "" || keys[0].Algorithm == "" {
		return "", errors.New("no userinfo signing key")
	}
	signing := keys[0]
	if !exchange && header.Kid == signing.KeyID && header.Alg == signing.Algorithm {
		return "", nil
	}
	err = verifyIdentitySystemToken(compact, header.Kid, keys[1:])
	if errors.Is(err, errUnknownIdentitySystemKey) {
		if refresher, ok := cfg.Signer.(IdentitySystemKeyRefresher); ok {
			err = verifyIdentitySystemToken(compact, header.Kid, refresher.RefreshIdentitySystemKeys(ctx))
		}
	}
	if err != nil {
		return "", err
	}
	if exchange {
		limitToScope(claims, token, cfg.ScopeClaims)
	}
	return cfg.sign(ctx, signing, claims)
}

// errUnknownIdentitySystemKey: no identity system key has the token's kid.
var errUnknownIdentitySystemKey = errors.New("no identity system key for the userinfo kid")

// verifyIdentitySystemToken checks the identity system's token against the identity system key
// whose kid (or certificate thumbprint) matches the token's kid.
func verifyIdentitySystemToken(compact, kid string, idSystemKeys []providers.PublicKeyInfo) error {
	if kid == "" {
		return errors.New("identity system userinfo has no kid")
	}
	jws, err := jose.ParseSigned(compact, identitySystemAlgorithms)
	if err != nil {
		return fmt.Errorf("identity system userinfo is not an accepted JWS: %w", err)
	}
	for _, key := range idSystemKeys {
		if key.PublicKey == nil || (key.KeyID != kid && key.Thumbprint != kid) {
			continue
		}
		if _, err := jws.Verify(key.PublicKey); err != nil {
			return fmt.Errorf("identity system userinfo signature does not verify (kid %s)", kid)
		}
		return nil
	}
	return fmt.Errorf("%w %s", errUnknownIdentitySystemKey, kid)
}

// limitToScope removes the claims the access token's scopes do not release and sets aud to the
// requesting client.
func limitToScope(claims map[string]any, token accessTokenClaims, scopeClaims map[string][]string) {
	allowed := map[string]bool{}
	for _, scope := range strings.Fields(token.Scope) {
		for _, claim := range scopeClaims[scope] {
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
}

// sign signs claims with the engine's signing key, using the alg and kid /oauth2/jwks publishes
// for that key (the JWKS kid is PublicKeyInfo.KeyID), so a client that requires the token's alg
// to equal the published alg accepts it.
func (cfg UserinfoScopeConfig) sign(ctx context.Context, key providers.PublicKeyInfo, claims map[string]any) (string, error) {
	header, err := json.Marshal(map[string]string{"alg": key.Algorithm, "typ": "JWT", "kid": key.KeyID})
	if err != nil {
		return "", errors.New("userinfo header cannot be encoded")
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", errors.New("userinfo claims cannot be encoded")
	}
	signingInput := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(payload)
	sig, err := cfg.Signer.Sign(ctx, providers.KeyRef{KeyID: cfg.SigningKeyID}, key.Algorithm, []byte(signingInput))
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
	applog.GetLogger().Error(ctx, "userinfo not issued", applog.String("reason", reason))
	body, _ := json.Marshal(map[string]string{
		"error":             "server_error",
		"error_description": "userinfo cannot be issued",
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
