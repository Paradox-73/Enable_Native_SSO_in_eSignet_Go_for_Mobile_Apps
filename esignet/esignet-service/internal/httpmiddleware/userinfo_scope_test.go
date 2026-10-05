/*
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */

package httpmiddleware

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	jose "github.com/go-jose/go-jose/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thunder-id/thunderid/pkg/thunderidengine/providers"
)

const (
	testHubClient   = "hub-client"
	testChildClient = "health-card-client"
	testSigningKey  = "OIDC_SERVICE_SIGN"
	testEngineKid   = "engine-key-id"
	testThumbprint  = "engine-key-thumbprint"
	testIDSystemKid = "identity-system"
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
	t         *testing.T
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
	return &fakeUserinfoSigner{t: t, key: key}
}

func (f *fakeUserinfoSigner) GetPublicKeys(_ context.Context, filter providers.PublicKeyFilter) ([]providers.PublicKeyInfo, error) {
	f.gotFilter = filter
	if f.keysErr != nil {
		return nil, f.keysErr
	}
	// The order and fields the engine's /oauth2/jwks is built from: the engine's key first, then
	// the identity system's key, published with alg PS256 although that system signs with RS256.
	return []providers.PublicKeyInfo{
		{KeyID: testEngineKid, Algorithm: "ES256", PublicKey: &f.key.PublicKey, Thumbprint: testThumbprint},
		{KeyID: testIDSystemKid, Algorithm: "PS256", PublicKey: &idSystemKey(f.t).PublicKey,
			Thumbprint: "identity-system-thumbprint"},
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

var (
	idSystemKeyOnce sync.Once
	idSystemKeyVal  *rsa.PrivateKey
)

// idSystemKey is the identity system's RSA signing key, shared by every test.
func idSystemKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	idSystemKeyOnce.Do(func() {
		key, err := rsa.GenerateKey(rand.Reader, 2048)
		require.NoError(t, err)
		idSystemKeyVal = key
	})
	return idSystemKeyVal
}

// unsignedJWT builds a compact JWS with a dummy signature, used for access tokens: the
// middleware does not verify them (the engine already did).
func unsignedJWT(t *testing.T, claims map[string]any) string {
	t.Helper()
	return b64JSON(t, map[string]string{"alg": "RS256", "kid": testIDSystemKid}) + "." + b64JSON(t, claims) + ".c2ln"
}

// idSystemJWT signs claims the way the MOSIP mock identity system does: RS256 with its own key.
func idSystemJWT(t *testing.T, alg jose.SignatureAlgorithm, kid string, claims map[string]any) string {
	t.Helper()
	opts := (&jose.SignerOptions{}).WithType("JWT")
	if kid != "" {
		opts = opts.WithHeader("kid", kid)
	}
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: alg, Key: idSystemKey(t)}, opts)
	require.NoError(t, err)
	payload, err := json.Marshal(claims)
	require.NoError(t, err)
	jws, err := signer.Sign(payload)
	require.NoError(t, err)
	compact, err := jws.CompactSerialize()
	require.NoError(t, err)
	return compact
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
	return idSystemJWT(t, jose.RS256, testIDSystemKid, identitySystemClaims())
}

func identitySystemClaims() map[string]any {
	return map[string]any{
		"iss": "http://mock-identity-system", "sub": "psut-1", "aud": testHubClient, "iat": 1700000000,
		"name": "Siddharth K Mansour", "name#eng": "Siddharth K Mansour", "birthdate": "1987/11/25",
		"email": "siddhartha.km@gmail.com", "phone_number": "+919427357934",
	}
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
			assert.Equal(t, map[string]any{"alg": "ES256", "typ": "JWT", "kid": testEngineKid}, header)
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
	kyc := idSystemJWT(t, jose.RS256, testIDSystemKid, map[string]any{"sub": "psut-1", "email": "a@b.c", "name": "A"})
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
		{name: "authorization-code token, encrypted for the client", method: http.MethodGet, path: UserinfoPath,
			authorization: "DPoP " + accessToken(t, "authorization_code", "openid email", testHubClient),
			next:          engineUserinfo("a.b.c.d.e", http.StatusOK), wantStatus: http.StatusOK, wantBody: "a.b.c.d.e"},
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
	codeToken := "DPoP " + accessToken(t, "authorization_code", "openid email", testHubClient)
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
		{name: "no signing key, authorization-code token", authorization: codeToken, body: identitySystemUserinfo(t),
			keysErr: errors.New("key store down")},
		{name: "header not JSON", authorization: codeToken,
			body: base64.RawURLEncoding.EncodeToString([]byte("[1]")) + ".e30.c2ln"},
		{name: "identity system signature does not verify", authorization: codeToken,
			body: tamperedPayload(t, identitySystemUserinfo(t))},
		{name: "identity system signature does not verify, token exchange", authorization: exchangeToken,
			body: tamperedPayload(t, identitySystemUserinfo(t))},
		{name: "unknown kid", authorization: codeToken,
			body: idSystemJWT(t, jose.RS256, "some-other-key", identitySystemClaims())},
		{name: "kid of the engine but signed with the identity system key", authorization: codeToken,
			body: idSystemJWT(t, jose.RS256, testEngineKid, identitySystemClaims())},
		{name: "no kid", authorization: codeToken, body: idSystemJWT(t, jose.RS256, "", identitySystemClaims())},
		{name: "alg none", authorization: codeToken,
			body: b64JSON(t, map[string]string{"alg": "none", "kid": testIDSystemKid}) + "." +
				b64JSON(t, identitySystemClaims()) + "."},
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
			assert.JSONEq(t, `{"error":"server_error","error_description":"userinfo cannot be issued"}`,
				rr.Body.String())
			assert.NotContains(t, rr.Body.String(), "siddhartha")
		})
	}
}

