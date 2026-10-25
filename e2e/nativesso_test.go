// SPDX-License-Identifier: MIT
// Copyright (c) 2026 Team Badmosip

//go:build e2e

// Live OpenID Connect Native SSO suite: one test per server rule of design.md section 20 (R01-R28)
// that can be reached from outside. Run it with
//
//	bash scripts/with-stack.sh bash e2e/run.sh
//
// which runs phase 1, restarts eSignet with a short device secret lifetime, and runs phase 2.
// R03 and R08 are unit-only (they need a server without a device secret provider, or a failing
// one); R12's sid-only mismatch needs a forged id_token, because the ds_hash check runs first.
package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/badmosip/esignet-native-sso/e2e/client"
	"github.com/badmosip/esignet-native-sso/e2e/internal/uuid"
)

// R01: a client allowed device_sso that asks for it gets a device secret with its tokens.
func TestE2E_SuperAppLogin_IssuesDeviceSecret(t *testing.T) {
	f := nativeSSO(t)
	if f.tok.DeviceSecret == "" {
		t.Fatal("no device_secret")
	}
	if !strings.Contains(" "+f.tok.Scope+" ", " "+client.ScopeDeviceSSO+" ") {
		t.Logf("token response scope %q does not list device_sso", f.tok.Scope)
	}
	record(t, "R01", 200, client.OAuthError{}, "device_secret issued")
}

// R02: a client whose registration does not allow device_sso is refused at PAR.
func TestE2E_DeviceSSO_ClientNotAllowed(t *testing.T) {
	c := newClient(t)
	reg := mustRegisterNative(t, c, "ps03-vendor-"+uuid.New()[:8], "plainapp", func(r *client.Registration) {
		r.AllowedScopes = nil
	})
	dpop, _ := client.NewECKey()
	r, err := c.PushAuthorization(client.LoginRequest{Reg: reg, Scope: hubScope, DPoPKey: dpop}, client.NewPKCE(),
		client.RandomString(12), client.RandomString(12))
	if err != nil {
		t.Fatal(err)
	}
	expectError(t, "R02", r, http.StatusBadRequest, "invalid_scope")
}

// R04: the device secret is a compact JWE: RSA-OAEP-256, A256GCM, content type JWT.
func TestE2E_DeviceSecret_IsNestedJWE(t *testing.T) {
	f := nativeSSO(t)
	h, err := client.JWEHeader(f.tok.DeviceSecret)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"alg": "RSA-OAEP-256", "enc": "A256GCM", "cty": "JWT"}
	for k, v := range want {
		if h[k] != v {
			t.Errorf("device secret header %s = %v, want %s", k, h[k], v)
		}
	}
	if kid, _ := h["kid"].(string); kid == "" {
		t.Error("device secret header has no kid")
	}
	record(t, "R04", 200, client.OAuthError{}, "JWE header alg=RSA-OAEP-256 enc=A256GCM cty=JWT")
}

// R05: the Hub id_token carries sid and ds_hash (left half of SHA-256 of the device secret).
func TestE2E_IDToken_HasSIDAndDSHash(t *testing.T) {
	f := nativeSSO(t)
	c := newClient(t)
	jwks, err := c.JWKS()
	if err != nil {
		t.Fatal(err)
	}
	if err := f.idt.VerifyWithJWKS(jwks); err != nil {
		t.Errorf("id_token signature: %v", err)
	}
	if f.idt.Str("sid") == "" {
		t.Error("id_token has no sid")
	}
	if got, want := f.idt.Str("ds_hash"), client.DSHash(f.tok.DeviceSecret); got != want {
		t.Errorf("id_token ds_hash %q, want %q", got, want)
	}
	record(t, "R05", 200, client.OAuthError{}, "sid present, ds_hash matches")
}

