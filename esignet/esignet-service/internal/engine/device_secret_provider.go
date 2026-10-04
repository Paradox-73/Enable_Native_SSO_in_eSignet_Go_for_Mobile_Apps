/*
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */

package engine

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/thunder-id/thunderid/pkg/thunderidengine/common"
	"github.com/thunder-id/thunderid/pkg/thunderidengine/providers"

	"github.com/mosip/esignet/internal/config"
	"github.com/mosip/esignet/internal/engine/shared"
	"github.com/mosip/esignet/internal/keymanager"
	"github.com/mosip/esignet/internal/keymanager/cryptomanager"
	"github.com/mosip/esignet/internal/keymanager/signature"
	applog "github.com/mosip/esignet/internal/log"
)

const (
	// deviceSecretJWSType is the typ header of the inner device secret JWS. An explicit type
	// keeps an id_token or access token signed by this server from being accepted as a device
	// secret, and the other way round (RFC 8725, 3.11).
	deviceSecretJWSType = "ds+jwt"

	// deviceSecretJWSAlgorithm is the only signature algorithm of the inner JWS. The engine's
	// tokens are signed with the RSA key (PS256), so the two never share a key.
	deviceSecretJWSAlgorithm = "ES256"
)

// DeviceSecretSigner signs and verifies the inner device secret JWS. *signature.Service
// implements it.
type DeviceSecretSigner interface {
	ResolveSigner(ctx context.Context, appID, refID string) (*x509.Certificate, crypto.Signer, error)
	SignWithKey(signer crypto.Signer, pub crypto.PublicKey, alg string, signingInput []byte) ([]byte, error)
	VerifyRaw(ctx context.Context, appID, refID, alg string, signingInput, sig []byte) error
}

// DeviceSecretCipher encrypts the signed device secret into a JWE and decrypts it again.
// *cryptomanager.Service implements it.
type DeviceSecretCipher interface {
	JWTEncrypt(ctx context.Context, req cryptomanager.JWTEncryptRequest) (cryptomanager.JWTCipherResponse, error)
	JWTDecrypt(ctx context.Context, req cryptomanager.JWTDecryptRequest) (cryptomanager.JWTCipherResponse, error)
}

// deviceSecretHeader is the protected header of the inner device secret JWS.
type deviceSecretHeader struct {
	Alg string `json:"alg"`
	Typ string `json:"typ"`
	Kid string `json:"kid,omitempty"`
}

// deviceSecretConfirmation is the cnf claim (RFC 7800) binding the device secret to the DPoP key
// of the app it was issued to (RFC 9449, 6.1).
type deviceSecretConfirmation struct {
	JKT string `json:"jkt"`
}

// deviceSecretClaims are the claims of the inner device secret JWS. They carry no user
// identifier and no claim values: those stay in the session record, so a leaked device secret
// reveals nothing about the user and stops working once the record is gone.
type deviceSecretClaims struct {
	Issuer    string                   `json:"iss"`
	ClientID  string                   `json:"client_id"`
	AuthID    string                   `json:"auth_id"`
	SessionID string                   `json:"sid"`
	Scope     string                   `json:"scope"`
	ACR       string                   `json:"acr,omitempty"`
	Claims    []string                 `json:"claims,omitempty"`
	CNF       deviceSecretConfirmation `json:"cnf"`
	IssuedAt  int64                    `json:"iat"`
	ExpiresAt int64                    `json:"exp"`
	JTI       string                   `json:"jti"`
}

// nativeSSOSession is the login-session record of a device secret, stored in the runtime store
// under providers.NamespaceNativeSSOSession with the session id (sid) as key. Its TTL equals the
// device secret lifetime; once it is gone the device secret is refused (Native SSO 4.3, step 4).
type nativeSSOSession struct {
	AuthID           string `json:"auth_id"`
	ClientID         string `json:"client_id"`
	Subject          string `json:"sub"`
	OUID             string `json:"ou_id,omitempty"`
	AttributeCacheID string `json:"attribute_cache_id,omitempty"`
	AuthTime         int64  `json:"auth_time,omitempty"`
	ExpiresAt        int64  `json:"expires_at"`
}

type deviceSecretProvider struct {
	issuer          string
	validitySeconds int64
	leewaySeconds   int64
	encryptionRefID string
	signingRefID    string
	signer          DeviceSecretSigner
	cipher          DeviceSecretCipher
	store           providers.RuntimeStoreProvider
	now             func() time.Time
}