// tamperedPayload swaps the payload of a signed token, keeping its header and signature.
func tamperedPayload(t *testing.T, compact string) string {
	t.Helper()
	parts := strings.Split(compact, ".")
	require.Len(t, parts, 3)
	return parts[0] + "." + b64JSON(t, map[string]any{"sub": "someone-else"}) + "." + parts[2]
}

// jwksEntryFor returns the published key (as /oauth2/jwks lists it) whose kid is kid.
func jwksEntryFor(t *testing.T, signer UserinfoSigner, kid string) providers.PublicKeyInfo {
	t.Helper()
	keys, err := signer.GetPublicKeys(context.Background(), providers.PublicKeyFilter{})
	require.NoError(t, err)
	for _, k := range keys {
		if k.KeyID == kid {
			return k
		}
	}
	t.Fatalf("no published key with kid %q", kid)
	return providers.PublicKeyInfo{}
}

// The authorization-code login (the Hub's own sign-in): the identity system's RS256 token is
// checked and signed again with the engine's key, with the alg and kid of that key's published
// entry; every claim is kept.
func TestUserinfoScope_AuthorizationCodeTokenSignedWithPublishedAlgAndKid(t *testing.T) {
	signer := newFakeUserinfoSigner(t)
	rr := serveUserinfo(t, signer, engineUserinfo(identitySystemUserinfo(t), http.StatusOK), http.MethodGet, UserinfoPath,
		"DPoP "+accessToken(t, "authorization_code", "openid profile email phone", testHubClient))

	require.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, "application/jwt", rr.Header().Get("Content-Type"))
	body := rr.Body.String()
	signer.verify(t, body)
	header, claims := payloadOf(t, body)
	published := jwksEntryFor(t, signer, header["kid"].(string))
	assert.Equal(t, published.Algorithm, header["alg"], "alg must equal the published alg of the kid")
	assert.Equal(t, testEngineKid, header["kid"])
	assert.NotEqual(t, "RS256", header["alg"])
	want := identitySystemClaims()
	want["iat"] = float64(1700000000)
	assert.Equal(t, want, claims, "claims and aud of an authorization-code login are kept")
	assert.Equal(t, "ES256", signer.gotAlg)
}

// With the deployment's RSA signing key the rebuilt token is PS256, the alg /oauth2/jwks lists.
func TestUserinfoScope_RSAEngineKeySignsPS256(t *testing.T) {
	engineKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	signer := &rsaUserinfoSigner{t: t, key: engineKey}
	for _, grant := range []string{"authorization_code", tokenExchange} {
		t.Run(grant, func(t *testing.T) {
			rr := serveUserinfo(t, signer, engineUserinfo(identitySystemUserinfo(t), http.StatusOK), http.MethodGet,
				UserinfoPath, "DPoP "+accessToken(t, grant, "openid email", testChildClient))

			require.Equal(t, http.StatusOK, rr.Code)
			header, _ := payloadOf(t, rr.Body.String())
			published := jwksEntryFor(t, signer, header["kid"].(string))
			assert.Equal(t, "PS256", header["alg"])
			assert.Equal(t, published.Algorithm, header["alg"])
			jws, err := jose.ParseSigned(rr.Body.String(), []jose.SignatureAlgorithm{jose.PS256})
			require.NoError(t, err)
			_, err = jws.Verify(&engineKey.PublicKey)
			require.NoError(t, err)
		})
	}
}