// R06: the token response has access_token, id_token, device_secret and token_type DPoP; the
// access token is bound to the Hub's DPoP key.
func TestE2E_TokenResponse_Fields(t *testing.T) {
	f := nativeSSO(t)
	if f.tok.AccessToken == "" || f.tok.IDToken == "" || f.tok.DeviceSecret == "" {
		t.Fatalf("missing fields: access_token %t id_token %t device_secret %t",
			f.tok.AccessToken != "", f.tok.IDToken != "", f.tok.DeviceSecret != "")
	}
	if !strings.EqualFold(f.tok.TokenType, "DPoP") {
		t.Errorf("token_type %q, want DPoP", f.tok.TokenType)
	}
	if got := cnfJKT(t, f.tok.AccessToken); got != f.hubDPoP.Thumbprint() {
		t.Errorf("access token cnf.jkt %q, want the Hub DPoP key %q", got, f.hubDPoP.Thumbprint())
	}
	record(t, "R06", 200, client.OAuthError{}, "access_token, id_token, device_secret, token_type DPoP")
}

// R07: device_sso needs a DPoP proof at the code grant: invalid_request for a client without
// DPoP-bound tokens, invalid_dpop_proof for a DPoP-bound client.
func TestE2E_DeviceSSO_RequiresDPoP(t *testing.T) {
	c := newClient(t)
	rp := "ps03-vendor-" + uuid.New()[:8]

	t.Run("client without DPoP binding", func(t *testing.T) {
		reg := mustRegisterNative(t, c, rp, "nodpopapp", func(r *client.Registration) { r.NoDPoPBinding = true })
		req := client.LoginRequest{Reg: reg, Scope: hubScope, User: seededUser}
		code, err := c.Authorize(req)
		if err != nil {
			t.Fatal(err)
		}
		r, err := c.TokenRequest(req, code, nil)
		if err != nil {
			t.Fatal(err)
		}
		expectError(t, "R07", r, http.StatusBadRequest, "invalid_request")
	})

	t.Run("DPoP-bound client", func(t *testing.T) {
		reg := mustRegisterNative(t, c, rp, "superapp", nil)
		dpop, _ := client.NewECKey()
		req := client.LoginRequest{Reg: reg, Scope: hubScope, User: seededUser, DPoPKey: dpop}
		code, err := c.Authorize(req)
		if err != nil {
			t.Fatal(err)
		}
		r, err := c.TokenRequest(req, code, nil)
		if err != nil {
			t.Fatal(err)
		}
		expectError(t, "R07", r, http.StatusBadRequest, "invalid_dpop_proof")
	})
}

// R09: a tampered device secret, or another token in its place, is invalid_request.
func TestE2E_Exchange_TamperedDeviceSecret(t *testing.T) {
	f := nativeSSO(t)
	c := newClient(t)

	parts := strings.Split(f.tok.DeviceSecret, ".")
	ct := []byte(parts[3])
	i := len(ct) / 2
	if ct[i] == 'A' {
		ct[i] = 'B'
	} else {
		ct[i] = 'A'
	}
	parts[3] = string(ct)
	tampered := strings.Join(parts, ".")

	for name, actor := range map[string]string{
		"tampered ciphertext":       tampered,
		"id_token as device secret": f.tok.IDToken,
		"access token as device secret": f.tok.AccessToken,
	} {
		t.Run(name, func(t *testing.T) {
			req := f.exchange(f.child)
			req.ActorToken = actor
			expectError(t, "R09", doExchange(t, c, req), http.StatusBadRequest, "invalid_request")
		})
	}
}

// R10: only urn:openid:params:token-type:device-secret is accepted as actor_token_type; the
// draft URN and other types are invalid_request.
func TestE2E_Exchange_OldURNRejected(t *testing.T) {
	f := nativeSSO(t)
	c := newClient(t)
	for _, typ := range []string{client.TokenTypeOldDeviceSecret, client.TokenTypeAccessToken} {
		t.Run(typ, func(t *testing.T) {
			req := f.exchange(f.child)
			req.ActorTokenType = typ
			expectError(t, "R10", doExchange(t, c, req), http.StatusBadRequest, "invalid_request")
		})
	}
}

// R11: the id_token of one login with the device secret of another (ds_hash mismatch).
func TestE2E_Exchange_DSHashMismatch(t *testing.T) {
	f := nativeSSO(t)
	other := secondHubLogin(t, f)
	c := newClient(t)
	req := f.exchange(f.child)
	req.ActorToken = other.DeviceSecret
	e := expectError(t, "R11", doExchange(t, c, req), http.StatusBadRequest, "invalid_request")
	if !strings.Contains(e.Description, "ds_hash") {
		t.Logf("error_description %q does not name ds_hash", e.Description)
	}
}

