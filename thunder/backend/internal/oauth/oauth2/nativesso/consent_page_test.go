// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package nativesso

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/thunder-id/thunderid/internal/oauth/oauth2/constants"
	tidcommon "github.com/thunder-id/thunderid/pkg/thunderidengine/common"
	"github.com/thunder-id/thunderid/pkg/thunderidengine/providers"
	"github.com/thunder-id/thunderid/tests/mocks/actorprovidermock"
)

const testBaseURL = "https://esignet.example"

type pageFixture struct {
	t       *testing.T
	rs      *fakeRuntimeStore
	store   ConsentStoreInterface
	browser *ConsentBrowserBinding
	actor   *actorprovidermock.ActorProviderMock
	handler *ConsentPageHandler
	mux     *http.ServeMux
	now     time.Time
	// cookie is what the browser of the login (auth-1) sends; nil plays any other client.
	cookie *http.Cookie
}

func newPageFixture(t *testing.T) *pageFixture {
	f := &pageFixture{t: t, rs: newFakeRuntimeStore(), actor: actorprovidermock.NewActorProviderMock(t)}
	f.store = NewConsentStore(f.rs)
	f.browser = NewConsentBrowserBinding(f.rs, testBaseURL, 28800)
	f.handler = NewConsentPageHandler(f.store, f.browser, f.actor, testBaseURL, nil)
	f.now = time.Unix(1900000000, 0)
	f.handler.now = func() time.Time { return f.now }
	f.mux = http.NewServeMux()
	RegisterConsentRoutes(f.mux, f.handler)
	f.cookie = f.loginBrowser("auth-1")
	return f
}

// loginBrowser marks a browser for the login authID, as the authorization endpoint does, and returns
// the cookie it would keep.
func (f *pageFixture) loginBrowser(authID string) *http.Cookie {
	rec := httptest.NewRecorder()
	require.NoError(f.t, f.browser.Bind(context.Background(), rec, authID))
	cookies := rec.Result().Cookies()
	require.Len(f.t, cookies, 1)
	return cookies[0]
}

func (f *pageFixture) addCookie(req *http.Request) {
	if f.cookie != nil {
		req.AddCookie(&http.Cookie{Name: f.cookie.Name, Value: f.cookie.Value})
	}
}

// expectNames lets the actor provider name both apps (the first one Hub).
func (f *pageFixture) expectNames(childName string) {
	f.actor.EXPECT().GetOAuthClientByClientID(mock.Anything, "health-card").Return(&providers.OAuthClient{
		ID: "health-card", ClientID: "health-card", ScopeClaims: map[string][]string{"email": {"email"}},
	}, nil).Maybe()
	f.actor.EXPECT().GetOAuthClientByClientID(mock.Anything, "hub").Return(&providers.OAuthClient{
		ID: "hub", ClientID: "hub",
	}, nil).Maybe()
	f.actor.EXPECT().GetInboundClientByID(mock.Anything, "health-card").Return(&providers.InboundClient{
		Properties: map[string]interface{}{"name": childName},
	}, nil).Maybe()
	f.actor.EXPECT().GetInboundClientByID(mock.Anything, "hub").Return(&providers.InboundClient{
		Properties: map[string]interface{}{"name": "Hub"},
	}, nil).Maybe()
}

func (f *pageFixture) issue(ticket ConsentTicket) string {
	value, err := f.store.IssueTicket(context.Background(), ticket, 300)
	require.NoError(f.t, err)
	return value
}

func (f *pageFixture) get(value string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, constants.OAuth2NativeSSOConsentEndpoint+"?ticket="+
		url.QueryEscape(value), nil)
	f.addCookie(req)
	rec := httptest.NewRecorder()
	f.mux.ServeHTTP(rec, req)
	return rec
}

