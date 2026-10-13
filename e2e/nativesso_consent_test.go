// SPDX-License-Identifier: MIT
// Copyright (c) 2026 Team Badmosip

//go:build e2e

package e2e

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/badmosip/esignet-native-sso/e2e/client"
	"github.com/badmosip/esignet-native-sso/e2e/internal/uuid"
)

// R28: consent-only re-consent (bonus B1). A scope beyond the Hub's grant that needs no new user data
// (the permission health_card_share) is approved on eSignet's consent page without a new login; the
// approval counts for that login session and that app only, and a later exchange still passes every
// binding check. A scope that needs new user data (phone) gets no consent page. Only the browser of the
// Hub's login can answer: the client c plays that browser (it kept the cookie of /oauth2/authorize), and
// a second client plays the app that received the consent_uri.

const (
	permissionScope = "health_card_share"
	consentScope    = "openid email " + permissionScope
)

// consentAnswer is the interaction_required answer with the consent-only members.
type consentAnswer struct {
	client.OAuthError
	ConsentURI       string `json:"consent_uri"`
	ConsentExpiresIn int64  `json:"consent_expires_in"`
}

func consentOf(t *testing.T, r *client.Response) consentAnswer {
	t.Helper()
	var a consentAnswer
	if err := json.Unmarshal(r.Body, &a); err != nil {
		t.Fatalf("HTTP %d answer is not JSON: %s", r.Status, r.Body)
	}
	return a
}

// askConsent sends an exchange for consentScope and returns its consent_uri.
func askConsent(t *testing.T, c *client.Client, req client.ExchangeRequest) consentAnswer {
	t.Helper()
	req.Scope = consentScope
	r := doExchange(t, c, req)
	expectError(t, "R28", r, http.StatusBadRequest, "interaction_required")
	a := consentOf(t, r)
	if a.ConsentURI == "" {
		t.Fatalf("R28: no consent_uri in %s", r.Body)
	}
	if !strings.HasPrefix(a.ConsentURI, c.Issuer+"/oauth2/native-sso/consent?ticket=") {
		t.Errorf("consent_uri %q is not on the issuer's consent page", a.ConsentURI)
	}
	if a.ConsentExpiresIn <= 0 || a.ConsentExpiresIn > 600 {
		t.Errorf("consent_expires_in %d, want 1..600", a.ConsentExpiresIn)
	}
	if !strings.Contains(a.Description, permissionScope) || strings.Contains(a.Description, "email") {
		t.Errorf("error_description %q should name only %s", a.Description, permissionScope)
	}
	if got := r.Header.Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control %q on an answer with a consent_uri, want no-store", got)
	}
	return a
}