// R13: the requesting client must allow device_sso and the token-exchange grant.
func TestE2E_Exchange_ClientWithoutDeviceSSO(t *testing.T) {
	f := nativeSSO(t)
	c := newClient(t)
	cases := map[string]func(*client.Registration){
		"no device_sso":           func(r *client.Registration) { r.AllowedScopes = nil },
		"no token-exchange grant": func(r *client.Registration) { r.GrantTypes = []string{"authorization_code"} },
	}
	for name, mod := range cases {
		t.Run(name, func(t *testing.T) {
			reg := mustRegisterNative(t, c, f.rp, "childapp", mod)
			expectError(t, "R13", doExchange(t, c, f.exchange(reg)), http.StatusBadRequest, "unauthorized_client")
		})
	}
}

// R14: the proof must be made with the Hub's DPoP key (the device secret's cnf.jkt).
func TestE2E_Exchange_DPoPKeyMismatch(t *testing.T) {
	f := nativeSSO(t)
	c := newClient(t)
	req := f.exchange(f.child)
	req.DPoPKey, _ = client.NewECKey()
	expectError(t, "R14", doExchange(t, c, req), http.StatusBadRequest, "invalid_grant")
}

// R14: a DPoP-bound client without a proof is refused before the grant handler runs.
func TestE2E_Exchange_NoDPoP(t *testing.T) {
	f := nativeSSO(t)
	c := newClient(t)
	req := f.exchange(f.child)
	req.DPoPKey = nil
	expectError(t, "R14", doExchange(t, c, req), http.StatusBadRequest, "invalid_dpop_proof")
}

// R15: a scope wider than the Hub's grant is interaction_required, naming only the missing scope.
func TestE2E_Exchange_ScopeSuperset(t *testing.T) {
	f := nativeSSO(t)
	c := newClient(t)
	req := f.exchange(f.child)
	req.Scope = "openid profile email phone"
	e := expectError(t, "R15", doExchange(t, c, req), http.StatusBadRequest, "interaction_required")
	if !strings.Contains(e.Description, "phone") {
		t.Errorf("error_description %q does not name the missing scope phone", e.Description)
	}
	for _, granted := range []string{"profile", "email"} {
		if strings.Contains(e.Description, granted) {
			t.Errorf("error_description %q names the granted scope %s", e.Description, granted)
		}
	}
}

// R16: device_sso cannot be asked for in the exchange.
func TestE2E_Exchange_DeviceSSOInScope(t *testing.T) {
	f := nativeSSO(t)
	c := newClient(t)
	req := f.exchange(f.child)
	req.Scope = childNarrowScope + " " + client.ScopeDeviceSSO
	expectError(t, "R16", doExchange(t, c, req), http.StatusBadRequest, "invalid_scope")
}

// R17: a narrower scope is issued as asked, and userinfo for that token shows only its claims
// (eSignet's userinfo scope filter).
func TestE2E_Exchange_NarrowerScope(t *testing.T) {
	f := nativeSSO(t)
	c := newClient(t)
	req := f.exchange(f.child)
	req.Scope = childNarrowScope
	out := expectExchangeOK(t, "R17", doExchange(t, c, req))
	if out.Scope != childNarrowScope {
		t.Errorf("scope %q, want %q", out.Scope, childNarrowScope)
	}

	jwks, err := c.JWKS()
	if err != nil {
		t.Fatal(err)
	}
	hubInfo := userinfoJWT(t, c, f.hubDPoP, f.tok.AccessToken)
	childInfo := userinfoJWT(t, c, f.hubDPoP, out.AccessToken)
	t.Logf("Hub userinfo claims %v; Health Card (%s) userinfo claims %v", claimNames(hubInfo), childNarrowScope, claimNames(childInfo))
	if err := childInfo.VerifyWithJWKS(jwks); err != nil {
		t.Errorf("Health Card userinfo signature: %v", err)
	}
	if !audHas(childInfo.Claims["aud"], f.child.ClientID) && childInfo.Claims["aud"] != nil {
		t.Errorf("Health Card userinfo aud %v, want %s", childInfo.Claims["aud"], f.child.ClientID)
	}
	if childInfo.Str("sub") != hubInfo.Str("sub") {
		t.Errorf("userinfo sub %q, Hub's %q", childInfo.Str("sub"), hubInfo.Str("sub"))
	}
	hidden := 0
	for _, profileClaim := range []string{"name", "gender", "birthdate"} {
		if hasClaim(childInfo, profileClaim) {
			t.Errorf("Health Card userinfo for %q shows %s", childNarrowScope, profileClaim)
		}
		if hasClaim(hubInfo, profileClaim) {
			hidden++
		}
	}
	if hidden == 0 {
		t.Errorf("Hub userinfo has no profile claim, so hiding cannot be shown: %v", claimNames(hubInfo))
	}
	record(t, "R17", 200, client.OAuthError{}, "Health Card userinfo claims: "+strings.Join(claimNames(childInfo), ","))
}

