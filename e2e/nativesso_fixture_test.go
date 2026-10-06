// SPDX-License-Identifier: MIT
// Copyright (c) 2026 Team Badmosip

//go:build e2e

package e2e

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/badmosip/esignet-native-sso/e2e/client"
	"github.com/badmosip/esignet-native-sso/e2e/internal/uuid"
)

// Shared set-up of the live Native SSO suite (design.md section 20, rules R01-R28).
//
// Environment, besides E2E_ISSUER and E2E_DIAL_OVERRIDE (e2e/run.sh sets all of these):
//
//	E2E_PHASE                            1 (default) or 2. Phase 2 runs after eSignet was restarted,
//	                                     which empties its in-memory runtime store.
//	E2E_STATE_FILE                       phase 1 writes the Hub session there, phase 2 reads it
//	E2E_EVIDENCE_DIR                     one JSON line per checked answer goes to evidence.jsonl there
//	E2E_DEVICE_SECRET_VALIDITY_SECONDS   device secret lifetime eSignet runs with in phase 2

const (
	// hubScope is what the Hub (Super App) asks for. A Child App may ask for less (R17); phone is
	// left out so that asking for it is a wider scope (R15).
	hubScope = "openid profile email " + client.ScopeDeviceSSO
	// childNarrowScope is narrower than hubScope: its userinfo must not show profile claims.
	childNarrowScope = "openid email"
	// jwtLeewaySeconds is eSignet's jwt.leeway (data/deployment.yaml).
	jwtLeewaySeconds = 10
)

// nativeClaims are the user claims every Native SSO test client registers.
var nativeClaims = []string{"name", "gender", "birthdate", "email", "phone_number"}

// nativeFixture is one Hub login with device_sso, shared by the phase-1 tests: Hub and Health
// Card are clients of one relying party, both allowed device_sso and the token-exchange grant.
type nativeFixture struct {
	rp      string
	hub     client.Registration
	child   client.Registration
	hubDPoP *client.ECKey
	tok     *client.Tokens
	idt     *client.JWT
}

var (
	fixtureOnce sync.Once
	fixture     *nativeFixture
	fixtureErr  error

	secondOnce sync.Once
	second     *client.Tokens
	secondErr  error
)

// nativeSSO returns the shared Hub login, making it on first use.
func nativeSSO(t *testing.T) *nativeFixture {
	t.Helper()
	fixtureOnce.Do(func() { fixture, fixtureErr = newNativeFixture(newClient(t)) })
	if fixtureErr != nil {
		t.Fatalf("Native SSO set-up: %v", fixtureErr)
	}
	return fixture
}

func newNativeFixture(c *client.Client) (*nativeFixture, error) {
	c.Logf = nil // the client outlives the test that made it
	f := &nativeFixture{rp: "ps03-vendor-" + uuid.New()[:8]}
	var err error
	if f.hub, err = registerNative(c, f.rp, "superapp", nil); err != nil {
		return nil, err
	}
	if f.child, err = registerNative(c, f.rp, "childapp", nil); err != nil {
		return nil, err
	}
	if f.hubDPoP, err = client.NewECKey(); err != nil {
		return nil, err
	}
	if f.tok, err = c.Login(client.LoginRequest{Reg: f.hub, Scope: hubScope, User: seededUser, DPoPKey: f.hubDPoP}); err != nil {
		return nil, fmt.Errorf("Hub login with device_sso: %w", err)
	}
	if f.tok.DeviceSecret == "" {
		return nil, fmt.Errorf("Hub login returned no device_secret (scope %q)", f.tok.Scope)
	}
	if f.idt, err = client.ParseJWT(f.tok.IDToken); err != nil {
		return nil, fmt.Errorf("Hub id_token: %w", err)
	}
	return f, saveState(f)
}