// NewDeviceSecretProvider returns the providers.DeviceSecretProvider of OpenID Connect Native SSO
// for Mobile Apps 1.0. A device secret is a nested JWT: claims signed with ES256 by the
// native_sso.signing_key_ref_id key (typ ds+jwt), then encrypted with RSA-OAEP-256 and A256GCM to
// the native_sso.encryption_key_ref_id Component Encryption Key, both under OIDCServiceAppID.
// Each device secret has a session record in runtimeStore that ends with the device secret.
// clock may be nil, which means time.Now.
//
// Wired into thunderidengine.New via thunderidengine.WithDeviceSecretProvider (see
// cmd/esignet/main.go); an engine without it has Native SSO turned off.
func NewDeviceSecretProvider(cfg *config.AppConfig, signatureSvc DeviceSecretSigner, cryptoSvc DeviceSecretCipher,
	runtimeStore providers.RuntimeStoreProvider, clock func() time.Time) (providers.DeviceSecretProvider, error) {
	if err := cfg.NativeSSO.Validate(cfg.JWT.PreferredKeyID); err != nil {
		return nil, err
	}
	if signatureSvc == nil || cryptoSvc == nil || runtimeStore == nil {
		return nil, errors.New("device secret provider needs the signature, crypto and runtime store services")
	}
	if clock == nil {
		clock = time.Now
	}
	return &deviceSecretProvider{
		issuer:          cfg.Issuer,
		validitySeconds: cfg.NativeSSO.DeviceSecretValiditySeconds,
		leewaySeconds:   max(cfg.JWT.Leeway, 0),
		encryptionRefID: cfg.NativeSSO.EncryptionKeyRefID,
		signingRefID:    cfg.NativeSSO.SigningKeyRefID,
		signer:          signatureSvc,
		cipher:          cryptoSvc,
		store:           runtimeStore,
		now:             clock,
	}, nil
}

// GenerateDeviceSecret signs, encrypts and records a new device secret for an authorization-code
// grant that was granted device_sso. Every failure is a server error: the engine then issues no
// tokens at all, because the id_token and the device secret are bound to each other (Native SSO
// 3.4).
func (p *deviceSecretProvider) GenerateDeviceSecret(ctx context.Context,
	req providers.DeviceSecretRequest) (*providers.DeviceSecret, *common.ServiceError) {
	logger := applog.GetLogger()
	if err := req.Validate(); err != nil {
		logger.Error(ctx, "device secret request rejected", applog.String("clientId", req.ClientID),
			applog.Error(err))
		return nil, shared.InternalServerError
	}

	issuedAt := p.now().Unix()
	expiresAt := issuedAt + p.validitySeconds
	claims := deviceSecretClaims{
		Issuer:    p.issuer,
		ClientID:  req.ClientID,
		AuthID:    req.AuthID,
		SessionID: req.SessionID,
		Scope:     strings.Join(withoutDeviceSSO(req.Scopes), " "),
		ACR:       req.ACR,
		Claims:    req.Claims,
		CNF:       deviceSecretConfirmation{JKT: req.DPoPJkt},
		IssuedAt:  issuedAt,
		ExpiresAt: expiresAt,
		JTI:       rand.Text(),
	}

	signed, err := p.sign(ctx, claims)
	if err != nil {
		logger.Error(ctx, "device secret signing failed", applog.String("clientId", req.ClientID),
			applog.Error(err))
		return nil, shared.InternalServerError
	}
	encrypted, err := p.cipher.JWTEncrypt(ctx, cryptomanager.JWTEncryptRequest{
		ApplicationID: config.OIDCServiceAppID,
		ReferenceID:   p.encryptionRefID,
		Data:          signed,
	})
	if err != nil {
		logger.Error(ctx, "device secret encryption failed", applog.String("clientId", req.ClientID),
			applog.Error(err))
		return nil, shared.InternalServerError
	}

	// The record is written last, so that a signing or encryption failure leaves no session behind.
	record, err := json.Marshal(nativeSSOSession{
		AuthID:           req.AuthID,
		ClientID:         req.ClientID,
		Subject:          req.Subject,
		OUID:             req.OUID,
		AttributeCacheID: req.AttributeCacheID,
		AuthTime:         req.AuthTime,
		ExpiresAt:        expiresAt,
	})
	if err != nil {
		logger.Error(ctx, "device secret session encoding failed", applog.Error(err))
		return nil, shared.InternalServerError
	}
	stored, err := p.store.PutIfNotExists(ctx, providers.NamespaceNativeSSOSession, req.SessionID, record,
		p.validitySeconds)
	if err != nil {
		logger.Error(ctx, "device secret session store failed", applog.String("clientId", req.ClientID),
			applog.Error(err))
		return nil, shared.InternalServerError
	}
	if !stored {
		// A session id is 128 random bits minted per login; a live record under the same id means
		// the caller reused one, and binding a second login to it would merge two sessions.
		logger.Error(ctx, "device secret session id already in use", applog.String("clientId", req.ClientID))
		return nil, shared.InternalServerError
	}

	return &providers.DeviceSecret{Value: encrypted.Data, ExpiresAt: expiresAt}, nil
}