func userinfoJWT(t *testing.T, c *client.Client, dpop *client.ECKey, accessToken string) *client.JWT {
	t.Helper()
	r, err := c.Userinfo(dpop, accessToken)
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != 200 {
		t.Fatalf("userinfo: HTTP %d: %.300s", r.Status, r.Body)
	}
	j, err := client.ParseJWT(strings.TrimSpace(string(r.Body)))
	if err != nil {
		t.Fatalf("userinfo is not a signed JWT: %v: %.200s", err, r.Body)
	}
	return j
}

// R18: audience must be the issuer.
func TestE2E_Exchange_WrongAudience(t *testing.T) {
	f := nativeSSO(t)
	c := newClient(t)
	req := f.exchange(f.child)
	req.Audience = "https://other-issuer.example"
	expectError(t, "R18", doExchange(t, c, req), http.StatusBadRequest, "invalid_target")
}

// R19: a Child App of another relying party cannot use the Hub's device secret.
func TestE2E_Exchange_OtherVendor(t *testing.T) {
	f := nativeSSO(t)
	c := newClient(t)
	other := mustRegisterNative(t, c, "ps03-other-vendor-"+uuid.New()[:8], "otherapp", nil)
	expectError(t, "R19", doExchange(t, c, f.exchange(other)), http.StatusBadRequest, "invalid_request")
}

// R20: an id_token signed by another key, or changed after signing, is invalid_request.
func TestE2E_Exchange_ForgedIDToken(t *testing.T) {
	f := nativeSSO(t)
	c := newClient(t)
	forger, _ := client.NewECKey()
	header := map[string]any{"typ": "JWT"}
	if kid, ok := f.idt.Header["kid"]; ok {
		header["kid"] = kid
	}
	resigned, err := forger.SignJWT(header, f.idt.Claims)
	if err != nil {
		t.Fatal(err)
	}
	claims := map[string]any{}
	for k, v := range f.idt.Claims {
		claims[k] = v
	}
	claims["sub"] = "someone-else"
	parts := strings.Split(f.tok.IDToken, ".")
	changed := parts[0] + "." + client.B64(mustJSON(t, claims)) + "." + parts[2]

	for name, idToken := range map[string]string{"signed by another key": resigned, "claims changed": changed} {
		t.Run(name, func(t *testing.T) {
			req := f.exchange(f.child)
			req.SubjectToken = idToken
			expectError(t, "R20", doExchange(t, c, req), http.StatusBadRequest, "invalid_request")
		})
	}
}

// R21: the Hub id_token lives 120 s; after it expired the exchange still succeeds while the device
// secret and its session are valid (the stack sets no id_token_max_age_seconds cap).
func TestE2E_Exchange_ExpiredIDToken(t *testing.T) {
	t.Parallel()
	f := nativeSSO(t)
	exp, ok := f.idt.Claims["exp"].(float64)
	if !ok {
		t.Fatal("id_token has no exp")
	}
	wait := time.Until(time.Unix(int64(exp), 0).Add((jwtLeewaySeconds + 3) * time.Second))
	if wait > 0 {
		t.Logf("waiting %s for the Hub id_token to expire", wait.Round(time.Second))
		time.Sleep(wait)
	}
	c := newClient(t)
	expectExchangeOK(t, "R21", doExchange(t, c, f.exchange(f.child)))
}