func (f *pageFixture) post(form url.Values, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, constants.OAuth2NativeSSOConsentEndpoint,
		strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if headers == nil {
		headers = map[string]string{"Origin": testBaseURL}
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	f.addCookie(req)
	rec := httptest.NewRecorder()
	f.mux.ServeHTTP(rec, req)
	return rec
}

func assertPageHeaders(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	h := rec.Header()
	assert.Equal(t, "no-store", h.Get("Cache-Control"))
	assert.Equal(t, "DENY", h.Get("X-Frame-Options"))
	// no-referrer would make Chrome send "Origin: null" on the page's own form.
	assert.Equal(t, "same-origin", h.Get("Referrer-Policy"))
	assert.Equal(t, "nosniff", h.Get("X-Content-Type-Options"))
	csp := h.Get("Content-Security-Policy")
	assert.Contains(t, csp, "default-src 'none'")
	assert.Contains(t, csp, "frame-ancestors 'none'")
	assert.NotContains(t, csp, "script-src")
}

func TestConsentPage_ScopeDescriptions(t *testing.T) {
	f := newPageFixture(t)
	f.handler.descriptions = map[string]string{
		"health_card_share": "Share your <health> card with clinics",
		"email":             "  ",
	}
	f.expectNames("Health Card")
	ticket := testTicket()
	ticket.Scopes = []string{"health_card_share", "email", "other_scope"}
	value := f.issue(ticket)

	rec := f.get(value)

	require.Equal(t, http.StatusOK, rec.Code)
	assertPageHeaders(t, rec)
	assert.Contains(t, rec.Header().Get("Content-Security-Policy"), "form-action 'self' org.example.card:")
	body := rec.Body.String()
	assert.Contains(t, body,
		`<b>Share your &lt;health&gt; card with clinics</b> <span class="claims">(health_card_share)</span>`,
		"a configured description is shown, escaped, with the scope value in small text")
	assert.NotContains(t, body, "Permission: <b>health_card_share</b>")
	assert.Contains(t, body, "Share: email", "a blank description falls back to the claims line")
	assert.Contains(t, body, "Permission: <b>other_scope</b>", "no entry falls back to the raw value")
}

func TestConsentPage_GetShowsAppsAndScopes(t *testing.T) {
	f := newPageFixture(t)
	f.expectNames("Health <Card>")
	ticket := testTicket()
	ticket.FirstClientID = "hub"
	ticket.Scopes = []string{"health_card_share", "email"}
	value := f.issue(ticket)

	rec := f.get(value)

	require.Equal(t, http.StatusOK, rec.Code)
	assertPageHeaders(t, rec)
	assert.Contains(t, rec.Header().Get("Content-Security-Policy"), "form-action 'self' org.example.card:")
	body := rec.Body.String()
	assert.Contains(t, body, "Health &lt;Card&gt;", "names are escaped")
	assert.Contains(t, body, "<b>Hub</b>")
	assert.Contains(t, body, "Permission: <b>health_card_share</b>")
	assert.Contains(t, body, "Share: email")
	assert.Contains(t, body, "No new login and no OTP are needed")
	assert.Contains(t, body, `id="consent-allow"`)
	assert.Contains(t, body, `id="consent-deny"`)
	assert.Contains(t, body, `name="ticket" value="`+value+`"`)
	assert.NotContains(t, body, "psut-1", "no user data on the page")
	assert.NotContains(t, body, "<script")

	// Showing the page does not use the ticket up.
	assert.Equal(t, http.StatusOK, f.get(value).Code)
}

func TestConsentPage_GetUnknownOrMalformedTicket(t *testing.T) {
	f := newPageFixture(t)
	for _, value := range []string{"", "nope", strings.Repeat("A", 43)} {
		rec := f.get(value)
		assert.Equal(t, http.StatusBadRequest, rec.Code, value)
		assertPageHeaders(t, rec)
		assert.Contains(t, rec.Body.String(), "expired or was already answered")
	}
}

func TestConsentPage_GetStoreError(t *testing.T) {
	f := newPageFixture(t)
	value := f.issue(testTicket())
	f.rs.getErr = errFakeStore
	rec := f.get(value)
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Contains(t, rec.Body.String(), "Something went wrong")
}

func TestConsentPage_GetFallsBackToClientIDs(t *testing.T) {
	f := newPageFixture(t)
	f.actor.EXPECT().GetOAuthClientByClientID(mock.Anything, "health-card").
		Return(nil, &tidcommon.ServiceError{Type: tidcommon.ClientErrorType})
	f.actor.EXPECT().GetOAuthClientByClientID(mock.Anything, "hub").Return(&providers.OAuthClient{}, nil)
	f.actor.EXPECT().GetInboundClientByID(mock.Anything, "hub").
		Return(&providers.InboundClient{Properties: map[string]interface{}{"name": " "}}, nil)
	value := f.issue(testTicket())

	rec := f.get(value)

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "<b>health-card</b>")
	assert.Contains(t, rec.Body.String(), "<b>hub</b>")
}

