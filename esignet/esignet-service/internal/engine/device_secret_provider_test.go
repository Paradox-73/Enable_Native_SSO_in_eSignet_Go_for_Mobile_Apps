/*
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */

package engine

import (
	"context"
	"crypto"
	"crypto/x509"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/stretchr/testify/suite"
	"github.com/thunder-id/thunderid/pkg/thunderidengine/common"
	engineconfig "github.com/thunder-id/thunderid/pkg/thunderidengine/config"
	"github.com/thunder-id/thunderid/pkg/thunderidengine/providers"

	"github.com/mosip/esignet/internal/config"
	"github.com/mosip/esignet/internal/consentmgmt"
	"github.com/mosip/esignet/internal/engine/runtimestores/inmemory"
	"github.com/mosip/esignet/internal/keymanager"
	"github.com/mosip/esignet/internal/keymanager/cryptomanager"
	"github.com/mosip/esignet/internal/keymanager/db"
	"github.com/mosip/esignet/internal/keymanager/kmtest"
	"github.com/mosip/esignet/internal/keymanager/signature"
)

const (
	dsTestIssuer   = "https://esignet.example.com"
	dsTestValidity = int64(28800)
	dsTestLeeway   = int64(10)
)

// deviceSecretKeyQuerier extends kmtest.StateQuerier with the DB-resident key_store rows and the
// thumbprint lookup that cryptomanager's JWTEncrypt/JWTDecrypt need, so the tests run the real
// keymanager, signature and cryptomanager services rather than fakes of them.
type deviceSecretKeyQuerier struct {
	*kmtest.StateQuerier
	mu      sync.Mutex
	byThumb map[string]db.KeyAlias
	records map[string]db.KeyStoreRecord
}

func newDeviceSecretKeyQuerier() *deviceSecretKeyQuerier {
	return &deviceSecretKeyQuerier{
		StateQuerier: kmtest.NewStateQuerier(),
		byThumb:      map[string]db.KeyAlias{},
		records:      map[string]db.KeyStoreRecord{},
	}
}

func (q *deviceSecretKeyQuerier) remember(k db.KeyAlias) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if k.CertThumbprint != nil {
		q.byThumb[*k.CertThumbprint] = k
	}
}

func (q *deviceSecretKeyQuerier) InsertKeyAlias(ctx context.Context, k db.KeyAlias) error {
	q.remember(k)
	return q.StateQuerier.InsertKeyAlias(ctx, k)
}

func (q *deviceSecretKeyQuerier) UpdateKeyAlias(ctx context.Context, k db.KeyAlias) error {
	q.remember(k)
	return q.StateQuerier.UpdateKeyAlias(ctx, k)
}

func (q *deviceSecretKeyQuerier) GetKeyAliasByCertThumbprint(_ context.Context, thumb string) (db.KeyAlias, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	k, ok := q.byThumb[thumb]
	if !ok {
		return db.KeyAlias{}, sql.ErrNoRows
	}
	return k, nil
}

func (q *deviceSecretKeyQuerier) GetKeyStoreRecord(_ context.Context, id string) (db.KeyStoreRecord, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	r, ok := q.records[id]
	if !ok {
		return db.KeyStoreRecord{}, sql.ErrNoRows
	}
	return r, nil
}

func (q *deviceSecretKeyQuerier) InsertKeyStoreRecord(_ context.Context, r db.KeyStoreRecord) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.records[r.ID] = r
	return nil
}

func (q *deviceSecretKeyQuerier) UpdateKeyStoreRecord(ctx context.Context, r db.KeyStoreRecord) error {
	return q.InsertKeyStoreRecord(ctx, r)
}

// deviceSecretKeys is one server's key hierarchy: ROOT, the OIDC_SERVICE master key, its EC sign
// key and the DEVICE_SECRET encryption key, as cmd/esignet/main.go provisions them.
type deviceSecretKeys struct {
	sig    *signature.Service
	crypto *cryptomanager.Service
}

func newDeviceSecretKeys(t *testing.T) deviceSecretKeys {
	t.Helper()
	ctx := context.Background()
	q := newDeviceSecretKeyQuerier()
	km := keymanager.NewServiceWithQuerier(q, kmtest.NewFakeKeyStore(), keymanager.Config{
		AsymmetricKeyLength: 2048,
		CertCommonName:      "www.mosip.io",
	})
	for _, req := range []keymanager.GenerateMasterKeyRequest{
		{ApplicationID: keymanager.AppIDRoot, CommonName: "MOSIP Root CA"},
		{ApplicationID: config.OIDCServiceAppID, ReferenceID: keymanager.RefIDRSA2048},
		{ApplicationID: config.OIDCServiceAppID, ReferenceID: keymanager.RefIDECSECP256R1Sign},
	} {
		req.ObjectType = keymanager.ObjectTypeCertificate
		if _, err := km.GenerateMasterKey(ctx, req); err != nil {
			t.Fatalf("generate key %s/%s: %v", req.ApplicationID, req.ReferenceID, err)
		}
	}
	if _, err := km.GetCertificate(ctx, config.OIDCServiceAppID, "DEVICE_SECRET"); err != nil {
		t.Fatalf("create device secret encryption key: %v", err)
	}
	return deviceSecretKeys{
		sig:    signature.NewService(km),
		crypto: cryptomanager.NewService(q, km, cryptomanager.Config{}),
	}
}