// R22: a DPoP proof is accepted once.
func TestE2E_Exchange_DPoPReplay(t *testing.T) {
	f := nativeSSO(t)
	c := newClient(t)
	proof, err := client.DPoPProof(f.hubDPoP, http.MethodPost, c.Disc.TokenEndpoint, "", "")
	if err != nil {
		t.Fatal(err)
	}
	req := f.exchange(f.child)
	req.DPoPProof = proof
	expectExchangeOK(t, "R22", doExchange(t, c, req))
	expectError(t, "R22", doExchange(t, c, req), http.StatusBadRequest, "invalid_dpop_proof")
}

// R23: the Health Card's tokens: aud Health Card, the Hub's sid and sub, bound to the Hub's DPoP
// key, no ds_hash and no device secret; userinfo works.
func TestE2E_Exchange_ChildTokens(t *testing.T) {
	f := nativeSSO(t)
	c := newClient(t)
	out := expectExchangeOK(t, "R23", doExchange(t, c, f.exchange(f.child)))

	if out.IssuedTokenType != client.TokenTypeAccessToken {
		t.Errorf("issued_token_type %q, want %q", out.IssuedTokenType, client.TokenTypeAccessToken)
	}
	if !strings.EqualFold(out.TokenType, "DPoP") {
		t.Errorf("token_type %q, want DPoP", out.TokenType)
	}
	if want := "openid profile email"; out.Scope != want {
		t.Errorf("default scope %q, want %q (the Hub's without device_sso)", out.Scope, want)
	}
	if out.DeviceSecret != "" {
		t.Error("the exchange returned a device_secret")
	}
	if got := cnfJKT(t, out.AccessToken); got != f.hubDPoP.Thumbprint() {
		t.Errorf("access token cnf.jkt %q, want the Hub DPoP key %q", got, f.hubDPoP.Thumbprint())
	}
	if out.IDToken == "" {
		t.Fatal("no id_token")
	}
	idt, err := client.ParseJWT(out.IDToken)
	if err != nil {
		t.Fatal(err)
	}
	jwks, err := c.JWKS()
	if err != nil {
		t.Fatal(err)
	}
	if err := idt.VerifyWithJWKS(jwks); err != nil {
		t.Errorf("id_token signature: %v", err)
	}
	if !audHas(idt.Claims["aud"], f.child.ClientID) {
		t.Errorf("id_token aud %v, want %s", idt.Claims["aud"], f.child.ClientID)
	}
	for _, claim := range []string{"sub", "sid"} {
		if idt.Str(claim) == "" || idt.Str(claim) != f.idt.Str(claim) {
			t.Errorf("id_token %s %q, Hub's %q", claim, idt.Str(claim), f.idt.Str(claim))
		}
	}
	if _, ok := idt.Claims["ds_hash"]; ok {
		t.Error("Health Card id_token has ds_hash")
	}
	info := userinfoJWT(t, c, f.hubDPoP, out.AccessToken)
	if info.Str("sub") != f.idt.Str("sub") {
		t.Errorf("userinfo sub %q, want %q", info.Str("sub"), f.idt.Str("sub"))
	}
}

// R23: the sub the Health Card gets by exchange equals the one of its own direct login (sub is
// per relying party).
func TestE2E_Exchange_SubMatchesDirectLogin(t *testing.T) {
	f := nativeSSO(t)
	c := newClient(t)
	dpop, _ := client.NewECKey()
	tok, err := c.Login(client.LoginRequest{Reg: f.child, Scope: childNarrowScope, User: seededUser, DPoPKey: dpop})
	if err != nil {
		t.Fatal(err)
	}
	direct, err := client.ParseJWT(tok.IDToken)
	if err != nil {
		t.Fatal(err)
	}
	if direct.Str("sub") != f.idt.Str("sub") {
		t.Errorf("Health Card direct-login sub %q, Hub sub %q", direct.Str("sub"), f.idt.Str("sub"))
	}
	record(t, "R23", 200, client.OAuthError{}, "direct-login sub equals Hub sub")
}

