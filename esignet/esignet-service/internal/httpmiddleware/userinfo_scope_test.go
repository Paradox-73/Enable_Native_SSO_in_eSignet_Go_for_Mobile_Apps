/*
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */

package httpmiddleware

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thunder-id/thunderid/pkg/thunderidengine/providers"
)

const (
	testHubClient   = "hub-client"
	testChildClient = "health-card-client"
	testSigningKey  = "OIDC_SERVICE_SIGN"
	testThumbprint  = "engine-key-thumbprint"
	tokenExchange   = "urn:ietf:params:oauth:grant-type:token-exchange"
)

// testScopeClaims is the scope_claims block of data/deployment.yaml.
var testScopeClaims = map[string][]string{
	"openid":  nil,
	"profile": {"name", "given_name", "picture", "gender", "birthdate"},
	"email":   {"email", "email_verified"},
	"phone":   {"phone_number", "phone_number_verified"},
	"address": {"address"},
}

// fakeUserinfoSigner signs with an in-memory ES256 key and records the key reference it got.
type fakeUserinfoSigner struct {
	key       *ecdsa.PrivateKey
	keysErr   error
	signErr   error
	gotFilter providers.PublicKeyFilter
	gotKeyRef providers.KeyRef
	gotAlg    string
}

func newFakeUserinfoSigner(t *testing.T) *fakeUserinfoSigner {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	return &fakeUserinfoSigner{key: key}
}

func (f *fakeUserinfoSigner) GetPublicKeys(_ context.Context, filter providers.PublicKeyFilter) ([]providers.PublicKeyInfo, error) {
	f.gotFilter = filter
	if f.keysErr != nil {
		return nil, f.keysErr
	}
	return []providers.PublicKeyInfo{
		{KeyID: "engine", Algorithm: "ES256", PublicKey: &f.key.PublicKey, Thumbprint: testThumbprint},
		{KeyID: "identity-system", Algorithm: "RS256", Thumbprint: "identity-system-thumbprint"},
	}, nil
}

func (f *fakeUserinfoSigner) Sign(_ context.Context, keyRef providers.KeyRef, alg string, content []byte) ([]byte, error) {
	f.gotKeyRef, f.gotAlg = keyRef, alg
	if f.signErr != nil {
		return nil, f.signErr
	}
	digest := sha256.Sum256(content)
	r, s, err := ecdsa.Sign(rand.Reader, f.key, digest[:])
	if err != nil {
		return nil, err
	}
	sig := make([]byte, 64)
	r.FillBytes(sig[:32])
	s.FillBytes(sig[32:])
	return sig, nil
}

func (f *fakeUserinfoSigner) verify(t *testing.T, compact string) {
	t.Helper()
	parts := strings.Split(compact, ".")
	require.Len(t, parts, 3)
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	require.NoError(t, err)
	require.Len(t, sig, 64)
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	assert.True(t, ecdsa.Verify(&f.key.PublicKey, digest[:], new(big.Int).SetBytes(sig[:32]), new(big.Int).SetBytes(sig[32:])),
		"signature of the rebuilt userinfo token")
}

func b64JSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return base64.RawURLEncoding.EncodeToString(b)
}

// unsignedJWT builds a compact JWS with a dummy signature: the filter never verifies the access
// token (the engine already did) nor the identity system's token.
func unsignedJWT(t *testing.T, claims map[string]any) string {
	t.Helper()
	return b64JSON(t, map[string]string{"alg": "RS256", "kid": "identity-system"}) + "." + b64JSON(t, claims) + ".c2ln"
}

func accessToken(t *testing.T, grantType, scope, clientID string) string {
	t.Helper()
	return unsignedJWT(t, map[string]any{"grant_type": grantType, "scope": scope, "client_id": clientID,
		"sub": "psut-1", "aci": "cache-1"})
}

// identitySystemUserinfo is the identity system's signed userinfo token cached at the Hub login,
// which released every claim of "openid profile email phone".
func identitySystemUserinfo(t *testing.T) string {
	t.Helper()
	return unsignedJWT(t, map[string]any{
		"iss": "http://mock-identity-system", "sub": "psut-1", "aud": testHubClient, "iat": 1700000000,
		"name": "Siddharth K Mansour", "name#eng": "Siddharth K Mansour", "birthdate": "1987/11/25",
		"email": "siddhartha.km@gmail.com", "phone_number": "+919427357934",
	})
}

// engineUserinfo stands in for the engine's userinfo handler: 200 with the cached token as
// application/jwt.
func engineUserinfo(body string, status int) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/jwt")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	})
}

func serveUserinfo(t *testing.T, signer UserinfoSigner, next http.Handler, method, path, authorization string) *httptest.ResponseRecorder {
	t.Helper()
	mw := UserinfoScope(UserinfoScopeConfig{ScopeClaims: testScopeClaims, SigningKeyID: testSigningKey, Signer: signer})
	req := httptest.NewRequest(method, path, nil)
	if authorization != "" {
		req.Header.Set("Authorization", authorization)
	}
	rr := httptest.NewRecorder()
	mw(next).ServeHTTP(rr, req)
	return rr
}