func TestConsentPage_AllowRecordsGrantAndRedirects(t *testing.T) {
	f := newPageFixture(t)
	f.expectNames("Health Card")
	value := f.issue(testTicket())

	rec := f.post(url.Values{"ticket": {value}, "decision": {"allow"}},
		map[string]string{"Origin": testBaseURL, "Sec-Fetch-Site": "same-origin"})

	require.Equal(t, http.StatusSeeOther, rec.Code)
	assertPageHeaders(t, rec)
	assert.Equal(t, "org.example.card://oauth/callback?native_sso_consent=granted", rec.Header().Get("Location"))
	granted, err := f.store.GrantedScopes(context.Background(), "sid-1", "health-card", "psut-1")
	require.NoError(t, err)
	assert.Equal(t, []string{"health_card_share"}, granted)
	for k, ttl := range f.rs.ttls {
		if strings.Contains(k, "consent-grant:") {
			assert.Equal(t, int64(2000000000-1900000000), ttl, "the approval ends with the device secret")
		}
	}

	// The ticket is used up: a second submission changes nothing.
	again := f.post(url.Values{"ticket": {value}, "decision": {"allow"}}, nil)
	assert.Equal(t, http.StatusBadRequest, again.Code)
}

func TestConsentPage_DenyRecordsNothing(t *testing.T) {
	f := newPageFixture(t)
	f.expectNames("Health Card")
	ticket := testTicket()
	ticket.RedirectURI = "https://card.example/cb?x=1"
	value := f.issue(ticket)

	rec := f.post(url.Values{"ticket": {value}, "decision": {"deny"}}, map[string]string{"Origin": testBaseURL})

	require.Equal(t, http.StatusSeeOther, rec.Code)
	assert.Equal(t, "https://card.example/cb?native_sso_consent=denied&x=1", rec.Header().Get("Location"))
	assert.Contains(t, rec.Header().Get("Content-Security-Policy"), "form-action 'self' https://card.example")
	granted, err := f.store.GrantedScopes(context.Background(), "sid-1", "health-card", "psut-1")
	require.NoError(t, err)
	assert.Nil(t, granted)
}

func TestConsentPage_NoRedirectURIShowsMessage(t *testing.T) {
	f := newPageFixture(t)
	f.expectNames("Health Card")
	for decision, want := range map[string]string{"allow": "Health Card now has the permission",
		"deny": "Nothing changed. Go back to Health Card."} {
		ticket := testTicket()
		ticket.RedirectURI = ""
		value := f.issue(ticket)
		rec := f.post(url.Values{"ticket": {value}, "decision": {decision}}, nil)
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Contains(t, rec.Body.String(), want)
		assert.Contains(t, rec.Header().Get("Content-Security-Policy"), "form-action 'self'")
	}
}