// R24: discovery advertises Native SSO.
func TestE2E_Discovery_NativeSSO(t *testing.T) {
	c := newClient(t)
	if !c.Disc.NativeSSOSupported {
		t.Error("native_sso_supported is not true")
	}
	if !slices.Contains(c.Disc.ScopesSupported, client.ScopeDeviceSSO) {
		t.Errorf("scopes_supported %v lacks device_sso", c.Disc.ScopesSupported)
	}
	if !slices.Contains(c.Disc.GrantTypesSupported, client.GrantTypeTokenExchange) {
		t.Errorf("grant_types_supported %v lacks the token-exchange grant", c.Disc.GrantTypesSupported)
	}
	record(t, "R24", 200, client.OAuthError{}, "native_sso_supported="+strconv.FormatBool(c.Disc.NativeSSOSupported))
}

// R25: client registration accepts the token-exchange grant and rejects unknown grants.
func TestE2E_ClientRegistration_TokenExchangeGrant(t *testing.T) {
	c := newClient(t)
	rp := "ps03-vendor-" + uuid.New()[:8]
	mustRegisterNative(t, c, rp, "superapp", nil)
	_, err := registerNative(c, rp, "badapp", func(r *client.Registration) {
		r.GrantTypes = []string{"authorization_code", "urn:example:unknown-grant"}
	})
	if err == nil || !strings.Contains(err.Error(), "invalid_grant_type") {
		t.Errorf("unknown grant: got %v, want an invalid_grant_type error", err)
	}
	record(t, "R25", 200, client.OAuthError{Error: "invalid_grant_type"}, "token-exchange grant accepted; unknown grant rejected")
}

// R26: with exchange_only, a token exchange without a device secret is invalid_request.
func TestE2E_GenericExchangeRejected(t *testing.T) {
	f := nativeSSO(t)
	c := newClient(t)
	key, _ := client.NewECKey()
	req := client.ExchangeRequest{Reg: f.child, SubjectToken: f.tok.AccessToken, SubjectTokenType: client.TokenTypeAccessToken,
		ActorTokenType: "-", DPoPKey: key}
	expectError(t, "R26", doExchange(t, c, req), http.StatusBadRequest, "invalid_request")
}

// R27: the Hub revokes its device secret at the revocation endpoint (RFC 7009), which ends the
// login session: the Health Card's next exchange is invalid_grant, and the access token an earlier
// exchange gave it is refused at userinfo. Another client of the same vendor that holds the device
// secret cannot revoke it. Uses its own login, so the shared one stays valid.
func TestE2E_Revoke_DeviceSecret(t *testing.T) {
	c := newClient(t)
	if c.Disc.RevocationEndpoint != c.Issuer+"/oauth2/revoke" {
		t.Errorf("revocation_endpoint %q, want %q", c.Disc.RevocationEndpoint, c.Issuer+"/oauth2/revoke")
	}
	f := &nativeFixture{rp: "ps03-vendor-" + uuid.New()[:8]}
	f.hub = mustRegisterNative(t, c, f.rp, "superapp", nil)
	f.child = mustRegisterNative(t, c, f.rp, "childapp", nil)
	f.hubDPoP, _ = client.NewECKey()
	tok, err := c.Login(client.LoginRequest{Reg: f.hub, Scope: hubScope, User: seededUser, DPoPKey: f.hubDPoP})
	if err != nil {
		t.Fatal(err)
	}
	f.tok = tok
	// The apps' path: Health Card's token bound to the Hub key, Hub signs its userinfo proofs.
	childTok := expectExchangeOK(t, "R27", doExchange(t, c, f.exchange(f.child)))
	userinfoJWT(t, c, f.hubDPoP, childTok.AccessToken)
	expectActive(t, c, f.child, childTok.AccessToken, true)

	t.Run("other client of the vendor", func(t *testing.T) {
		r, err := c.Revoke(f.child, f.tok.DeviceSecret, client.TokenTypeHintDeviceSecret)
		if err != nil {
			t.Fatal(err)
		}
		expectError(t, "R27", r, http.StatusBadRequest, "invalid_grant")
		expectExchangeOK(t, "R27", doExchange(t, c, f.exchange(f.child)))
	})

	t.Run("issuing client", func(t *testing.T) {
		r, err := c.Revoke(f.hub, f.tok.DeviceSecret, client.TokenTypeHintDeviceSecret)
		if err != nil {
			t.Fatal(err)
		}
		if r.Status != http.StatusOK {
			t.Fatalf("revoke: HTTP %d %s, want 200", r.Status, r.Body)
		}
		record(t, "R27", r.Status, client.OAuthError{}, "device secret revoked by the Hub")
		expectError(t, "R27", doExchange(t, c, f.exchange(f.child)), http.StatusBadRequest, "invalid_grant")

		info, err := c.Userinfo(f.hubDPoP, childTok.AccessToken)
		if err != nil {
			t.Fatal(err)
		}
		expectError(t, "R27", info, http.StatusUnauthorized, "invalid_token")
		expectActive(t, c, f.child, childTok.AccessToken, false)
	})

	t.Run("again and unknown values are no-ops", func(t *testing.T) {
		for _, token := range []string{f.tok.DeviceSecret, "not-a-device-secret"} {
			r, err := c.Revoke(f.hub, token, client.TokenTypeHintDeviceSecret)
			if err != nil {
				t.Fatal(err)
			}
			if r.Status != http.StatusOK {
				t.Errorf("revoke %.12s...: HTTP %d %s, want 200", token, r.Status, r.Body)
			}
		}
	})

	t.Run("other tokens without the deny list", func(t *testing.T) {
		r, err := c.Revoke(f.hub, f.tok.AccessToken, "")
		if err != nil {
			t.Fatal(err)
		}
		expectError(t, "R27", r, http.StatusBadRequest, "unsupported_token_type")
	})
}

