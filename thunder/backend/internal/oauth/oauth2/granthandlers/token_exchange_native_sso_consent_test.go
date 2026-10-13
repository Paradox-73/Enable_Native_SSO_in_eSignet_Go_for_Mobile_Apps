// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package granthandlers

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/thunder-id/thunderid/internal/oauth/oauth2/constants"
	"github.com/thunder-id/thunderid/internal/oauth/oauth2/nativesso"
)

// Consent-only re-consent of the Native SSO token exchange (bonus B1; rule R28): extra scopes that
// need no new user data get a consent_uri, and an approval recorded on that page lets a later exchange
// of the same login session issue them.

const (
	nssoPermissionScope = "health_card_share"
	nssoChildRedirect   = "org.example.card://oauth/callback"
	nssoTicketValue     = "ticket-value"
)

var errTestStore = errors.New("store unavailable")

// fakeConsentStore records what the grant handler asks of the consent store.
type fakeConsentStore struct {
	issued     []nativesso.ConsentTicket
	issuedTTL  []int64
	issueErr   error
	granted    []string
	grantedErr error
	grantedFor []string
}

func (f *fakeConsentStore) IssueTicket(_ context.Context, ticket nativesso.ConsentTicket, ttl int64) (string, error) {
	if f.issueErr != nil {
		return "", f.issueErr
	}
	f.issued = append(f.issued, ticket)
	f.issuedTTL = append(f.issuedTTL, ttl)
	return nssoTicketValue, nil
}

func (f *fakeConsentStore) PeekTicket(context.Context, string) (*nativesso.ConsentTicket, error) {
	return nil, nil
}

func (f *fakeConsentStore) TakeTicket(context.Context, string) (*nativesso.ConsentTicket, error) {
	return nil, nil
}

func (f *fakeConsentStore) GrantedScopes(_ context.Context, sessionID, clientID, subject string) ([]string, error) {
	f.grantedFor = []string{sessionID, clientID, subject}
	return f.granted, f.grantedErr
}

func (f *fakeConsentStore) RecordGrant(context.Context, string, string, string, []string, int64) error {
	return nil
}

// enableConsentOnly turns the feature on with a fake store and lets the second app hold the permission
// scope and one redirect URI.
func (s *TokenExchangeNativeSSOTestSuite) enableConsentOnly() *fakeConsentStore {
	store := &fakeConsentStore{}
	s.handler.consentStore = store
	s.handler.cfg.OAuth.NativeSSO.ConsentOnlyReconsent = true
	s.handler.cfg.BaseURL = "https://esignet.example"
	s.childApp.Scopes = append(s.childApp.Scopes, "phone", nssoPermissionScope)
	s.childApp.RedirectURIs = []string{nssoChildRedirect}
	return store
}

// A permission scope that releases no user claim gets a consent_uri; the ticket names the two apps,
// the user, the session and only the missing scope, and lives five minutes.
func (s *TokenExchangeNativeSSOTestSuite) TestNativeSSO_Consent_PermissionScopeGetsConsentURI() {
	store := s.enableConsentOnly()
	s.request.Scope = "openid email " + nssoPermissionScope
	s.expectChecksPass()

	_, errResp := s.handle()

	s.assertError(errResp, constants.ErrorInteractionRequired)
	s.Equal("https://esignet.example"+constants.OAuth2NativeSSOConsentEndpoint+"?ticket="+nssoTicketValue,
		errResp.ConsentURI)
	s.Equal(int64(300), errResp.ConsentExpiresIn)
	s.Contains(errResp.ErrorDescription, "consent_uri")
	s.Contains(errResp.ErrorDescription, nssoPermissionScope)
	s.Require().Len(store.issued, 1)
	ticket := store.issued[0]
	s.Equal(nssoChildClientID, ticket.ClientID)
	s.Equal(nssoHubClientID, ticket.FirstClientID)
	s.Equal(testUserID, ticket.Subject)
	s.Equal(nssoSessionID, ticket.SessionID)
	s.Equal(nativeSSOAuthID, ticket.AuthID, "the consent page answers only in the browser of this login")
	s.Equal([]string{nssoPermissionScope}, ticket.Scopes)
	s.Equal(nssoChildRedirect, ticket.RedirectURI)
	s.Equal(s.dsClaims.ExpiresAt, ticket.GrantExpiresAt)
	s.Equal([]int64{300}, store.issuedTTL)
}