func deviceSecretTestConfig() *config.AppConfig {
	return &config.AppConfig{
		Issuer: dsTestIssuer,
		JWT:    engineconfig.JWTConfig{PreferredKeyID: keymanager.RefIDRSA2048, Leeway: dsTestLeeway},
		NativeSSO: config.NativeSSOConfig{
			DeviceSecretValiditySeconds: dsTestValidity,
			EncryptionKeyRefID:          "DEVICE_SECRET",
			SigningKeyRefID:             keymanager.RefIDECSECP256R1Sign,
		},
	}
}

func deviceSecretTestRequest() providers.DeviceSecretRequest {
	return providers.DeviceSecretRequest{
		ClientID:         "hub-client",
		OUID:             "ps03-vendor",
		Subject:          "psut-123",
		AuthID:           "authz-req-1",
		SessionID:        "sid-1",
		Scopes:           []string{"openid", "profile", providers.ScopeDeviceSSO},
		ACR:              "mosip:idp:acr:generated-code",
		Claims:           []string{"name", "email"},
		AuthTime:         1_760_000_000,
		DPoPJkt:          "jkt-thumbprint",
		AttributeCacheID: "aci-1",
	}
}

// stubDeviceSecretSigner wraps a real signer and fails the call whose error is set.
type stubDeviceSecretSigner struct {
	DeviceSecretSigner
	resolveErr error
	verifyErr  error
}

func (s *stubDeviceSecretSigner) ResolveSigner(ctx context.Context, appID, refID string) (*x509.Certificate,
	crypto.Signer, error) {
	if s.resolveErr != nil {
		return nil, nil, s.resolveErr
	}
	return s.DeviceSecretSigner.ResolveSigner(ctx, appID, refID)
}

func (s *stubDeviceSecretSigner) VerifyRaw(ctx context.Context, appID, refID, alg string, input, sig []byte) error {
	if s.verifyErr != nil {
		return s.verifyErr
	}
	return s.DeviceSecretSigner.VerifyRaw(ctx, appID, refID, alg, input, sig)
}

// stubDeviceSecretCipher wraps a real cipher and fails, or rewrites, the call that is set.
type stubDeviceSecretCipher struct {
	DeviceSecretCipher
	encryptErr  error
	decryptErr  error
	decryptData string
}

func (s *stubDeviceSecretCipher) JWTEncrypt(ctx context.Context,
	req cryptomanager.JWTEncryptRequest) (cryptomanager.JWTCipherResponse, error) {
	if s.encryptErr != nil {
		return cryptomanager.JWTCipherResponse{}, s.encryptErr
	}
	return s.DeviceSecretCipher.JWTEncrypt(ctx, req)
}

func (s *stubDeviceSecretCipher) JWTDecrypt(ctx context.Context,
	req cryptomanager.JWTDecryptRequest) (cryptomanager.JWTCipherResponse, error) {
	if s.decryptErr != nil {
		return cryptomanager.JWTCipherResponse{}, s.decryptErr
	}
	if s.decryptData != "" {
		return cryptomanager.JWTCipherResponse{Data: s.decryptData}, nil
	}
	return s.DeviceSecretCipher.JWTDecrypt(ctx, req)
}

// stubRuntimeStore wraps a real store and fails the call whose error is set.
type stubRuntimeStore struct {
	providers.RuntimeStoreProvider
	putErr    error
	getErr    error
	deleteErr error
}

func (s *stubRuntimeStore) Delete(ctx context.Context, ns providers.RuntimeStoreNamespace, key string) error {
	if s.deleteErr != nil {
		return s.deleteErr
	}
	return s.RuntimeStoreProvider.Delete(ctx, ns, key)
}

func (s *stubRuntimeStore) PutIfNotExists(ctx context.Context, ns providers.RuntimeStoreNamespace, key string,
	value []byte, ttl int64) (bool, error) {
	if s.putErr != nil {
		return false, s.putErr
	}
	return s.RuntimeStoreProvider.PutIfNotExists(ctx, ns, key, value, ttl)
}

func (s *stubRuntimeStore) Get(ctx context.Context, ns providers.RuntimeStoreNamespace, key string) ([]byte, error) {
	if s.getErr != nil {
		return nil, s.getErr
	}
	return s.RuntimeStoreProvider.Get(ctx, ns, key)
}

type DeviceSecretProviderTestSuite struct {
	suite.Suite
	keys    deviceSecretKeys
	store   providers.RuntimeStoreProvider
	history *fakeConsentHistory
	now     time.Time
}

// fakeConsentHistory keeps the consent_history rows a provider appends, or fails with err.
type fakeConsentHistory struct {
	rows []*consentmgmt.ConsentRecord
	err  error
}

func (f *fakeConsentHistory) AppendHistory(_ context.Context, record *consentmgmt.ConsentRecord) error {
	if f.err != nil {
		return f.err
	}
	f.rows = append(f.rows, record)
	return nil
}

func TestDeviceSecretProviderTestSuite(t *testing.T) {
	suite.Run(t, new(DeviceSecretProviderTestSuite))
}

func (ts *DeviceSecretProviderTestSuite) SetupSuite() {
	ts.keys = newDeviceSecretKeys(ts.T())
}

func (ts *DeviceSecretProviderTestSuite) SetupTest() {
	ts.store = inmemory.Initialize("device-secret-test")
	ts.history = &fakeConsentHistory{}
	ts.now = time.Now()
}

func (ts *DeviceSecretProviderTestSuite) clock() time.Time { return ts.now }