// expectActive checks the introspection answer (RFC 7662) for token, asked by reg.
func expectActive(t *testing.T, c *client.Client, reg client.Registration, token string, want bool) {
	t.Helper()
	r, err := c.Introspect(reg, token)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Active bool `json:"active"`
	}
	if r.Status != http.StatusOK || r.JSON(&got) != nil || got.Active != want {
		t.Errorf("R27: introspection HTTP %d %s, want 200 with active %v", r.Status, r.Body, want)
		return
	}
	record(t, "R27", r.Status, client.OAuthError{}, fmt.Sprintf("introspection active=%v", want))
}

// Phase 2 (after an eSignet restart, which empties the in-memory runtime store).

// R12: the phase-1 device secret's session ended with the restart: invalid_grant.
func TestE2E_Exchange_SessionEnded(t *testing.T) {
	requirePhase(t, "2")
	f, err := loadState(t)
	if err != nil {
		t.Fatal(err)
	}
	c := newClient(t)
	expectError(t, "R12", doExchange(t, c, f.exchange(f.child)), http.StatusBadRequest, "invalid_grant")
}

// R09: a device secret past its lifetime is invalid_request. eSignet runs phase 2 with
// E2E_DEVICE_SECRET_VALIDITY_SECONDS (a few seconds) so the test can wait it out.
func TestE2E_Exchange_ExpiredDeviceSecret(t *testing.T) {
	requirePhase(t, "2")
	validity, err := strconv.Atoi(os.Getenv("E2E_DEVICE_SECRET_VALIDITY_SECONDS"))
	if err != nil || validity <= 0 || validity > 300 {
		t.Fatalf("E2E_DEVICE_SECRET_VALIDITY_SECONDS=%q: want 1-300", os.Getenv("E2E_DEVICE_SECRET_VALIDITY_SECONDS"))
	}
	f, err := loadState(t)
	if err != nil {
		t.Fatal(err)
	}
	c := newClient(t)
	tok, err := c.Login(client.LoginRequest{Reg: f.hub, Scope: hubScope, User: seededUser, DPoPKey: f.hubDPoP})
	if err != nil {
		t.Fatal(err)
	}
	f.tok = tok
	expectExchangeOK(t, "R09", doExchange(t, c, f.exchange(f.child)))

	wait := time.Duration(validity+jwtLeewaySeconds+3) * time.Second
	t.Logf("waiting %s for the device secret to expire", wait)
	time.Sleep(wait)
	expectError(t, "R09", doExchange(t, c, f.exchange(f.child)), http.StatusBadRequest, "invalid_request")
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
