// SPDX-License-Identifier: MIT
// Copyright (c) 2026 Team Badmosip

//go:build e2e

// Live tests against the local stack (deploy/compose.yaml). Run them with
//
//	bash scripts/with-stack.sh bash e2e/run.sh -run 'TestE2E_FAPI2Login|TestE2E_PSUTProbe'
//
// Environment (e2e/run.sh sets these):
//
//	E2E_ISSUER         issuer URL (default http://localhost:38088)
//	E2E_DIAL_OVERRIDE  host:port=host:port pairs; inside the compose network
//	                   localhost:38088=esignet:8080, so URLs keep naming the issuer
package e2e

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/badmosip/esignet-native-sso/e2e/client"
	"github.com/badmosip/esignet-native-sso/e2e/internal/uuid"
)

// Seeded user of esignet/docker-compose/init.sql (upstream docker-compose README).
var seededUser = client.User{IndividualID: "1774231323", OTP: "111111", CaptchaToken: "e2e-captcha-token"}

const otpACR = "mosip:idp:acr:generated-code"

func newClient(t *testing.T) *client.Client {
	t.Helper()
	issuer := envOr("E2E_ISSUER", "http://localhost:38088")
	c, err := client.New(issuer, os.Getenv("E2E_DIAL_OVERRIDE"))
	if err != nil {
		t.Fatal(err)
	}
	c.Logf = t.Logf
	if err := c.Discover(); err != nil {
		t.Fatalf("discovery: %v", err)
	}
	return c
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// newRegistration makes a client id, a fresh ES256 client key and the registration request.
func newRegistration(t *testing.T, rp, app string) client.Registration {
	t.Helper()
	key, err := client.NewECKey()
	if err != nil {
		t.Fatal(err)
	}
	return client.Registration{
		ClientID:       "e2e-" + app + "-" + uuid.New()[:8],
		RelyingPartyID: rp,
		RedirectURI:    "org.badmosip.nativesso." + app + "://oauth/callback",
		Key:            key,
		UserClaims:     []string{"name", "email", "phone_number"},
		ACRs:           []string{otpACR},
	}
}

func login(t *testing.T, c *client.Client, reg client.Registration) (*client.Tokens, *client.ECKey) {
	t.Helper()
	dpop, err := client.NewECKey()
	if err != nil {
		t.Fatal(err)
	}
	tok, err := c.Login(client.LoginRequest{Reg: reg, Scope: "openid profile", User: seededUser, DPoPKey: dpop})
	if err != nil {
		t.Fatalf("login as %s: %v", reg.ClientID, err)
	}
	return tok, dpop
}

// TestE2E_FAPI2Login registers a FAPI 2.0 client (private_key_jwt with a key made now, PAR
// required, DPoP-bound tokens, PKCE) and logs the seeded user in through PAR, authorize, the
// OTP login steps, consent, the callback, token and userinfo.
func TestE2E_FAPI2Login(t *testing.T) {
	c := newClient(t)
	for _, alg := range []string{"ES256"} {
		if !contains(c.Disc.DPoPAlgs, alg) {
			t.Fatalf("discovery dpop_signing_alg_values_supported %v lacks %s", c.Disc.DPoPAlgs, alg)
		}
	}
	reg := newRegistration(t, "e2e-rp-"+uuid.New()[:8], "superapp")
	if err := c.RegisterClient(reg); err != nil {
		t.Fatal(err)
	}

	tok, dpop := login(t, c, reg)

	if !strings.EqualFold(tok.TokenType, "DPoP") {
		t.Errorf("token_type %q, want DPoP", tok.TokenType)
	}
	if tok.AccessToken == "" || tok.IDToken == "" {
		t.Fatalf("token response lacks access_token or id_token: %+v", tok)
	}
	if tok.CodeIssuer != "" && tok.CodeIssuer != c.Issuer {
		t.Errorf("authorization response iss %q, want %q", tok.CodeIssuer, c.Issuer)
	}

	jwks, err := c.JWKS()
	if err != nil {
		t.Fatal(err)
	}
	idt, err := client.ParseJWT(tok.IDToken)
	if err != nil {
		t.Fatalf("id_token: %v", err)
	}
	if err := idt.VerifyWithJWKS(jwks); err != nil {
		t.Errorf("id_token signature: %v", err)
	}
	if idt.Str("iss") != c.Issuer {
		t.Errorf("id_token iss %q, want %q", idt.Str("iss"), c.Issuer)
	}
	if !audHas(idt.Claims["aud"], reg.ClientID) {
		t.Errorf("id_token aud %v lacks %s", idt.Claims["aud"], reg.ClientID)
	}
	if idt.Str("sub") == "" {
		t.Error("id_token has no sub")
	}

	// A JWT access token must be bound to the DPoP key (RFC 9449 section 6.1).
	if at, err := client.ParseJWT(tok.AccessToken); err == nil {
		cnf, _ := at.Claims["cnf"].(map[string]any)
		if got, _ := cnf["jkt"].(string); got != dpop.Thumbprint() {
			t.Errorf("access token cnf.jkt %q, want %q", got, dpop.Thumbprint())
		}
	} else {
		t.Logf("access token is not a JWT (%v); cnf.jkt not checked", err)
	}

	r, err := c.Userinfo(dpop, tok.AccessToken)
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != 200 || len(r.Body) == 0 {
		t.Fatalf("userinfo: HTTP %d: %.300s", r.Status, r.Body)
	}
	if ui, err := client.ParseJWT(strings.TrimSpace(string(r.Body))); err == nil {
		if ui.Str("sub") != idt.Str("sub") {
			t.Errorf("userinfo sub %q, id_token sub %q", ui.Str("sub"), idt.Str("sub"))
		}
		// Signed with a key of the JWKS, with the alg that key declares (FAPI 2.0: PS256 or ES256).
		if err := ui.VerifyWithJWKS(jwks); err != nil {
			t.Errorf("userinfo signature: %v", err)
		}
		if alg, _ := ui.Header["alg"].(string); alg != "PS256" && alg != "ES256" {
			t.Errorf("userinfo alg %q, want PS256 or ES256", alg)
		}
	}

	// The same access token without a DPoP proof must be refused.
	r, err = c.Get(c.Disc.UserinfoEndpoint, map[string][]string{"Authorization": {"DPoP " + tok.AccessToken}})
	if err != nil {
		t.Fatal(err)
	}
	if r.Status == 200 {
		t.Errorf("userinfo without a DPoP proof answered 200, want 4xx")
	}
	t.Logf("login ok: client %s, sub %s, token_type %s", reg.ClientID, idt.Str("sub"), tok.TokenType)
}

// TestE2E_PSUTProbe logs the seeded user in through two clients of one relying party and
// reports whether the id_token sub (eSignet's partner-specific user token, PSUT) is the same
// for both clients (scope: relying party) or differs per client (scope: client). A third
// client under another relying party is reported too, for comparison.
func TestE2E_PSUTProbe(t *testing.T) {
	c := newClient(t)
	rp := "e2e-rp-" + uuid.New()[:8]
	subs := make([]string, 0, 3)
	for _, x := range []struct{ rp, app string }{{rp, "superapp"}, {rp, "childapp"}, {"e2e-rp-other-" + uuid.New()[:8], "otherapp"}} {
		reg := newRegistration(t, x.rp, x.app)
		if err := c.RegisterClient(reg); err != nil {
			t.Fatal(err)
		}
		tok, _ := login(t, c, reg)
		idt, err := client.ParseJWT(tok.IDToken)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("relying party %s client %s sub %s", x.rp, reg.ClientID, idt.Str("sub"))
		subs = append(subs, idt.Str("sub"))
	}
	if subs[0] == "" || subs[1] == "" {
		t.Fatal("empty sub")
	}
	scope := "client"
	if subs[0] == subs[1] {
		scope = "relying-party"
	}
	fmt.Printf("psut-scope: %s (same relying party: sub equal=%t; other relying party: sub equal=%t)\n",
		scope, subs[0] == subs[1], subs[0] == subs[2])
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func audHas(aud any, id string) bool {
	switch a := aud.(type) {
	case string:
		return a == id
	case []any:
		for _, v := range a {
			if v == id {
				return true
			}
		}
	}
	return false
}