// newProvider builds a provider on the suite's keys and store, with optional wrappers.
func (ts *DeviceSecretProviderTestSuite) newProvider(cfg *config.AppConfig, signer DeviceSecretSigner,
	cipher DeviceSecretCipher, store providers.RuntimeStoreProvider) *deviceSecretProvider {
	if cfg == nil {
		cfg = deviceSecretTestConfig()
	}
	if signer == nil {
		signer = ts.keys.sig
	}
	if cipher == nil {
		cipher = ts.keys.crypto
	}
	if store == nil {
		store = ts.store
	}
	p, err := NewDeviceSecretProvider(cfg, signer, cipher, store, ts.history, ts.clock)
	ts.Require().NoError(err)
	return p.(*deviceSecretProvider)
}

func (ts *DeviceSecretProviderTestSuite) generate(p *deviceSecretProvider) string {
	ds, svcErr := p.GenerateDeviceSecret(context.Background(), deviceSecretTestRequest())
	ts.Require().Nil(svcErr)
	return ds.Value
}

// sealInner signs header and claims with the suite's device secret signing key and encrypts the
// result to the suite's DEVICE_SECRET key, so a test can present an inner token of its choice.
func (ts *DeviceSecretProviderTestSuite) sealInner(keys deviceSecretKeys, header, claims any) string {
	ctx := context.Background()
	h, err := json.Marshal(header)
	ts.Require().NoError(err)
	c, err := json.Marshal(claims)
	ts.Require().NoError(err)
	input := base64.RawURLEncoding.EncodeToString(h) + "." + base64.RawURLEncoding.EncodeToString(c)
	alg := deviceSecretJWSAlgorithm
	refID := keymanager.RefIDECSECP256R1Sign
	if m, ok := header.(map[string]any); ok && m["alg"] == "PS256" {
		alg, refID = "PS256", keymanager.RefIDRSA2048
	}
	cert, key, err := keys.sig.ResolveSigner(ctx, config.OIDCServiceAppID, refID)
	ts.Require().NoError(err)
	sig, err := keys.sig.SignWithKey(key, cert.PublicKey, alg, []byte(input))
	ts.Require().NoError(err)
	return ts.seal(input + "." + base64.RawURLEncoding.EncodeToString(sig))
}

// seal encrypts data to the suite's DEVICE_SECRET key exactly as GenerateDeviceSecret does.
func (ts *DeviceSecretProviderTestSuite) seal(data string) string {
	resp, err := ts.keys.crypto.JWTEncrypt(context.Background(), cryptomanager.JWTEncryptRequest{
		ApplicationID: config.OIDCServiceAppID,
		ReferenceID:   "DEVICE_SECRET",
		Data:          data,
	})
	ts.Require().NoError(err)
	return resp.Data
}

func (ts *DeviceSecretProviderTestSuite) validClaims() map[string]any {
	now := ts.now.Unix()
	return map[string]any{
		"iss": dsTestIssuer, "client_id": "hub-client", "auth_id": "authz-req-1", "sid": "sid-1",
		"scope": "openid", "cnf": map[string]any{"jkt": "jkt-thumbprint"},
		"iat": now, "exp": now + 60, "jti": "jti-1",
	}
}

func (ts *DeviceSecretProviderTestSuite) validHeader() map[string]any {
	return map[string]any{"alg": deviceSecretJWSAlgorithm, "typ": deviceSecretJWSType}
}

func (ts *DeviceSecretProviderTestSuite) putSession(sid string, session nativeSSOSession) {
	raw, err := json.Marshal(session)
	ts.Require().NoError(err)
	ts.Require().NoError(ts.store.Put(context.Background(), providers.NamespaceNativeSSOSession, sid, raw, 600))
}

func (ts *DeviceSecretProviderTestSuite) assertInvalid(svcErr *common.ServiceError) {
	ts.Require().NotNil(svcErr)
	ts.Equal(providers.ErrorInvalidDeviceSecret.Code, svcErr.Code)
}

func (ts *DeviceSecretProviderTestSuite) assertSessionEnded(svcErr *common.ServiceError) {
	ts.Require().NotNil(svcErr)
	ts.Equal(providers.ErrorDeviceSecretSessionEnded.Code, svcErr.Code)
}

func (ts *DeviceSecretProviderTestSuite) assertServerError(svcErr *common.ServiceError) {
	ts.Require().NotNil(svcErr)
	ts.Equal(common.ServerErrorType, svcErr.Type)
}

func (ts *DeviceSecretProviderTestSuite) TestNewDeviceSecretProvider_RejectsBadSettings() {
	cases := map[string]func(cfg *config.AppConfig){
		"zero validity":            func(cfg *config.AppConfig) { cfg.NativeSSO.DeviceSecretValiditySeconds = 0 },
		"signing key is token key": func(cfg *config.AppConfig) { cfg.JWT.PreferredKeyID = cfg.NativeSSO.SigningKeyRefID },
	}
	for name, mutate := range cases {
		ts.Run(name, func() {
			cfg := deviceSecretTestConfig()
			mutate(cfg)
			p, err := NewDeviceSecretProvider(cfg, ts.keys.sig, ts.keys.crypto, ts.store, ts.history, nil)
			ts.Error(err)
			ts.Nil(p)
		})
	}
	ts.Run("missing services", func() {
		p, err := NewDeviceSecretProvider(deviceSecretTestConfig(), nil, ts.keys.crypto, ts.store, ts.history, nil)
		ts.ErrorContains(err, "needs the signature")
		ts.Nil(p)
		p, err = NewDeviceSecretProvider(deviceSecretTestConfig(), ts.keys.sig, ts.keys.crypto, ts.store, nil, nil)
		ts.ErrorContains(err, "consent services")
		ts.Nil(p)
	})
	ts.Run("nil clock means time.Now", func() {
		p, err := NewDeviceSecretProvider(deviceSecretTestConfig(), ts.keys.sig, ts.keys.crypto, ts.store, ts.history,
			nil)
		ts.Require().NoError(err)
		ts.WithinDuration(time.Now(), p.(*deviceSecretProvider).now(), time.Minute)
	})
}