// A scope whose claims the login did not release (phone) needs a new login: no consent_uri.
func (s *TokenExchangeNativeSSOTestSuite) TestNativeSSO_Consent_NewClaimsNeedLogin() {
	store := s.enableConsentOnly()
	s.request.Scope = "openid phone " + nssoPermissionScope
	s.expectChecksPass()

	_, errResp := s.handle()

	s.assertError(errResp, constants.ErrorInteractionRequired)
	s.Empty(errResp.ConsentURI)
	s.Contains(errResp.ErrorDescription, "phone "+nssoPermissionScope)
	s.Empty(store.issued)
}

// A scope the second app is not allowed is never offered on the consent page.
func (s *TokenExchangeNativeSSOTestSuite) TestNativeSSO_Consent_ScopeNotAllowedForApp() {
	store := s.enableConsentOnly()
	s.request.Scope = "openid other_permission"
	s.expectChecksPass()

	_, errResp := s.handle()

	s.assertError(errResp, constants.ErrorInteractionRequired)
	s.Empty(errResp.ConsentURI)
	s.Empty(store.issued)
}

// With the setting off, or without a store, the answer is the plain re-consent signal.
func (s *TokenExchangeNativeSSOTestSuite) TestNativeSSO_Consent_OffGivesPlainSignal() {
	s.enableConsentOnly()
	s.handler.cfg.OAuth.NativeSSO.ConsentOnlyReconsent = false
	s.request.Scope = "openid " + nssoPermissionScope
	s.expectChecksPass()

	_, errResp := s.handle()

	s.assertError(errResp, constants.ErrorInteractionRequired)
	s.Empty(errResp.ConsentURI)

	s.handler.cfg.OAuth.NativeSSO.ConsentOnlyReconsent = true
	s.handler.consentStore = nil
	_, errResp = s.handle()
	s.assertError(errResp, constants.ErrorInteractionRequired)
	s.Empty(errResp.ConsentURI)
}

// A failure to store the ticket leaves consent_uri out; the app falls back to its own login.
func (s *TokenExchangeNativeSSOTestSuite) TestNativeSSO_Consent_TicketStoreFailure() {
	store := s.enableConsentOnly()
	store.issueErr = errTestStore
	s.request.Scope = "openid " + nssoPermissionScope
	s.expectChecksPass()

	_, errResp := s.handle()

	s.assertError(errResp, constants.ErrorInteractionRequired)
	s.Empty(errResp.ConsentURI)
}

// The ticket never outlives the device secret.
func (s *TokenExchangeNativeSSOTestSuite) TestNativeSSO_Consent_TicketTTLCappedByDeviceSecret() {
	store := s.enableConsentOnly()
	s.dsClaims.ExpiresAt = s.now.Add(90 * time.Second).Unix()
	s.request.Scope = "openid " + nssoPermissionScope
	s.expectChecksPass()

	_, errResp := s.handle()

	s.assertError(errResp, constants.ErrorInteractionRequired)
	s.Equal(int64(90), errResp.ConsentExpiresIn)
	s.Equal([]int64{90}, store.issuedTTL)
}

// With several registered redirect URIs none is chosen; the page then asks the user to go back.
func (s *TokenExchangeNativeSSOTestSuite) TestNativeSSO_Consent_SeveralRedirectURIs() {
	store := s.enableConsentOnly()
	s.childApp.RedirectURIs = []string{nssoChildRedirect, "https://card.example/cb"}
	s.request.Scope = "openid " + nssoPermissionScope
	s.expectChecksPass()

	_, errResp := s.handle()

	s.Require().NotNil(errResp)
	s.Require().Len(store.issued, 1)
	s.Empty(store.issued[0].RedirectURI)
}

// A device secret without an expiry cannot bound an approval: no ticket.
func (s *TokenExchangeNativeSSOTestSuite) TestNativeSSO_Consent_NoDeviceSecretExpiry() {
	store := s.enableConsentOnly()
	s.dsClaims.ExpiresAt = 0
	s.request.Scope = "openid " + nssoPermissionScope
	s.expectChecksPass()

	_, errResp := s.handle()

	s.assertError(errResp, constants.ErrorInteractionRequired)
	s.Empty(errResp.ConsentURI)
	s.Empty(store.issued)
}