func payloadOf(t *testing.T, compact string) (map[string]any, map[string]any) {
	t.Helper()
	parts := strings.Split(compact, ".")
	require.Len(t, parts, 3)
	var header, claims map[string]any
	h, err := base64.RawURLEncoding.DecodeString(parts[0])
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(h, &header))
	p, err := base64.RawURLEncoding.DecodeString(parts[1])
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(p, &claims))
	return header, claims
}

func TestUserinfoScope_NativeSSONarrowerChildScopeHidesClaims(t *testing.T) {
	signer := newFakeUserinfoSigner(t)
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		t.Run(method, func(t *testing.T) {
			rr := serveUserinfo(t, signer, engineUserinfo(identitySystemUserinfo(t), http.StatusOK), method, UserinfoPath,
				"DPoP "+accessToken(t, tokenExchange, "openid email", testChildClient))

			require.Equal(t, http.StatusOK, rr.Code)
			assert.Equal(t, "application/jwt", rr.Header().Get("Content-Type"))
			assert.Equal(t, "no-store", rr.Header().Get("Cache-Control"))
			body := rr.Body.String()
			signer.verify(t, body)
			header, claims := payloadOf(t, body)
			assert.Equal(t, map[string]any{"alg": "ES256", "typ": "JWT", "kid": testThumbprint}, header)
			assert.Equal(t, map[string]any{
				"iss": "http://mock-identity-system", "sub": "psut-1", "aud": testChildClient, "iat": float64(1700000000),
				"email": "siddhartha.km@gmail.com",
			}, claims)
			assert.Equal(t, providers.PublicKeyFilter{KeyID: testSigningKey}, signer.gotFilter)
			assert.Equal(t, providers.KeyRef{KeyID: testSigningKey}, signer.gotKeyRef)
			assert.Equal(t, "ES256", signer.gotAlg)
		})
	}
}

func TestUserinfoScope_NativeSSOEqualScopeKeepsGrantedClaims(t *testing.T) {
	signer := newFakeUserinfoSigner(t)
	rr := serveUserinfo(t, signer, engineUserinfo(identitySystemUserinfo(t), http.StatusOK), http.MethodGet, UserinfoPath,
		"Bearer "+accessToken(t, tokenExchange, "openid profile email phone", testChildClient))

	require.Equal(t, http.StatusOK, rr.Code)
	signer.verify(t, rr.Body.String())
	_, claims := payloadOf(t, rr.Body.String())
	for _, name := range []string{"name", "name#eng", "birthdate", "email", "phone_number", "sub"} {
		assert.Contains(t, claims, name)
	}
	assert.Equal(t, testChildClient, claims["aud"])
}

func TestUserinfoScope_NativeSSOOpenIDOnlyKeepsOnlyRegisteredClaims(t *testing.T) {
	signer := newFakeUserinfoSigner(t)
	rr := serveUserinfo(t, signer, engineUserinfo(identitySystemUserinfo(t), http.StatusOK), http.MethodGet, UserinfoPath,
		"DPoP "+accessToken(t, tokenExchange, "openid", testChildClient))

	require.Equal(t, http.StatusOK, rr.Code)
	_, claims := payloadOf(t, rr.Body.String())
	assert.ElementsMatch(t, []string{"iss", "sub", "aud", "iat"}, keysOf(claims))
}

func TestUserinfoScope_NativeSSOWithoutAudLeavesAudOut(t *testing.T) {
	signer := newFakeUserinfoSigner(t)
	kyc := unsignedJWT(t, map[string]any{"sub": "psut-1", "email": "a@b.c", "name": "A"})
	rr := serveUserinfo(t, signer, engineUserinfo(kyc, http.StatusOK), http.MethodGet, UserinfoPath,
		"DPoP "+accessToken(t, tokenExchange, "openid email", testChildClient))

	require.Equal(t, http.StatusOK, rr.Code)
	_, claims := payloadOf(t, rr.Body.String())
	assert.Equal(t, map[string]any{"sub": "psut-1", "email": "a@b.c"}, claims)
}