func (ts *DeviceSecretProviderTestSuite) TestDeviceSecret_RoundTrip() {
	p := ts.newProvider(nil, nil, nil, nil)
	req := deviceSecretTestRequest()

	ds, svcErr := p.GenerateDeviceSecret(context.Background(), req)
	ts.Require().Nil(svcErr)
	ts.Equal(ts.now.Unix()+dsTestValidity, ds.ExpiresAt)
	ts.Equal(4, strings.Count(ds.Value, "."), "device secret is a compact JWE")

	claims, svcErr := p.ValidateDeviceSecret(context.Background(), ds.Value)
	ts.Require().Nil(svcErr)
	ts.Equal(req, claims.DeviceSecretRequest, "the claims carry the values the device secret was issued for")
	ts.Equal(dsTestIssuer, claims.Issuer)
	ts.Equal(ts.now.Unix(), claims.IssuedAt)
	ts.Equal(ds.ExpiresAt, claims.ExpiresAt)
	ts.NoError(claims.Validate())
}

func (ts *DeviceSecretProviderTestSuite) TestDeviceSecret_EveryClaimPresent() {
	p := ts.newProvider(nil, nil, nil, nil)
	value := ts.generate(p)

	outer, err := jose.ParseEncryptedCompact(value, []jose.KeyAlgorithm{jose.RSA_OAEP_256},
		[]jose.ContentEncryption{jose.A256GCM})
	ts.Require().NoError(err)
	ts.Equal("RSA-OAEP-256", outer.Header.Algorithm)
	ts.Equal("JWT", outer.Header.ExtraHeaders[jose.HeaderContentType])
	ts.NotEmpty(outer.Header.KeyID)

	plain, err := ts.keys.crypto.JWTDecrypt(context.Background(), cryptomanager.JWTDecryptRequest{
		ApplicationID: config.OIDCServiceAppID, ReferenceID: "DEVICE_SECRET", EncData: value,
	})
	ts.Require().NoError(err)
	inner, err := base64.RawURLEncoding.DecodeString(plain.Data)
	ts.Require().NoError(err)
	parts := strings.Split(string(inner), ".")
	ts.Require().Len(parts, 3)

	var header map[string]any
	ts.Require().NoError(decodeDeviceSecretSegment(parts[0], &header))
	ts.Equal("ES256", header["alg"])
	ts.Equal("ds+jwt", header["typ"])
	ts.NotEmpty(header["kid"])

	var claims map[string]any
	ts.Require().NoError(decodeDeviceSecretSegment(parts[1], &claims))
	for _, name := range []string{"iss", "client_id", "auth_id", "sid", "scope", "acr", "claims", "cnf", "iat",
		"exp", "jti"} {
		ts.Contains(claims, name)
	}
	ts.Equal(map[string]any{"jkt": "jkt-thumbprint"}, claims["cnf"])
	ts.Equal("openid profile", claims["scope"], "device_sso is implied and left out of the scope claim")
	ts.Equal([]any{"name", "email"}, claims["claims"], "claim names only")
	ts.InDelta(float64(dsTestValidity), claims["exp"].(float64)-claims["iat"].(float64), 0)
	for _, personal := range []string{"sub", "ou_id", "attribute_cache_id", "auth_time"} {
		ts.NotContains(claims, personal, "user data stays in the session record")
	}
}

func (ts *DeviceSecretProviderTestSuite) TestGenerateDeviceSecret_StoresSessionRecord() {
	p := ts.newProvider(nil, nil, nil, nil)
	ts.generate(p)

	raw, err := ts.store.Get(context.Background(), providers.NamespaceNativeSSOSession, "sid-1")
	ts.Require().NoError(err)
	var session nativeSSOSession
	ts.Require().NoError(json.Unmarshal(raw, &session))
	ts.NotEmpty(session.JTI, "the record names its device secret")
	ts.Equal(nativeSSOSession{
		JTI:    session.JTI,
		AuthID: "authz-req-1", ClientID: "hub-client", Subject: "psut-123", OUID: "ps03-vendor",
		AttributeCacheID: "aci-1", AuthTime: 1_760_000_000, ExpiresAt: ts.now.Unix() + dsTestValidity,
	}, session)
}