// secondHubLogin is another login of the same Hub client with the same DPoP key, for the tests
// that mix the tokens of two logins.
func secondHubLogin(t *testing.T, f *nativeFixture) *client.Tokens {
	t.Helper()
	secondOnce.Do(func() {
		c := newClient(t)
		c.Logf = nil
		second, secondErr = c.Login(client.LoginRequest{Reg: f.hub, Scope: hubScope, User: seededUser, DPoPKey: f.hubDPoP})
	})
	if secondErr != nil {
		t.Fatalf("second Hub login: %v", secondErr)
	}
	return second
}

// registerNative registers a client of relying party rp allowed device_sso and the token-exchange
// grant; mod may change the registration first.
func registerNative(c *client.Client, rp, app string, mod func(*client.Registration)) (client.Registration, error) {
	key, err := client.NewECKey()
	if err != nil {
		return client.Registration{}, err
	}
	reg := client.Registration{
		ClientID:       "e2e-" + app + "-" + uuid.New()[:8],
		RelyingPartyID: rp,
		RedirectURI:    "org.badmosip.nativesso." + app + "://oauth/callback",
		Key:            key,
		UserClaims:     nativeClaims,
		ACRs:           []string{otpACR},
		GrantTypes:     []string{"authorization_code", client.GrantTypeTokenExchange},
		AllowedScopes:  []string{client.ScopeDeviceSSO},
	}
	if mod != nil {
		mod(&reg)
	}
	return reg, c.RegisterClient(reg)
}

func mustRegisterNative(t *testing.T, c *client.Client, rp, app string, mod func(*client.Registration)) client.Registration {
	t.Helper()
	reg, err := registerNative(c, rp, app, mod)
	if err != nil {
		t.Fatal(err)
	}
	return reg
}

// exchange is the Health Card's token exchange with the Hub's id_token, device secret and DPoP key.
func (f *nativeFixture) exchange(reg client.Registration) client.ExchangeRequest {
	return client.ExchangeRequest{Reg: reg, SubjectToken: f.tok.IDToken, ActorToken: f.tok.DeviceSecret, DPoPKey: f.hubDPoP}
}