// answerConsent posts the user's decision the way the page's form does and returns the answer.
func answerConsent(t *testing.T, c *client.Client, consentURI, decision string) *client.Response {
	t.Helper()
	u, err := url.Parse(consentURI)
	if err != nil {
		t.Fatal(err)
	}
	form := url.Values{"ticket": {u.Query().Get("ticket")}, "decision": {decision}}
	r, err := c.PostForm(c.Issuer+"/oauth2/native-sso/consent", form, http.Header{"Origin": {c.Issuer}})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestE2E_Exchange_ConsentOnlyReconsent(t *testing.T) {
	c := newClient(t)
	f := &nativeFixture{rp: "ps03-vendor-" + uuid.New()[:8]}
	f.hub = mustRegisterNative(t, c, f.rp, "superapp", nil)
	f.child = mustRegisterNative(t, c, f.rp, "childapp", func(r *client.Registration) {
		r.AllowedScopes = append(r.AllowedScopes, permissionScope)
	})
	f.hubDPoP, _ = client.NewECKey()
	tok, err := c.Login(client.LoginRequest{Reg: f.hub, Scope: hubScope, User: seededUser, DPoPKey: f.hubDPoP})
	if err != nil {
		t.Fatal(err)
	}
	f.tok = tok
	if f.idt, err = client.ParseJWT(tok.IDToken); err != nil {
		t.Fatal(err)
	}

	t.Run("consent page", func(t *testing.T) {
		a := askConsent(t, c, f.exchange(f.child))
		r, err := c.Get(a.ConsentURI, nil)
		if err != nil {
			t.Fatal(err)
		}
		body := string(r.Body)
		if r.Status != http.StatusOK || !strings.Contains(body, "Allow more access?") ||
			!strings.Contains(body, permissionScope) || !strings.Contains(body, "No new login and no OTP") {
			t.Fatalf("consent page: HTTP %d %.300s", r.Status, body)
		}
		for header, want := range map[string]string{
			"Cache-Control": "no-store", "X-Frame-Options": "DENY", "Referrer-Policy": "same-origin",
		} {
			if got := r.Header.Get(header); got != want {
				t.Errorf("%s %q, want %q", header, got, want)
			}
		}
		csp := r.Header.Get("Content-Security-Policy")
		if !strings.Contains(csp, "frame-ancestors 'none'") || strings.Contains(csp, "script-src") {
			t.Errorf("Content-Security-Policy %q", csp)
		}
		if strings.Contains(body, f.idt.Str("sub")) {
			t.Error("the consent page shows the user's sub")
		}
		record(t, "R28", r.Status, client.OAuthError{}, "consent page shown without a login")
	})

	t.Run("declined", func(t *testing.T) {
		a := askConsent(t, c, f.exchange(f.child))
		r := answerConsent(t, c, a.ConsentURI, "deny")
		if r.Status != http.StatusSeeOther ||
			r.Header.Get("Location") != f.child.RedirectURI+"?native_sso_consent=denied" {
			t.Fatalf("decline: HTTP %d Location %q", r.Status, r.Header.Get("Location"))
		}
		record(t, "R28", r.Status, client.OAuthError{}, "declined: "+r.Header.Get("Location"))
		req := f.exchange(f.child)
		req.Scope = consentScope
		expectError(t, "R28", doExchange(t, c, req), http.StatusBadRequest, "interaction_required")
	})

	t.Run("cross-site form refused", func(t *testing.T) {
		a := askConsent(t, c, f.exchange(f.child))
		u, _ := url.Parse(a.ConsentURI)
		form := url.Values{"ticket": {u.Query().Get("ticket")}, "decision": {"allow"}}
		r, err := c.PostForm(c.Issuer+"/oauth2/native-sso/consent", form,
			http.Header{"Origin": {"https://attacker.example"}})
		if err != nil {
			t.Fatal(err)
		}
		if r.Status != http.StatusForbidden {
			t.Errorf("cross-site POST: HTTP %d, want 403", r.Status)
		}
	})

	t.Run("the app cannot answer for the user", func(t *testing.T) {
		a := askConsent(t, c, f.exchange(f.child))
		app := newClient(t)
		page, err := app.Get(a.ConsentURI, nil)
		if err != nil {
			t.Fatal(err)
		}
		if page.Status != http.StatusForbidden || strings.Contains(string(page.Body), "consent-allow") {
			t.Errorf("consent page outside the login browser: HTTP %d %.200s", page.Status, page.Body)
		}
		r := answerConsent(t, app, a.ConsentURI, "allow")
		if r.Status != http.StatusForbidden {
			t.Fatalf("allow outside the login browser: HTTP %d Location %q, want 403", r.Status,
				r.Header.Get("Location"))
		}
		record(t, "R28", r.Status, client.OAuthError{}, "allow posted by the app itself refused")
		req := f.exchange(f.child)
		req.Scope = consentScope
		expectError(t, "R28", doExchange(t, c, req), http.StatusBadRequest, "interaction_required")
		// The ticket was not used up: the user can still answer it in the browser.
		if d := answerConsent(t, c, a.ConsentURI, "deny"); d.Status != http.StatusSeeOther {
			t.Errorf("user's answer after the app's attempt: HTTP %d, want 303", d.Status)
		}
	})

	t.Run("allowed", func(t *testing.T) {
		a := askConsent(t, c, f.exchange(f.child))
		r := answerConsent(t, c, a.ConsentURI, "allow")
		if r.Status != http.StatusSeeOther ||
			r.Header.Get("Location") != f.child.RedirectURI+"?native_sso_consent=granted" {
			t.Fatalf("allow: HTTP %d Location %q %s", r.Status, r.Header.Get("Location"), r.Body)
		}
		record(t, "R28", r.Status, client.OAuthError{}, "allowed: "+r.Header.Get("Location"))
		if again := answerConsent(t, c, a.ConsentURI, "allow"); again.Status != http.StatusBadRequest {
			t.Errorf("second answer with the same ticket: HTTP %d, want 400", again.Status)
		}

		req := f.exchange(f.child)
		req.Scope = consentScope
		out := expectExchangeOK(t, "R28", doExchange(t, c, req))
		if out.Scope != consentScope {
			t.Errorf("scope %q, want %q", out.Scope, consentScope)
		}
		if got, want := cnfJKT(t, out.AccessToken), f.hubDPoP.Thumbprint(); got != want {
			t.Errorf("cnf.jkt %q, want the Hub key %q", got, want)
		}
	})

	t.Run("approval needs every binding check", func(t *testing.T) {
		other, _ := client.NewECKey()
		req := f.exchange(f.child)
		req.Scope = consentScope
		req.DPoPKey = other
		expectError(t, "R28", doExchange(t, c, req), http.StatusBadRequest, "invalid_grant")
	})

	t.Run("new user data needs a login", func(t *testing.T) {
		req := f.exchange(f.child)
		req.Scope = "openid email phone"
		r := doExchange(t, c, req)
		expectError(t, "R28", r, http.StatusBadRequest, "interaction_required")
		if a := consentOf(t, r); a.ConsentURI != "" {
			t.Errorf("consent_uri for phone, which needs new user data: %s", a.ConsentURI)
		}
	})

	t.Run("scope not allowed for the app", func(t *testing.T) {
		plain := mustRegisterNative(t, c, f.rp, "childapp", nil)
		req := f.exchange(plain)
		req.Scope = consentScope
		r := doExchange(t, c, req)
		expectError(t, "R28", r, http.StatusBadRequest, "interaction_required")
		if a := consentOf(t, r); a.ConsentURI != "" {
			t.Errorf("consent_uri for a scope the app may not ask for: %s", a.ConsentURI)
		}
	})

	t.Run("approval is per login session", func(t *testing.T) {
		tok2, err := c.Login(client.LoginRequest{Reg: f.hub, Scope: hubScope, User: seededUser, DPoPKey: f.hubDPoP})
		if err != nil {
			t.Fatal(err)
		}
		req := client.ExchangeRequest{Reg: f.child, SubjectToken: tok2.IDToken, ActorToken: tok2.DeviceSecret,
			DPoPKey: f.hubDPoP, Scope: consentScope}
		r := doExchange(t, c, req)
		expectError(t, "R28", r, http.StatusBadRequest, "interaction_required")
		if consentOf(t, r).ConsentURI == "" {
			t.Error("a new login session should be asked again")
		}
	})

	t.Run("revoked device secret ends the approval", func(t *testing.T) {
		r, err := c.Revoke(f.hub, f.tok.DeviceSecret, client.TokenTypeHintDeviceSecret)
		if err != nil {
			t.Fatal(err)
		}
		if r.Status != http.StatusOK {
			t.Fatalf("revoke: HTTP %d %s", r.Status, r.Body)
		}
		req := f.exchange(f.child)
		req.Scope = consentScope
		expectError(t, "R28", doExchange(t, c, req), http.StatusBadRequest, "invalid_grant")
	})
}
