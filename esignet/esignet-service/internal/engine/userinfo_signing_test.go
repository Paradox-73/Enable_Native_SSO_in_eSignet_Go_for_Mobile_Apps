/*
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */

package engine

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	jose "github.com/go-jose/go-jose/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thunder-id/thunderid/pkg/thunderidengine/providers"

	"github.com/mosip/esignet/internal/config"
	"github.com/mosip/esignet/internal/engine/shared"
	"github.com/mosip/esignet/internal/httpmiddleware"
	"github.com/mosip/esignet/internal/keymanager"
	"github.com/mosip/esignet/internal/keymanager/signature"
)

// The deployment signs with the RSA_2048 key, which /oauth2/jwks publishes with alg PS256 (the
// JWKS is built from GetPublicKeys: kid = KeyID, alg = Algorithm). The mock identity system
// signs its userinfo token with RS256. The userinfo answer must be signed with a key of the
// JWKS, with exactly the alg that JWKS entry declares, so a FAPI 2.0 client that requires both
// to match accepts it.
func TestUserinfo_SignedWithTheAlgAndKidOfTheJWKS(t *testing.T) {
	ctx := context.Background()
	km := newTestKeyManagerService(t)
	_, err := km.GenerateMasterKey(ctx, keymanager.GenerateMasterKeyRequest{
		ApplicationID: config.OIDCServiceAppID,
		ReferenceID:   keymanager.RefIDRSA2048,
		ObjectType:    keymanager.ObjectTypeCertificate,
		CommonName:    "test esignet RSA sign key",
	})
	require.NoError(t, err)

	idKey, idCertPEM := rsaKeyAndCertPEM(t, "mock identity system")
	p := &runtimeCryptoProvider{
		svc: km, sigSvc: signature.NewService(km), signReferenceID: keymanager.RefIDRSA2048,
		authnProvider: &fakeAuthnProviderForCrypto{
			certs: []shared.CertificateData{{KeyID: "O10N-id-system", Certificate: idCertPEM}},
		},
	}
	jwks, err := p.GetPublicKeys(ctx, providers.PublicKeyFilter{})
	require.NoError(t, err)
	require.Len(t, jwks, 2)
	require.Equal(t, "PS256", jwks[0].Algorithm, "the engine's RSA key is published as PS256")
	require.Equal(t, "PS256", jwks[1].Algorithm, "the identity system's RSA key is published as PS256")

	kyc := signRS256(t, idKey, "O10N-id-system", map[string]any{
		"iss": "http://mock-identity-system", "sub": "psut-1", "aud": "hub", "name": "A", "email": "a@b.c",
	})
	mw := httpmiddleware.UserinfoScope(httpmiddleware.UserinfoScopeConfig{
		ScopeClaims:  map[string][]string{"email": {"email"}},
		SigningKeyID: keymanager.RefIDRSA2048,
		Signer:       p,
	})
	engineAnswer := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/jwt")
		_, _ = w.Write([]byte(kyc))
	})

	for _, grant := range []string{"authorization_code", string(providers.GrantTypeTokenExchange)} {
		t.Run(grant, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, httpmiddleware.UserinfoPath, nil)
			req.Header.Set("Authorization", "DPoP "+accessTokenFor(t, grant))
			rr := httptest.NewRecorder()
			mw(engineAnswer).ServeHTTP(rr, req)
			require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())

			jws, err := jose.ParseSigned(rr.Body.String(), []jose.SignatureAlgorithm{jose.PS256, jose.RS256, jose.ES256})
			require.NoError(t, err)
			header := jws.Signatures[0].Header
			var entry *providers.PublicKeyInfo
			for i := range jwks {
				if jwks[i].KeyID == header.KeyID {
					entry = &jwks[i]
				}
			}
			require.NotNil(t, entry, "kid %q is not in the JWKS", header.KeyID)
			assert.Equal(t, entry.Algorithm, header.Algorithm, "alg must equal the JWKS entry's alg")
			assert.Equal(t, "PS256", header.Algorithm)
			assert.Equal(t, jwks[0].KeyID, header.KeyID, "signed with the engine's key")
			_, err = jws.Verify(entry.PublicKey)
			require.NoError(t, err)
		})
	}
}

func rsaKeyAndCertPEM(t *testing.T, cn string) (*rsa.PrivateKey, string) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()), Subject: pkix.Name{CommonName: cn},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature, BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	require.NoError(t, err)
	return key, keymanager.EncodeCertPEM(der)
}

func signRS256(t *testing.T, key *rsa.PrivateKey, kid string, claims map[string]any) string {
	t.Helper()
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: key},
		(&jose.SignerOptions{}).WithHeader("kid", kid))
	require.NoError(t, err)
	payload, err := json.Marshal(claims)
	require.NoError(t, err)
	jws, err := signer.Sign(payload)
	require.NoError(t, err)
	compact, err := jws.CompactSerialize()
	require.NoError(t, err)
	return compact
}

// accessTokenFor is an access token with a dummy signature; the middleware reads its claims only.
func accessTokenFor(t *testing.T, grant string) string {
	t.Helper()
	enc := func(v any) string {
		b, err := json.Marshal(v)
		require.NoError(t, err)
		return base64.RawURLEncoding.EncodeToString(b)
	}
	return strings.Join([]string{
		enc(map[string]string{"alg": "PS256"}),
		enc(map[string]string{"grant_type": grant, "scope": "openid email", "client_id": "health-card"}),
		"c2ln",
	}, ".")
}