// Without the auth_id of the login there is no browser to answer in, so no consent_uri is offered.
func (s *TokenExchangeNativeSSOTestSuite) TestNativeSSO_Consent_NoAuthID() {
	store := s.enableConsentOnly()
	s.dsClaims.AuthID = ""
	s.request.Scope = "openid " + nssoPermissionScope
	s.expectChecksPass()

	_, errResp := s.handle()

	s.assertError(errResp, constants.ErrorInteractionRequired)
	s.Empty(errResp.ConsentURI)
	s.Empty(store.issued)
}

// After the user allowed the scope, the same login session gets it in a normal exchange, with the
// first app's DPoP key, and the approval is read for this session, app and user.
func (s *TokenExchangeNativeSSOTestSuite) TestNativeSSO_Consent_ApprovedScopeIssued() {
	store := s.enableConsentOnly()
	store.granted = []string{nssoPermissionScope}
	s.request.Scope = "openid email " + nssoPermissionScope
	s.expectChecksPass()
	s.expectIssue()

	resp, errResp := s.handle()

	s.Require().Nil(errResp)
	s.Equal([]string{"openid", "email", nssoPermissionScope}, resp.AccessToken.Scopes)
	s.Equal(testDPoPThumbprint, s.builtAccess[0].DPoPJkt)
	s.Equal([]string{nssoSessionID, nssoChildClientID, testUserID}, store.grantedFor)
	s.Empty(store.issued)
}

// An approval stops counting as soon as the app is no longer allowed the scope.
func (s *TokenExchangeNativeSSOTestSuite) TestNativeSSO_Consent_ApprovalNeedsScopeStillAllowed() {
	store := s.enableConsentOnly()
	store.granted = []string{nssoPermissionScope}
	s.childApp.Scopes = []string{"openid", "profile", "email", constants.ScopeDeviceSSO}
	s.request.Scope = "openid " + nssoPermissionScope
	s.expectChecksPass()

	_, errResp := s.handle()

	s.assertError(errResp, constants.ErrorInteractionRequired)
	s.Empty(errResp.ConsentURI)
}

// An approval covers only what was approved: another extra scope still gets its own consent_uri.
func (s *TokenExchangeNativeSSOTestSuite) TestNativeSSO_Consent_ApprovalDoesNotCoverOtherScopes() {
	store := s.enableConsentOnly()
	store.granted = []string{nssoPermissionScope}
	s.childApp.Scopes = append(s.childApp.Scopes, "card_print")
	s.request.Scope = "openid " + nssoPermissionScope + " card_print"
	s.expectChecksPass()

	_, errResp := s.handle()

	s.assertError(errResp, constants.ErrorInteractionRequired)
	s.NotEmpty(errResp.ConsentURI)
	s.Require().Len(store.issued, 1)
	s.Equal([]string{"card_print"}, store.issued[0].Scopes)
	s.False(strings.Contains(errResp.ErrorDescription, nssoPermissionScope))
}

// A store that cannot be read stops the exchange: an approval must never be guessed.
func (s *TokenExchangeNativeSSOTestSuite) TestNativeSSO_Consent_GrantReadFailure() {
	store := s.enableConsentOnly()
	store.grantedErr = errTestStore
	s.request.Scope = "openid " + nssoPermissionScope
	s.expectChecksPass()

	_, errResp := s.handle()

	s.assertError(errResp, constants.ErrorServerError)
}

// The scope check stays after the binding checks: a request with a forged device secret never gets
// a consent ticket.
func (s *TokenExchangeNativeSSOTestSuite) TestNativeSSO_Consent_NoTicketBeforeBindingChecks() {
	store := s.enableConsentOnly()
	s.request.Scope = "openid " + nssoPermissionScope
	s.handler.now = func() time.Time { return s.now }
	s.dsClaims.DPoPJkt = "another-key"
	s.expectChecksPass()

	_, errResp := s.handle()

	s.assertError(errResp, constants.ErrorInvalidGrant)
	s.Empty(store.issued)
}