func (ts *DeviceSecretProviderTestSuite) TestGenerateDeviceSecret_Failures() {
	cases := []struct {
		name   string
		build  func() *deviceSecretProvider
		mutate func(*providers.DeviceSecretRequest)
	}{
		{name: "request without device_sso", build: func() *deviceSecretProvider {
			return ts.newProvider(nil, nil, nil, nil)
		}, mutate: func(r *providers.DeviceSecretRequest) { r.Scopes = []string{"openid"} }},
		{name: "request without DPoP thumbprint", build: func() *deviceSecretProvider {
			return ts.newProvider(nil, nil, nil, nil)
		}, mutate: func(r *providers.DeviceSecretRequest) { r.DPoPJkt = "" }},
		{name: "signing key unavailable", build: func() *deviceSecretProvider {
			return ts.newProvider(nil, &stubDeviceSecretSigner{DeviceSecretSigner: ts.keys.sig,
				resolveErr: errors.New("hsm down")}, nil, nil)
		}},
		{name: "signing key is not EC", build: func() *deviceSecretProvider {
			cfg := deviceSecretTestConfig()
			cfg.NativeSSO.SigningKeyRefID = keymanager.RefIDRSA2048
			cfg.JWT.PreferredKeyID = "OTHER"
			return ts.newProvider(cfg, nil, nil, nil)
		}},
		{name: "encryption fails", build: func() *deviceSecretProvider {
			return ts.newProvider(nil, nil, &stubDeviceSecretCipher{DeviceSecretCipher: ts.keys.crypto,
				encryptErr: errors.New("db down")}, nil)
		}},
		{name: "session store fails", build: func() *deviceSecretProvider {
			return ts.newProvider(nil, nil, nil, &stubRuntimeStore{RuntimeStoreProvider: ts.store,
				putErr: errors.New("redis down")})
		}},
	}
	for _, tc := range cases {
		ts.Run(tc.name, func() {
			ts.SetupTest()
			p := tc.build()
			req := deviceSecretTestRequest()
			if tc.mutate != nil {
				tc.mutate(&req)
			}
			ds, svcErr := p.GenerateDeviceSecret(context.Background(), req)
			ts.Nil(ds)
			ts.assertServerError(svcErr)
			raw, _ := ts.store.Get(context.Background(), providers.NamespaceNativeSSOSession, req.SessionID)
			ts.Nil(raw, "a failed generation leaves no session record")
		})
	}
}

func (ts *DeviceSecretProviderTestSuite) TestGenerateDeviceSecret_SessionIDInUse() {
	p := ts.newProvider(nil, nil, nil, nil)
	ts.generate(p)
	ds, svcErr := p.GenerateDeviceSecret(context.Background(), deviceSecretTestRequest())
	ts.Nil(ds)
	ts.assertServerError(svcErr)
}

func (ts *DeviceSecretProviderTestSuite) TestValidateDeviceSecret_Empty() {
	p := ts.newProvider(nil, nil, nil, nil)
	_, svcErr := p.ValidateDeviceSecret(context.Background(), "  ")
	ts.assertInvalid(svcErr)
}