func doExchange(t *testing.T, c *client.Client, req client.ExchangeRequest) *client.Response {
	t.Helper()
	r, err := c.TokenExchange(req)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// expectError checks an error answer and records it as evidence for rule.
func expectError(t *testing.T, rule string, r *client.Response, status int, code string) client.OAuthError {
	t.Helper()
	e := client.ErrorOf(r)
	record(t, rule, r.Status, e, "")
	if r.Status != status || e.Error != code {
		t.Errorf("%s: HTTP %d error %q (%s), want HTTP %d error %q", rule, r.Status, e.Error, e.Description, status, code)
	} else {
		t.Logf("%s: HTTP %d %s: %s", rule, r.Status, e.Error, e.Description)
	}
	return e
}

// expectExchangeOK checks a successful token-exchange answer and records it for rule.
func expectExchangeOK(t *testing.T, rule string, r *client.Response) *client.ExchangeResponse {
	t.Helper()
	if r.Status != 200 {
		e := client.ErrorOf(r)
		record(t, rule, r.Status, e, "")
		t.Fatalf("%s: HTTP %d error %q (%s), want 200", rule, r.Status, e.Error, e.Description)
	}
	var out client.ExchangeResponse
	if err := r.JSON(&out); err != nil {
		t.Fatal(err)
	}
	record(t, rule, r.Status, client.OAuthError{}, "scope="+out.Scope+" token_type="+out.TokenType)
	return &out
}

var evidenceMu sync.Mutex

// record appends one line to $E2E_EVIDENCE_DIR/evidence.jsonl: the test, the rule, the HTTP status
// and the OAuth error. Tokens are never written.
func record(t *testing.T, rule string, status int, e client.OAuthError, note string) {
	dir := os.Getenv("E2E_EVIDENCE_DIR")
	if dir == "" {
		return
	}
	line, err := json.Marshal(map[string]any{
		"time": time.Now().UTC().Format(time.RFC3339), "test": t.Name(), "rule": rule, "status": status,
		"error": e.Error, "error_description": e.Description, "note": note,
	})
	if err != nil {
		return
	}
	evidenceMu.Lock()
	defer evidenceMu.Unlock()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Logf("evidence: %v", err)
		return
	}
	fh, err := os.OpenFile(filepath.Join(dir, "evidence.jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		t.Logf("evidence: %v", err)
		return
	}
	defer func() { _ = fh.Close() }()
	_, _ = fh.Write(append(line, '\n'))
}

// requirePhase skips a test that belongs to another phase of e2e/run.sh.
func requirePhase(t *testing.T, phase string) {
	t.Helper()
	if envOr("E2E_PHASE", "1") != phase {
		t.Skipf("phase %s test (E2E_PHASE=%s)", phase, envOr("E2E_PHASE", "1"))
	}
}

// savedRegistration and savedState carry the phase-1 Hub session to phase 2. The keys are made by
// the test run and registered only on this throw-away stack.
type savedRegistration struct {
	ClientID, RelyingPartyID, RedirectURI, Key string
}

type savedState struct {
	Hub, Child   savedRegistration
	HubDPoP      string
	IDToken      string
	DeviceSecret string
}

func saveReg(r client.Registration) (savedRegistration, error) {
	k, err := r.Key.MarshalPrivate()
	return savedRegistration{ClientID: r.ClientID, RelyingPartyID: r.RelyingPartyID, RedirectURI: r.RedirectURI, Key: k}, err
}

func (s savedRegistration) registration() (client.Registration, error) {
	k, err := client.ParseECKey(s.Key)
	return client.Registration{ClientID: s.ClientID, RelyingPartyID: s.RelyingPartyID, RedirectURI: s.RedirectURI,
		Key: k, UserClaims: nativeClaims, ACRs: []string{otpACR}}, err
}

func saveState(f *nativeFixture) error {
	path := os.Getenv("E2E_STATE_FILE")
	if path == "" {
		return nil
	}
	hub, err := saveReg(f.hub)
	if err != nil {
		return err
	}
	child, err := saveReg(f.child)
	if err != nil {
		return err
	}
	dpop, err := f.hubDPoP.MarshalPrivate()
	if err != nil {
		return err
	}
	b, err := json.Marshal(savedState{Hub: hub, Child: child, HubDPoP: dpop, IDToken: f.tok.IDToken, DeviceSecret: f.tok.DeviceSecret})
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o600)
}

// loadState reads the phase-1 Hub session.
func loadState(t *testing.T) (*nativeFixture, error) {
	t.Helper()
	path := os.Getenv("E2E_STATE_FILE")
	if path == "" {
		return nil, fmt.Errorf("E2E_STATE_FILE is not set")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("phase-1 state: %w", err)
	}
	var s savedState
	if err := json.Unmarshal(b, &s); err != nil {
		return nil, err
	}
	f := &nativeFixture{tok: &client.Tokens{IDToken: s.IDToken, DeviceSecret: s.DeviceSecret}}
	if f.hub, err = s.Hub.registration(); err != nil {
		return nil, err
	}
	if f.child, err = s.Child.registration(); err != nil {
		return nil, err
	}
	if f.hubDPoP, err = client.ParseECKey(s.HubDPoP); err != nil {
		return nil, err
	}
	f.rp = f.hub.RelyingPartyID
	return f, nil
}

// claimNames lists a JWT's claim names, sorted.
func claimNames(j *client.JWT) []string {
	names := make([]string, 0, len(j.Claims))
	for k := range j.Claims {
		names = append(names, k)
	}
	slices.Sort(names)
	return names
}

// hasClaim reports a claim by its base name: language variants such as name#eng count as name.
func hasClaim(j *client.JWT, name string) bool {
	for k := range j.Claims {
		if base, _, _ := strings.Cut(k, "#"); base == name {
			return true
		}
	}
	return false
}

func cnfJKT(t *testing.T, accessToken string) string {
	t.Helper()
	at, err := client.ParseJWT(accessToken)
	if err != nil {
		t.Fatalf("access token is not a JWT: %v", err)
	}
	cnf, _ := at.Claims["cnf"].(map[string]any)
	jkt, _ := cnf["jkt"].(string)
	return jkt
}
