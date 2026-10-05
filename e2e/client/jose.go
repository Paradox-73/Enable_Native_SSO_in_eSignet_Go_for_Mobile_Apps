// SPDX-License-Identifier: MIT
// Copyright (c) 2026 Team Badmosip

// Package client is a headless OpenID Connect client for the local stack: discovery, PKCE,
// DPoP (ES256), private_key_jwt, PAR, authorize, the /flow/execute login steps, the eSignet
// callback, token and userinfo. It uses only the Go standard library.
package client

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strings"
)

var b64 = base64.RawURLEncoding

// B64 encodes bytes as unpadded base64url.
func B64(b []byte) string { return b64.EncodeToString(b) }

// ECKey is a P-256 key used for ES256 signatures (client assertions and DPoP proofs).
type ECKey struct {
	Private *ecdsa.PrivateKey
	Kid     string
}

// NewECKey makes a fresh P-256 key; its kid is the RFC 7638 thumbprint.
func NewECKey() (*ECKey, error) {
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	key := &ECKey{Private: k}
	key.Kid = key.Thumbprint()
	return key, nil
}

func (k *ECKey) coords() (string, string) {
	pub, err := k.Private.PublicKey.Bytes() // 0x04 || X || Y
	if err != nil {
		panic(err)
	}
	return B64(pub[1:33]), B64(pub[33:65])
}

// PublicJWK is the public key as a JWK with only the required members.
func (k *ECKey) PublicJWK() map[string]string {
	x, y := k.coords()
	return map[string]string{"kty": "EC", "crv": "P-256", "x": x, "y": y}
}

// Thumbprint is the RFC 7638 SHA-256 JWK thumbprint (the DPoP jkt).
func (k *ECKey) Thumbprint() string {
	x, y := k.coords()
	// Members in lexicographic order, no spaces (RFC 7638 section 3.2).
	canon := fmt.Sprintf(`{"crv":"P-256","kty":"EC","x":"%s","y":"%s"}`, x, y)
	sum := sha256.Sum256([]byte(canon))
	return B64(sum[:])
}

// SignJWT signs header and claims with ES256 and returns the compact JWS.
func (k *ECKey) SignJWT(header, claims map[string]any) (string, error) {
	header["alg"] = "ES256"
	h, err := json.Marshal(header)
	if err != nil {
		return "", err
	}
	c, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	input := B64(h) + "." + B64(c)
	sum := sha256.Sum256([]byte(input))
	r, s, err := ecdsa.Sign(rand.Reader, k.Private, sum[:])
	if err != nil {
		return "", err
	}
	sig := make([]byte, 64)
	r.FillBytes(sig[:32])
	s.FillBytes(sig[32:])
	return input + "." + B64(sig), nil
}

// RandomString returns n random bytes as base64url.
func RandomString(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return B64(b)
}

// PKCE holds an RFC 7636 verifier and its S256 challenge.
type PKCE struct{ Verifier, Challenge string }

// NewPKCE makes a 43-character verifier and its S256 challenge.
func NewPKCE() PKCE {
	v := RandomString(32)
	sum := sha256.Sum256([]byte(v))
	return PKCE{Verifier: v, Challenge: B64(sum[:])}
}

// JWT is a decoded compact JWS (signature not checked unless VerifyRS is called).
type JWT struct {
	Raw     string
	Header  map[string]any
	Claims  map[string]any
	signing string
	sig     []byte
}

// ParseJWT decodes the header and claims of a compact JWS.
func ParseJWT(raw string) (*JWT, error) {
	parts := strings.Split(raw, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("not a compact JWS: %d parts", len(parts))
	}
	j := &JWT{Raw: raw, signing: parts[0] + "." + parts[1]}
	for i, dst := range []*map[string]any{&j.Header, &j.Claims} {
		b, err := b64.DecodeString(parts[i])
		if err != nil {
			return nil, fmt.Errorf("part %d: %w", i, err)
		}
		if err := json.Unmarshal(b, dst); err != nil {
			return nil, fmt.Errorf("part %d: %w", i, err)
		}
	}
	sig, err := b64.DecodeString(parts[2])
	if err != nil {
		return nil, fmt.Errorf("signature: %w", err)
	}
	j.sig = sig
	return j, nil
}

// Str returns a string claim, or "".
func (j *JWT) Str(name string) string {
	s, _ := j.Claims[name].(string)
	return s
}

// VerifyWithJWKS checks the signature (PS256, RS256 or ES256) against the key in jwks whose
// kid matches the header's kid. A key that declares an alg must declare the header's alg, as a
// FAPI 2.0 client requires.
func (j *JWT) VerifyWithJWKS(jwks []map[string]any) error {
	kid, _ := j.Header["kid"].(string)
	alg, _ := j.Header["alg"].(string)
	for _, k := range jwks {
		if kid != "" && k["kid"] != kid {
			continue
		}
		if keyAlg, ok := k["alg"].(string); ok && keyAlg != "" && keyAlg != alg {
			return fmt.Errorf("JWKS key %q declares alg %q, token uses %q", kid, keyAlg, alg)
		}
		switch k["kty"] {
		case "RSA":
			n, e, err := rsaParts(k)
			if err != nil {
				return err
			}
			pub := &rsa.PublicKey{N: n, E: e}
			sum := sha256.Sum256([]byte(j.signing))
			switch alg {
			case "PS256":
				return rsa.VerifyPSS(pub, crypto.SHA256, sum[:], j.sig, &rsa.PSSOptions{SaltLength: rsa.PSSSaltLengthAuto})
			case "RS256":
				return rsa.VerifyPKCS1v15(pub, crypto.SHA256, sum[:], j.sig)
			}
		case "EC":
			if alg != "ES256" || len(j.sig) != 64 {
				continue
			}
			x, err1 := b64.DecodeString(fmt.Sprint(k["x"]))
			y, err2 := b64.DecodeString(fmt.Sprint(k["y"]))
			if err1 != nil || err2 != nil {
				return errors.New("bad EC key in JWKS")
			}
			pub, err := ecdsa.ParseUncompressedPublicKey(elliptic.P256(), append(append([]byte{4}, x...), y...))
			if err != nil {
				return fmt.Errorf("bad EC key in JWKS: %w", err)
			}
			sum := sha256.Sum256([]byte(j.signing))
			if ecdsa.Verify(pub, sum[:], new(big.Int).SetBytes(j.sig[:32]), new(big.Int).SetBytes(j.sig[32:])) {
				return nil
			}
			return errors.New("ES256 signature does not verify")
		}
	}
	return fmt.Errorf("no JWKS key for kid %q alg %q", kid, alg)
}

func rsaParts(k map[string]any) (*big.Int, int, error) {
	nb, err := b64.DecodeString(fmt.Sprint(k["n"]))
	if err != nil {
		return nil, 0, err
	}
	eb, err := b64.DecodeString(fmt.Sprint(k["e"]))
	if err != nil {
		return nil, 0, err
	}
	return new(big.Int).SetBytes(nb), int(new(big.Int).SetBytes(eb).Int64()), nil
}