// ValidateDeviceSecret decrypts and verifies a device secret presented as an actor_token, checks
// its type, issuer and lifetime, and then its session record. A value that fails any check
// before the session lookup is ErrorInvalidDeviceSecret; a genuine value whose session record is
// gone is ErrorDeviceSecretSessionEnded.
func (p *deviceSecretProvider) ValidateDeviceSecret(ctx context.Context,
	deviceSecret string) (*providers.DeviceSecretClaims, *common.ServiceError) {
	logger := applog.GetLogger()
	if strings.TrimSpace(deviceSecret) == "" {
		return nil, invalidDeviceSecret()
	}

	decrypted, err := p.cipher.JWTDecrypt(ctx, cryptomanager.JWTDecryptRequest{
		ApplicationID: config.OIDCServiceAppID,
		ReferenceID:   p.encryptionRefID,
		EncData:       deviceSecret,
	})
	if err != nil {
		if isDeviceSecretDecryptRejection(err) {
			logger.Debug(ctx, "device secret rejected: not decryptable", applog.Error(err))
			return nil, invalidDeviceSecret()
		}
		logger.Error(ctx, "device secret decryption failed", applog.Error(err))
		return nil, shared.InternalServerError
	}
	signed, err := base64.RawURLEncoding.DecodeString(decrypted.Data)
	if err != nil {
		logger.Error(ctx, "device secret plaintext is not base64url", applog.Error(err))
		return nil, shared.InternalServerError
	}

	claims, reason, svcErr := p.verify(ctx, string(signed))
	if svcErr != nil {
		return nil, svcErr
	}
	if reason != "" {
		logger.Debug(ctx, "device secret rejected", applog.String("reason", reason))
		return nil, invalidDeviceSecret()
	}

	session, svcErr := p.loadSession(ctx, claims)
	if svcErr != nil {
		return nil, svcErr
	}

	return &providers.DeviceSecretClaims{
		DeviceSecretRequest: providers.DeviceSecretRequest{
			ClientID:  claims.ClientID,
			OUID:      session.OUID,
			Subject:   session.Subject,
			AuthID:    claims.AuthID,
			SessionID: claims.SessionID,
			// The scope claim leaves device_sso out (it is implied by every device secret), so it
			// is added back: the claims carry the same values the device secret was issued for.
			Scopes:           append(strings.Fields(claims.Scope), providers.ScopeDeviceSSO),
			ACR:              claims.ACR,
			Claims:           claims.Claims,
			AuthTime:         session.AuthTime,
			DPoPJkt:          claims.CNF.JKT,
			AttributeCacheID: session.AttributeCacheID,
		},
		Issuer:    claims.Issuer,
		IssuedAt:  claims.IssuedAt,
		ExpiresAt: claims.ExpiresAt,
	}, nil
}

// sign builds the inner JWS (compact serialization) over claims with the device secret signing
// key. The certificate and the key come from one ResolveSigner call, so the kid always names the
// key that produced the signature, even across a key rotation.
func (p *deviceSecretProvider) sign(ctx context.Context, claims deviceSecretClaims) (string, error) {
	cert, key, err := p.signer.ResolveSigner(ctx, config.OIDCServiceAppID, p.signingRefID)
	if err != nil {
		return "", fmt.Errorf("resolve device secret signing key: %w", err)
	}
	header, err := json.Marshal(deviceSecretHeader{
		Alg: deviceSecretJWSAlgorithm,
		Typ: deviceSecretJWSType,
		Kid: keymanager.ThumbprintForCert(cert),
	})
	if err != nil {
		return "", fmt.Errorf("encode device secret header: %w", err)
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", fmt.Errorf("encode device secret claims: %w", err)
	}
	signingInput := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(payload)
	sig, err := p.signer.SignWithKey(key, cert.PublicKey, deviceSecretJWSAlgorithm, []byte(signingInput))
	if err != nil {
		return "", err
	}
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}