func TestConsentPage_PostRejections(t *testing.T) {
	f := newPageFixture(t)
	value := f.issue(testTicket())

	cases := []struct {
		name    string
		form    url.Values
		headers map[string]string
		status  int
	}{
		{"other origin", url.Values{"ticket": {value}, "decision": {"allow"}},
			map[string]string{"Origin": "https://evil.example"}, http.StatusForbidden},
		{"cross-site fetch", url.Values{"ticket": {value}, "decision": {"allow"}},
			map[string]string{"Sec-Fetch-Site": "cross-site"}, http.StatusForbidden},
		{"same-site fetch", url.Values{"ticket": {value}, "decision": {"allow"}},
			map[string]string{"Sec-Fetch-Site": "same-site"}, http.StatusForbidden},
		{"neither Origin nor Sec-Fetch-Site", url.Values{"ticket": {value}, "decision": {"allow"}},
			map[string]string{}, http.StatusForbidden},
		{"unknown decision", url.Values{"ticket": {value}, "decision": {"maybe"}}, nil, http.StatusBadRequest},
		{"no decision", url.Values{"ticket": {value}}, nil, http.StatusBadRequest},
		{"malformed ticket", url.Values{"ticket": {"x"}, "decision": {"allow"}}, nil, http.StatusBadRequest},
	}
	for _, c := range cases {
		rec := f.post(c.form, c.headers)
		assert.Equal(t, c.status, rec.Code, c.name)
		assertPageHeaders(t, rec)
	}
	// None of them used the ticket up.
	peeked, err := f.store.PeekTicket(context.Background(), value)
	require.NoError(t, err)
	assert.NotNil(t, peeked)
}

func TestConsentPage_PostBodyTooLarge(t *testing.T) {
	f := newPageFixture(t)
	rec := f.post(url.Values{"ticket": {strings.Repeat("a", 5000)}, "decision": {"allow"}}, nil)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestConsentPage_PostExpiredGrant(t *testing.T) {
	f := newPageFixture(t)
	ticket := testTicket()
	ticket.GrantExpiresAt = f.now.Unix()
	value := f.issue(ticket)
	rec := f.post(url.Values{"ticket": {value}, "decision": {"allow"}}, nil)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "expired")
}