// A token the engine signed itself with its key and published alg is passed on unchanged.
func TestUserinfoScope_EngineSignedTokenPassesThrough(t *testing.T) {
	signer := newFakeUserinfoSigner(t)
	own := b64JSON(t, map[string]string{"alg": "ES256", "kid": testEngineKid}) + "." + b64JSON(t, map[string]any{"sub": "psut-1"}) + ".c2ln"
	rr := serveUserinfo(t, signer, engineUserinfo(own, http.StatusOK), http.MethodGet, UserinfoPath,
		"DPoP "+accessToken(t, "authorization_code", "openid", testHubClient))
	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, own, rr.Body.String())
}

// refreshingSigner lists no identity system key until it is asked to fetch them again.
type refreshingSigner struct {
	*fakeUserinfoSigner
	refreshed []providers.PublicKeyInfo
	refreshes int
}

func (f *refreshingSigner) GetPublicKeys(ctx context.Context, filter providers.PublicKeyFilter) ([]providers.PublicKeyInfo, error) {
	keys, err := f.fakeUserinfoSigner.GetPublicKeys(ctx, filter)
	if err != nil {
		return nil, err
	}
	return keys[:1], nil
}

func (f *refreshingSigner) RefreshIdentitySystemKeys(context.Context) []providers.PublicKeyInfo {
	f.refreshes++
	return f.refreshed
}

// A key the identity system created after its keys were cached: the middleware asks for the keys
// again once, and refuses the token when the key is still unknown.
func TestUserinfoScope_UnknownKidFetchesIdentitySystemKeysAgain(t *testing.T) {
	codeToken := "DPoP " + accessToken(t, "authorization_code", "openid email", testHubClient)

	signer := &refreshingSigner{fakeUserinfoSigner: newFakeUserinfoSigner(t)}
	signer.refreshed = []providers.PublicKeyInfo{{KeyID: testIDSystemKid, Algorithm: "PS256", PublicKey: &idSystemKey(t).PublicKey}}
	rr := serveUserinfo(t, signer, engineUserinfo(identitySystemUserinfo(t), http.StatusOK), http.MethodGet, UserinfoPath, codeToken)
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	signer.verify(t, rr.Body.String())
	assert.Equal(t, 1, signer.refreshes)

	stillUnknown := &refreshingSigner{fakeUserinfoSigner: newFakeUserinfoSigner(t)}
	rr = serveUserinfo(t, stillUnknown, engineUserinfo(identitySystemUserinfo(t), http.StatusOK), http.MethodGet, UserinfoPath, codeToken)
	assert.Equal(t, http.StatusInternalServerError, rr.Code)
	assert.Equal(t, 1, stillUnknown.refreshes)

	// A tampered token is refused even when the refresh finds its key.
	known := &refreshingSigner{fakeUserinfoSigner: newFakeUserinfoSigner(t)}
	known.refreshed = signer.refreshed
	rr = serveUserinfo(t, known, engineUserinfo(tamperedPayload(t, identitySystemUserinfo(t)), http.StatusOK), http.MethodGet,
		UserinfoPath, codeToken)
	assert.Equal(t, http.StatusInternalServerError, rr.Code)
	assert.Equal(t, 1, known.refreshes)
}

// rsaUserinfoSigner is the engine with an RSA signing key, published with alg PS256.
type rsaUserinfoSigner struct {
	t   *testing.T
	key *rsa.PrivateKey
}

func (f *rsaUserinfoSigner) GetPublicKeys(context.Context, providers.PublicKeyFilter) ([]providers.PublicKeyInfo, error) {
	return []providers.PublicKeyInfo{
		{KeyID: "O10N-engine", Algorithm: "PS256", PublicKey: &f.key.PublicKey, Thumbprint: "O10N-engine"},
		{KeyID: testIDSystemKid, Algorithm: "PS256", PublicKey: &idSystemKey(f.t).PublicKey},
	}, nil
}

func (f *rsaUserinfoSigner) Sign(_ context.Context, _ providers.KeyRef, alg string, content []byte) ([]byte, error) {
	if alg != "PS256" {
		return nil, errors.New("unexpected alg " + alg)
	}
	digest := sha256.Sum256(content)
	return rsa.SignPSS(rand.Reader, f.key, crypto.SHA256, digest[:], &rsa.PSSOptions{SaltLength: rsa.PSSSaltLengthEqualsHash})
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