// verify checks the inner JWS: its header, its signature, then iss, iat and exp. A non-empty
// reason means the device secret is invalid; a ServiceError means the check itself failed.
func (p *deviceSecretProvider) verify(ctx context.Context, compact string) (*deviceSecretClaims, string, *common.ServiceError) {
	parts := strings.Split(compact, ".")
	if len(parts) != 3 {
		return nil, "inner token is not a compact JWS", nil
	}
	var header deviceSecretHeader
	if err := decodeDeviceSecretSegment(parts[0], &header); err != nil {
		return nil, "malformed header", nil
	}
	if header.Typ != deviceSecretJWSType {
		return nil, "wrong typ", nil
	}
	if header.Alg != deviceSecretJWSAlgorithm {
		return nil, "wrong alg", nil
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return nil, "malformed signature", nil
	}
	err = p.signer.VerifyRaw(ctx, config.OIDCServiceAppID, p.signingRefID, deviceSecretJWSAlgorithm,
		[]byte(parts[0]+"."+parts[1]), sig)
	if err != nil {
		if errors.Is(err, signature.ErrVerifyFailed) {
			return nil, "bad signature", nil
		}
		applog.GetLogger().Error(ctx, "device secret signature check failed", applog.Error(err))
		return nil, "", shared.InternalServerError
	}

	var claims deviceSecretClaims
	if err := decodeDeviceSecretSegment(parts[1], &claims); err != nil {
		return nil, "malformed claims", nil
	}
	now := p.now().Unix()
	switch {
	case claims.Issuer != p.issuer:
		return nil, "wrong issuer", nil
	case claims.ClientID == "" || claims.AuthID == "" || claims.SessionID == "" || claims.CNF.JKT == "":
		return nil, "missing required claim", nil
	case claims.IssuedAt > now+p.leewaySeconds:
		return nil, "issued in the future", nil
	case claims.ExpiresAt <= now-p.leewaySeconds:
		return nil, "expired", nil
	}
	return &claims, "", nil
}

// loadSession returns the session record of a verified device secret. The record must exist and
// belong to the same login and client as the device secret.
func (p *deviceSecretProvider) loadSession(ctx context.Context, claims *deviceSecretClaims) (*nativeSSOSession,
	*common.ServiceError) {
	logger := applog.GetLogger()
	raw, err := p.store.Get(ctx, providers.NamespaceNativeSSOSession, claims.SessionID)
	if err != nil && !errors.Is(err, providers.ErrRuntimeStoreKeyNotFound) {
		logger.Error(ctx, "device secret session lookup failed", applog.Error(err))
		return nil, shared.InternalServerError
	}
	if len(raw) == 0 {
		return nil, sessionEnded()
	}
	var session nativeSSOSession
	if err := json.Unmarshal(raw, &session); err != nil {
		logger.Error(ctx, "device secret session record is corrupt", applog.Error(err))
		return nil, shared.InternalServerError
	}
	if session.AuthID != claims.AuthID || session.ClientID != claims.ClientID {
		logger.Warn(ctx, "device secret does not match its session record", applog.String("clientId", claims.ClientID))
		return nil, invalidDeviceSecret()
	}
	if session.ExpiresAt <= p.now().Unix() {
		return nil, sessionEnded()
	}
	return &session, nil
}

// isDeviceSecretDecryptRejection reports whether a JWTDecrypt error comes from the presented
// value (not a JWE, not for our key, tampered) rather than from the key store.
func isDeviceSecretDecryptRejection(err error) bool {
	for _, target := range []error{
		cryptomanager.ErrInvalidRequest,
		cryptomanager.ErrJWEInvalidCompactSerialization,
		cryptomanager.ErrJWEDecryptFailed,
		cryptomanager.ErrKeyNotFoundForThumbprint,
		cryptomanager.ErrKeyIdentifierMismatch,
		cryptomanager.ErrDecryptionNotAllowed,
		cryptomanager.ErrForeignDomainKeyNotDecryptable,
	} {
		if errors.Is(err, target) {
			return true
		}
	}
	return false
}

func decodeDeviceSecretSegment(segment string, v any) error {
	raw, err := base64.RawURLEncoding.DecodeString(segment)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, v)
}

func withoutDeviceSSO(scopes []string) []string {
	return slices.DeleteFunc(slices.Clone(scopes), func(s string) bool { return s == providers.ScopeDeviceSSO })
}

// invalidDeviceSecret returns a copy, so that no caller can change the shared error value.
func invalidDeviceSecret() *common.ServiceError {
	svcErr := providers.ErrorInvalidDeviceSecret
	return &svcErr
}

func sessionEnded() *common.ServiceError {
	svcErr := providers.ErrorDeviceSecretSessionEnded
	return &svcErr
}