func TestConsentPage_PostStoreErrors(t *testing.T) {
	f := newPageFixture(t)
	value := f.issue(testTicket())
	f.rs.takeErr = errFakeStore
	rec := f.post(url.Values{"ticket": {value}, "decision": {"allow"}}, nil)
	assert.Equal(t, http.StatusInternalServerError, rec.Code)

	f.rs.takeErr = nil
	f.rs.putErr = errFakeStore
	rec = f.post(url.Values{"ticket": {value}, "decision": {"allow"}}, nil)
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestConsentPage_BadRegisteredRedirect(t *testing.T) {
	f := newPageFixture(t)
	f.expectNames("Health Card")
	ticket := testTicket()
	ticket.RedirectURI = "http://bad host/%zz"
	value := f.issue(ticket)
	rec := f.post(url.Values{"ticket": {value}, "decision": {"deny"}}, nil)
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestConsentPage_SameOriginWithBadBaseURL(t *testing.T) {
	h := NewConsentPageHandler(nil, nil, nil, "not a url", nil)
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Header.Set("Origin", "https://esignet.example")
	assert.False(t, h.sameOrigin(req))
}

func TestRedirectSource(t *testing.T) {
	assert.Equal(t, "", redirectSource(""))
	assert.Equal(t, "org.example.card:", redirectSource("org.example.card://oauth/callback"))
	assert.Equal(t, "https://card.example", redirectSource("https://card.example/cb"))
	assert.Equal(t, "http://10.0.2.2:8088", redirectSource("HTTP://10.0.2.2:8088/cb"))
	assert.Equal(t, "", redirectSource("https:///nohost"))
	assert.Equal(t, "", redirectSource("1bad://x"))
	assert.Equal(t, "", redirectSource("%zz"))
}

func TestConsentPage_NilActorProvider(t *testing.T) {
	h := NewConsentPageHandler(nil, nil, nil, testBaseURL, nil)
	assert.Nil(t, h.client(context.Background(), "x"))
	assert.Equal(t, "x", h.clientName(context.Background(), nil, "x"))
}

func TestConsentPage_SameOriginFetchWithoutOrigin(t *testing.T) {
	f := newPageFixture(t)
	f.expectNames("Health Card")
	value := f.issue(testTicket())
	rec := f.post(url.Values{"ticket": {value}, "decision": {"allow"}},
		map[string]string{"Sec-Fetch-Site": "same-origin"})
	assert.Equal(t, http.StatusSeeOther, rec.Code)
}

// The app that receives the consent_uri can read the ticket and post the form itself, as a plain HTTP
// client or in its own web view. Neither has the cookie of the browser where the user logged in, so the
// page shows no choice, records nothing and leaves the ticket for the user.
func TestConsentPage_OnlyTheLoginBrowserCanAnswer(t *testing.T) {
	cases := []struct {
		name   string
		cookie func(f *pageFixture) *http.Cookie
	}{
		{"no cookie (the app's own client)", func(*pageFixture) *http.Cookie { return nil }},
		{"cookie of another login", func(f *pageFixture) *http.Cookie { return f.loginBrowser("auth-2") }},
		{"forged value", func(f *pageFixture) *http.Cookie {
			return &http.Cookie{Name: ConsentBrowserCookieName, Value: "forged"}
		}},
		{"empty value", func(f *pageFixture) *http.Cookie {
			return &http.Cookie{Name: ConsentBrowserCookieName, Value: ""}
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newPageFixture(t)
			value := f.issue(testTicket())
			f.cookie = c.cookie(f)

			page := f.get(value)
			assert.Equal(t, http.StatusForbidden, page.Code)
			assertPageHeaders(t, page)
			assert.Contains(t, page.Body.String(), "only in the browser where you signed in")
			assert.NotContains(t, page.Body.String(), "consent-allow")

			answer := f.post(url.Values{"ticket": {value}, "decision": {"allow"}},
				map[string]string{"Origin": testBaseURL, "Sec-Fetch-Site": "same-origin"})
			assert.Equal(t, http.StatusForbidden, answer.Code)
			assert.Contains(t, answer.Body.String(), "only in the browser where you signed in")

			granted, err := f.store.GrantedScopes(context.Background(), "sid-1", "health-card", "psut-1")
			require.NoError(t, err)
			assert.Nil(t, granted, "nothing recorded")
			peeked, err := f.store.PeekTicket(context.Background(), value)
			require.NoError(t, err)
			assert.NotNil(t, peeked, "the ticket is left for the user")
		})
	}
}

func TestConsentPage_TicketWithoutAuthIDIsNotAnswered(t *testing.T) {
	f := newPageFixture(t)
	ticket := testTicket()
	ticket.AuthID = ""
	value := f.issue(ticket)
	assert.Equal(t, http.StatusForbidden, f.get(value).Code)
	assert.Equal(t, http.StatusForbidden, f.post(url.Values{"ticket": {value}, "decision": {"allow"}}, nil).Code)
}

func TestConsentPage_NoBrowserBindingAcceptsNothing(t *testing.T) {
	f := newPageFixture(t)
	f.handler.browser = nil
	value := f.issue(testTicket())
	assert.Equal(t, http.StatusForbidden, f.get(value).Code)
	assert.Equal(t, http.StatusForbidden, f.post(url.Values{"ticket": {value}, "decision": {"allow"}}, nil).Code)
}

func TestConsentPage_BrowserCheckStoreError(t *testing.T) {
	f := newPageFixture(t)
	value := f.issue(testTicket())
	// The ticket is read first, then the browser record: fail only the second read.
	f.rs.getErrAfter = 1
	f.rs.getErr = errFakeStore
	assert.Equal(t, http.StatusInternalServerError, f.get(value).Code)

	f.rs.getErrAfter = 1
	assert.Equal(t, http.StatusInternalServerError,
		f.post(url.Values{"ticket": {value}, "decision": {"allow"}}, nil).Code)
}

func TestConsentPage_PostPeekStoreError(t *testing.T) {
	f := newPageFixture(t)
	value := f.issue(testTicket())
	f.rs.getErr = errFakeStore
	rec := f.post(url.Values{"ticket": {value}, "decision": {"allow"}}, nil)
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}