func TestUserinfoScope_PassesThroughUnchanged(t *testing.T) {
	kyc := identitySystemUserinfo(t)
	cases := []struct {
		name          string
		method, path  string
		authorization string
		next          http.Handler
		wantStatus    int
		wantBody      string
	}{
		{name: "authorization-code token", method: http.MethodGet, path: UserinfoPath,
			authorization: "DPoP " + accessToken(t, "authorization_code", "openid email", testHubClient),
			next:          engineUserinfo(kyc, http.StatusOK), wantStatus: http.StatusOK, wantBody: kyc},
		{name: "engine error", method: http.MethodGet, path: UserinfoPath,
			authorization: "DPoP " + accessToken(t, tokenExchange, "openid email", testChildClient),
			next:          engineUserinfo(`{"error":"invalid_token"}`, http.StatusUnauthorized),
			wantStatus:    http.StatusUnauthorized, wantBody: `{"error":"invalid_token"}`},
		{name: "engine error without a token", method: http.MethodGet, path: UserinfoPath,
			next:       engineUserinfo("", http.StatusUnauthorized),
			wantStatus: http.StatusUnauthorized, wantBody: ""},
		{name: "JSON userinfo built by the engine", method: http.MethodGet, path: UserinfoPath,
			authorization: "DPoP " + accessToken(t, tokenExchange, "openid email", testChildClient),
			next:          engineUserinfo(`{"sub":"psut-1","email":"a@b.c"}`, http.StatusOK),
			wantStatus:    http.StatusOK, wantBody: `{"sub":"psut-1","email":"a@b.c"}`},
		{name: "other path", method: http.MethodGet, path: "/oauth2/token",
			authorization: "DPoP " + accessToken(t, tokenExchange, "openid email", testChildClient),
			next:          engineUserinfo(kyc, http.StatusOK), wantStatus: http.StatusOK, wantBody: kyc},
		{name: "OPTIONS", method: http.MethodOptions, path: UserinfoPath,
			next: engineUserinfo("", http.StatusNoContent), wantStatus: http.StatusNoContent, wantBody: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rr := serveUserinfo(t, newFakeUserinfoSigner(t), tc.next, tc.method, tc.path, tc.authorization)
			assert.Equal(t, tc.wantStatus, rr.Code)
			assert.Equal(t, tc.wantBody, rr.Body.String())
		})
	}
}

func TestUserinfoScope_NoSignerPassesThrough(t *testing.T) {
	kyc := identitySystemUserinfo(t)
	rr := serveUserinfo(t, nil, engineUserinfo(kyc, http.StatusOK), http.MethodGet, UserinfoPath,
		"DPoP "+accessToken(t, tokenExchange, "openid email", testChildClient))
	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, kyc, rr.Body.String())
}

func TestUserinfoScope_RefusesWhatItCannotFilter(t *testing.T) {
	exchangeToken := "DPoP " + accessToken(t, tokenExchange, "openid email", testChildClient)
	cases := []struct {
		name          string
		authorization string
		body          string
		keysErr       error
		signErr       error
	}{
		{name: "encrypted userinfo", authorization: exchangeToken, body: "a.b.c.d.e"},
		{name: "payload not base64url", authorization: exchangeToken, body: "a.!!!.c"},
		{name: "payload not JSON", authorization: exchangeToken,
			body: "a." + base64.RawURLEncoding.EncodeToString([]byte("[1]")) + ".c"},
		{name: "no signing key", authorization: exchangeToken, body: identitySystemUserinfo(t),
			keysErr: errors.New("key store down")},
		{name: "signing fails", authorization: exchangeToken, body: identitySystemUserinfo(t),
			signErr: errors.New("hsm down")},
		{name: "no authorization header", body: identitySystemUserinfo(t)},
		{name: "unknown scheme", authorization: "Basic abc", body: identitySystemUserinfo(t)},
		{name: "opaque access token", authorization: "DPoP opaque-token", body: identitySystemUserinfo(t)},
		{name: "access token payload not base64url", authorization: "DPoP a.!!!.c", body: identitySystemUserinfo(t)},
		{name: "access token payload not JSON", authorization: "DPoP a." +
			base64.RawURLEncoding.EncodeToString([]byte("[1]")) + ".c", body: identitySystemUserinfo(t)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			signer := newFakeUserinfoSigner(t)
			signer.keysErr, signer.signErr = tc.keysErr, tc.signErr
			rr := serveUserinfo(t, signer, engineUserinfo(tc.body, http.StatusOK), http.MethodGet, UserinfoPath,
				tc.authorization)

			assert.Equal(t, http.StatusInternalServerError, rr.Code)
			assert.Equal(t, "application/json", rr.Header().Get("Content-Type"))
			assert.JSONEq(t, `{"error":"server_error","error_description":"userinfo cannot be limited to the access token's scope"}`,
				rr.Body.String())
			assert.NotContains(t, rr.Body.String(), "siddhartha")
		})
	}
}

func TestBufferedResponse_KeepsFirstStatusAndDefaultsToOK(t *testing.T) {
	b := newBufferedResponse()
	_, err := b.Write([]byte("x"))
	require.NoError(t, err)
	b.WriteHeader(http.StatusTeapot)
	assert.Equal(t, http.StatusOK, b.status)

	rr := httptest.NewRecorder()
	b.copyTo(rr, []byte("xy"))
	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, "2", rr.Header().Get("Content-Length"))
	assert.Equal(t, "xy", rr.Body.String())
}

func keysOf(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