func (ts *DeviceSecretProviderTestSuite) TestValidateDeviceSecret_IDTokenRejected() {
	p := ts.newProvider(nil, nil, nil, nil)
	ts.putSession("sid-1", nativeSSOSession{JTI: "jti-1", AuthID: "authz-req-1", ClientID: "hub-client",
		ExpiresAt: ts.now.Unix() + 60})

	ts.Run("plain signed id_token", func() {
		// An id_token is a JWS, not a JWE, so it never reaches the signature check.
		idToken := strings.Join([]string{
			base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"PS256","typ":"JWT"}`)),
			base64.RawURLEncoding.EncodeToString([]byte(`{"iss":"https://esignet.example.com","sub":"x"}`)),
			base64.RawURLEncoding.EncodeToString([]byte("signature")),
		}, ".")
		_, svcErr := p.ValidateDeviceSecret(context.Background(), idToken)
		ts.assertInvalid(svcErr)
	})
	ts.Run("id_token signed by the token key and encrypted to the device secret key", func() {
		header := map[string]any{"alg": "PS256", "typ": "JWT"}
		_, svcErr := p.ValidateDeviceSecret(context.Background(), ts.sealInner(ts.keys, header, ts.validClaims()))
		ts.assertInvalid(svcErr)
	})
}

func (ts *DeviceSecretProviderTestSuite) TestValidateDeviceSecret_WrongTypeOrAlgorithm() {
	p := ts.newProvider(nil, nil, nil, nil)
	ts.putSession("sid-1", nativeSSOSession{JTI: "jti-1", AuthID: "authz-req-1", ClientID: "hub-client",
		ExpiresAt: ts.now.Unix() + 60})
	for name, header := range map[string]map[string]any{
		"typ JWT":    {"alg": "ES256", "typ": "JWT"},
		"typ absent": {"alg": "ES256"},
		"alg PS256":  {"alg": "PS256", "typ": "ds+jwt"},
	} {
		ts.Run(name, func() {
			_, svcErr := p.ValidateDeviceSecret(context.Background(), ts.sealInner(ts.keys, header, ts.validClaims()))
			ts.assertInvalid(svcErr)
		})
	}
	ts.Run("control: the same claims with the right header pass", func() {
		_, svcErr := p.ValidateDeviceSecret(context.Background(), ts.sealInner(ts.keys, ts.validHeader(),
			ts.validClaims()))
		ts.Nil(svcErr)
	})
}

func (ts *DeviceSecretProviderTestSuite) TestValidateDeviceSecret_TamperedCiphertext() {
	p := ts.newProvider(nil, nil, nil, nil)
	parts := strings.Split(ts.generate(p), ".")
	ct := []byte(parts[3])
	if ct[0] == 'A' {
		ct[0] = 'B'
	} else {
		ct[0] = 'A'
	}
	parts[3] = string(ct)
	_, svcErr := p.ValidateDeviceSecret(context.Background(), strings.Join(parts, "."))
	ts.assertInvalid(svcErr)
}

func (ts *DeviceSecretProviderTestSuite) TestValidateDeviceSecret_WrongKey() {
	other := newDeviceSecretKeys(ts.T())
	p := ts.newProvider(nil, nil, nil, nil)

	ts.Run("encrypted to another server's key", func() {
		foreign := ts.newProvider(nil, other.sig, other.crypto, inmemory.Initialize("other"))
		_, svcErr := p.ValidateDeviceSecret(context.Background(), ts.generate(foreign))
		ts.assertInvalid(svcErr)
	})
	ts.Run("signed by another server's key", func() {
		ts.putSession("sid-1", nativeSSOSession{JTI: "jti-1", AuthID: "authz-req-1", ClientID: "hub-client",
			ExpiresAt: ts.now.Unix() + 60})
		// Signed with the other key set, encrypted to ours: decryption works, the signature does not.
		ctx := context.Background()
		h, _ := json.Marshal(ts.validHeader())
		c, _ := json.Marshal(ts.validClaims())
		input := base64.RawURLEncoding.EncodeToString(h) + "." + base64.RawURLEncoding.EncodeToString(c)
		cert, key, err := other.sig.ResolveSigner(ctx, config.OIDCServiceAppID, keymanager.RefIDECSECP256R1Sign)
		ts.Require().NoError(err)
		sig, err := other.sig.SignWithKey(key, cert.PublicKey, "ES256", []byte(input))
		ts.Require().NoError(err)
		forged := ts.seal(input + "." + base64.RawURLEncoding.EncodeToString(sig))
		_, svcErr := p.ValidateDeviceSecret(ctx, forged)
		ts.assertInvalid(svcErr)
	})
}

func (ts *DeviceSecretProviderTestSuite) TestValidateDeviceSecret_Expired() {
	p := ts.newProvider(nil, nil, nil, nil)
	value := ts.generate(p)

	ts.now = ts.now.Add(time.Duration(dsTestValidity+dsTestLeeway) * time.Second)
	_, svcErr := p.ValidateDeviceSecret(context.Background(), value)
	ts.assertInvalid(svcErr)
}

func (ts *DeviceSecretProviderTestSuite) TestValidateDeviceSecret_IssuedInTheFuture() {
	p := ts.newProvider(nil, nil, nil, nil)
	claims := ts.validClaims()
	claims["iat"] = ts.now.Unix() + dsTestLeeway + 5
	_, svcErr := p.ValidateDeviceSecret(context.Background(), ts.sealInner(ts.keys, ts.validHeader(), claims))
	ts.assertInvalid(svcErr)
}

func (ts *DeviceSecretProviderTestSuite) TestValidateDeviceSecret_WrongIssuer() {
	cfg := deviceSecretTestConfig()
	cfg.Issuer = "https://other.example.com"
	foreign := ts.newProvider(cfg, nil, nil, nil)
	value := ts.generate(foreign)

	_, svcErr := ts.newProvider(nil, nil, nil, nil).ValidateDeviceSecret(context.Background(), value)
	ts.assertInvalid(svcErr)
}

func (ts *DeviceSecretProviderTestSuite) TestValidateDeviceSecret_MissingClaims() {
	p := ts.newProvider(nil, nil, nil, nil)
	for _, name := range []string{"client_id", "auth_id", "sid", "cnf"} {
		ts.Run(name, func() {
			claims := ts.validClaims()
			delete(claims, name)
			_, svcErr := p.ValidateDeviceSecret(context.Background(), ts.sealInner(ts.keys, ts.validHeader(), claims))
			ts.assertInvalid(svcErr)
		})
	}
}

func (ts *DeviceSecretProviderTestSuite) TestValidateDeviceSecret_MalformedInnerToken() {
	p := ts.newProvider(nil, nil, nil, nil)
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"ES256","typ":"ds+jwt"}`))
	claims := base64.RawURLEncoding.EncodeToString([]byte(`{"iss":"x"}`))
	cases := map[string]string{
		"not a JWS":         base64.RawURLEncoding.EncodeToString([]byte(`{"iss":"x"}`)),
		"header not base64": "!!." + claims + ".c2ln",
		"header not JSON":   base64.RawURLEncoding.EncodeToString([]byte("nope")) + "." + claims + ".c2ln",
		"signature garbled": header + "." + claims + ".!!",
		"bad signature":     header + "." + claims + "." + base64.RawURLEncoding.EncodeToString(make([]byte, 64)),
	}
	for name, inner := range cases {
		ts.Run(name, func() {
			_, svcErr := p.ValidateDeviceSecret(context.Background(), ts.seal(inner))
			ts.assertInvalid(svcErr)
		})
	}
	ts.Run("claims not JSON", func() {
		// A valid signature over a payload that is not JSON.
		ctx := context.Background()
		input := header + "." + base64.RawURLEncoding.EncodeToString([]byte("not json"))
		cert, key, err := ts.keys.sig.ResolveSigner(ctx, config.OIDCServiceAppID, keymanager.RefIDECSECP256R1Sign)
		ts.Require().NoError(err)
		sig, err := ts.keys.sig.SignWithKey(key, cert.PublicKey, "ES256", []byte(input))
		ts.Require().NoError(err)
		_, svcErr := p.ValidateDeviceSecret(ctx, ts.seal(input+"."+base64.RawURLEncoding.EncodeToString(sig)))
		ts.assertInvalid(svcErr)
	})
}

func (ts *DeviceSecretProviderTestSuite) TestValidateDeviceSecret_UnknownOrDeletedSession() {
	ts.Run("deleted session", func() {
		p := ts.newProvider(nil, nil, nil, nil)
		value := ts.generate(p)
		ts.Require().NoError(ts.store.Delete(context.Background(), providers.NamespaceNativeSSOSession, "sid-1"))
		_, svcErr := p.ValidateDeviceSecret(context.Background(), value)
		ts.assertSessionEnded(svcErr)
	})
	ts.Run("unknown session (store restarted)", func() {
		value := ts.generate(ts.newProvider(nil, nil, nil, nil))
		restarted := ts.newProvider(nil, nil, nil, inmemory.Initialize("restarted"))
		_, svcErr := restarted.ValidateDeviceSecret(context.Background(), value)
		ts.assertSessionEnded(svcErr)
	})
	ts.Run("session record from before records named their jti", func() {
		p := ts.newProvider(nil, nil, nil, nil)
		ts.putSession("sid-1", nativeSSOSession{AuthID: "authz-req-1", ClientID: "hub-client",
			ExpiresAt: ts.now.Unix() + 60})
		_, svcErr := p.ValidateDeviceSecret(context.Background(), ts.sealInner(ts.keys, ts.validHeader(),
			ts.validClaims()))
		ts.assertSessionEnded(svcErr)
	})
	ts.Run("session record past its end", func() {
		p := ts.newProvider(nil, nil, nil, nil)
		ts.putSession("sid-1", nativeSSOSession{JTI: "jti-1", AuthID: "authz-req-1", ClientID: "hub-client",
			ExpiresAt: ts.now.Unix()})
		_, svcErr := p.ValidateDeviceSecret(context.Background(), ts.sealInner(ts.keys, ts.validHeader(),
			ts.validClaims()))
		ts.assertSessionEnded(svcErr)
	})
}

func (ts *DeviceSecretProviderTestSuite) TestValidateDeviceSecret_SessionMismatch() {
	p := ts.newProvider(nil, nil, nil, nil)
	for name, session := range map[string]nativeSSOSession{
		"other login": {JTI: "jti-1", AuthID: "authz-req-2", ClientID: "hub-client", ExpiresAt: ts.now.Unix() + 60},
		"other client": {JTI: "jti-1", AuthID: "authz-req-1", ClientID: "child-client",
			ExpiresAt: ts.now.Unix() + 60},
		// The record names one device secret: another jti under the same sid is not that device secret.
		"other jti": {JTI: "jti-2", AuthID: "authz-req-1", ClientID: "hub-client", ExpiresAt: ts.now.Unix() + 60},
	} {
		ts.Run(name, func() {
			ts.putSession("sid-1", session)
			_, svcErr := p.ValidateDeviceSecret(context.Background(), ts.sealInner(ts.keys, ts.validHeader(),
				ts.validClaims()))
			ts.assertInvalid(svcErr)
		})
	}
}

func (ts *DeviceSecretProviderTestSuite) TestValidateDeviceSecret_ServerFailures() {
	value := ts.generate(ts.newProvider(nil, nil, nil, nil))
	cases := map[string]*deviceSecretProvider{
		"key store down while decrypting": ts.newProvider(nil, nil, &stubDeviceSecretCipher{
			DeviceSecretCipher: ts.keys.crypto, decryptErr: errors.New("db down")}, nil),
		"decrypted data not base64url": ts.newProvider(nil, nil, &stubDeviceSecretCipher{
			DeviceSecretCipher: ts.keys.crypto, decryptData: "***"}, nil),
		"verification key unavailable": ts.newProvider(nil, &stubDeviceSecretSigner{
			DeviceSecretSigner: ts.keys.sig, verifyErr: signature.ErrVerifyCertificateNotFound}, nil, nil),
		"session store down": ts.newProvider(nil, nil, nil, &stubRuntimeStore{
			RuntimeStoreProvider: ts.store, getErr: errors.New("redis down")}),
	}
	for name, p := range cases {
		ts.Run(name, func() {
			_, svcErr := p.ValidateDeviceSecret(context.Background(), value)
			ts.assertServerError(svcErr)
		})
	}
	ts.Run("session record corrupt", func() {
		ts.Require().NoError(ts.store.Put(context.Background(), providers.NamespaceNativeSSOSession, "sid-1",
			[]byte("{"), 600))
		_, svcErr := ts.newProvider(nil, nil, nil, nil).ValidateDeviceSecret(context.Background(), value)
		ts.assertServerError(svcErr)
	})
}

func (ts *DeviceSecretProviderTestSuite) TestValidateDeviceSecret_StoreReportsKeyNotFound() {
	value := ts.generate(ts.newProvider(nil, nil, nil, nil))
	p := ts.newProvider(nil, nil, nil, &stubRuntimeStore{RuntimeStoreProvider: ts.store,
		getErr: providers.ErrRuntimeStoreKeyNotFound})
	_, svcErr := p.ValidateDeviceSecret(context.Background(), value)
	ts.assertSessionEnded(svcErr)
}

// Rule R27: revoking a device secret ends its session, so the same value is refused afterwards.
func (ts *DeviceSecretProviderTestSuite) TestEndDeviceSecretSession_RevokesDeviceSecret() {
	p := ts.newProvider(nil, nil, nil, nil)
	value := ts.generate(p)
	claims, svcErr := p.ValidateDeviceSecret(context.Background(), value)
	ts.Require().Nil(svcErr)

	ts.Nil(p.EndDeviceSecretSession(context.Background(), claims))

	_, svcErr = p.ValidateDeviceSecret(context.Background(), value)
	ts.assertSessionEnded(svcErr)
	ts.Nil(p.EndDeviceSecretSession(context.Background(), claims), "ending an ended session is not an error")
}

func (ts *DeviceSecretProviderTestSuite) TestEndDeviceSecretSession_Failures() {
	ts.Run("no claims", func() {
		ts.assertServerError(ts.newProvider(nil, nil, nil, nil).EndDeviceSecretSession(context.Background(), nil))
	})
	ts.Run("no session id", func() {
		ts.assertServerError(ts.newProvider(nil, nil, nil, nil).EndDeviceSecretSession(context.Background(),
			&providers.DeviceSecretClaims{}))
	})
	ts.Run("store reports key not found", func() {
		p := ts.newProvider(nil, nil, nil, &stubRuntimeStore{RuntimeStoreProvider: ts.store,
			deleteErr: providers.ErrRuntimeStoreKeyNotFound})
		ts.Nil(p.EndDeviceSecretSession(context.Background(), &providers.DeviceSecretClaims{
			DeviceSecretRequest: providers.DeviceSecretRequest{SessionID: "sid-1"}}))
	})
	ts.Run("store down", func() {
		p := ts.newProvider(nil, nil, nil, &stubRuntimeStore{RuntimeStoreProvider: ts.store,
			deleteErr: errors.New("redis down")})
		ts.assertServerError(p.EndDeviceSecretSession(context.Background(), &providers.DeviceSecretClaims{
			DeviceSecretRequest: providers.DeviceSecretRequest{SessionID: "sid-1"}}))
	})
}

// Rule R27: the access tokens of a session are refused once the session ends, so the provider must
// report a revoked or expired session as inactive.
func (ts *DeviceSecretProviderTestSuite) TestIsDeviceSecretSessionActive() {
	ctx := context.Background()
	p := ts.newProvider(nil, nil, nil, nil)
	value := ts.generate(p)

	active, svcErr := p.IsDeviceSecretSessionActive(ctx, "sid-1")
	ts.Require().Nil(svcErr)
	ts.True(active, "a fresh login session is active")

	claims, svcErr := p.ValidateDeviceSecret(ctx, value)
	ts.Require().Nil(svcErr)
	ts.Require().Nil(p.EndDeviceSecretSession(ctx, claims))
	active, svcErr = p.IsDeviceSecretSessionActive(ctx, "sid-1")
	ts.Nil(svcErr)
	ts.False(active, "a revoked session is not active")

	for _, sid := range []string{"", " ", "sid-unknown"} {
		active, svcErr = p.IsDeviceSecretSessionActive(ctx, sid)
		ts.Nil(svcErr)
		ts.False(active, "session %q", sid)
	}

	ts.putSession("sid-old", nativeSSOSession{AuthID: "a", ClientID: "c", ExpiresAt: ts.now.Unix()})
	active, svcErr = p.IsDeviceSecretSessionActive(ctx, "sid-old")
	ts.Nil(svcErr)
	ts.False(active, "a record past its expiry is not active")
}

func (ts *DeviceSecretProviderTestSuite) TestIsDeviceSecretSessionActive_Failures() {
	ctx := context.Background()
	ts.Run("store down", func() {
		p := ts.newProvider(nil, nil, nil, &stubRuntimeStore{RuntimeStoreProvider: ts.store,
			getErr: errors.New("redis down")})
		active, svcErr := p.IsDeviceSecretSessionActive(ctx, "sid-1")
		ts.False(active)
		ts.assertServerError(svcErr)
	})
	ts.Run("corrupt record", func() {
		ts.Require().NoError(ts.store.Put(ctx, providers.NamespaceNativeSSOSession, "sid-bad", []byte("{"), 600))
		active, svcErr := ts.newProvider(nil, nil, nil, nil).IsDeviceSecretSessionActive(ctx, "sid-bad")
		ts.False(active)
		ts.assertServerError(svcErr)
	})
}

// Statement diagram 2: a silent consent reuse appends one consent_history row for the second app, with the
// issued scopes, the released claims and the device secret's expiry.
func (ts *DeviceSecretProviderTestSuite) TestRecordConsentReuse_AppendsHistoryRow() {
	p := ts.newProvider(nil, nil, nil, nil)
	device := &providers.DeviceSecretClaims{
		DeviceSecretRequest: providers.DeviceSecretRequest{ClientID: "hub-client", Subject: "psut-123"},
		ExpiresAt:           ts.now.Unix() + 600,
	}
	svcErr := p.RecordConsentReuse(context.Background(), providers.ConsentReuse{
		ClientID: "child-client", Device: device, Scopes: []string{"openid", "email"}, Claims: []string{"email"},
	})
	ts.Require().Nil(svcErr)
	ts.Require().Len(ts.history.rows, 1)
	row := ts.history.rows[0]
	ts.Equal("child-client", row.ClientID, "the row is for the app that received the tokens")
	ts.Equal("psut-123", row.UserID)
	ts.Equal([]string{"openid", "email"}, row.AuthorizationScopes)
	ts.Equal([]string{"openid", "email"}, row.PermittedScopes)
	ts.Equal([]string{"email"}, row.AcceptedClaims)
	ts.Equal(map[string]any{"userinfo": map[string]any{"email": nil}}, row.Claims)
	ts.Equal(ts.now.Unix(), row.CreatedAt.Unix())
	ts.True(row.ExpiresAt.Valid)
	ts.Equal(device.ExpiresAt, row.ExpiresAt.Time.Unix())
}

func (ts *DeviceSecretProviderTestSuite) TestRecordConsentReuse_Failures() {
	device := &providers.DeviceSecretClaims{DeviceSecretRequest: providers.DeviceSecretRequest{Subject: "psut-123"}}
	ts.Run("history write fails", func() {
		ts.history.err = errors.New("db down")
		defer func() { ts.history.err = nil }()
		svcErr := ts.newProvider(nil, nil, nil, nil).RecordConsentReuse(context.Background(),
			providers.ConsentReuse{ClientID: "child-client", Device: device})
		ts.assertServerError(svcErr)
	})
	ts.Run("no device secret", func() {
		svcErr := ts.newProvider(nil, nil, nil, nil).RecordConsentReuse(context.Background(),
			providers.ConsentReuse{ClientID: "child-client"})
		ts.assertServerError(svcErr)
	})
	ts.Empty(ts.history.rows)
}
