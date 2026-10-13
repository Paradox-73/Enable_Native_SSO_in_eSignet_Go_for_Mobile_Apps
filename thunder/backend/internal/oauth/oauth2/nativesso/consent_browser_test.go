// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package nativesso

import (
	"context"
	"crypto/sha256"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/thunder-id/thunderid/internal/oauth/oauth2/constants"
	"github.com/thunder-id/thunderid/pkg/thunderidengine/providers"
)

func bindBrowser(t *testing.T, b *ConsentBrowserBinding, authID string) *http.Cookie {
	t.Helper()
	rec := httptest.NewRecorder()
	require.NoError(t, b.Bind(context.Background(), rec, authID))
	cookies := rec.Result().Cookies()
	require.Len(t, cookies, 1)
	return cookies[0]
}

func requestWith(cookies ...*http.Cookie) *http.Request {
	req := httptest.NewRequest(http.MethodGet, constants.OAuth2NativeSSOConsentEndpoint, nil)
	for _, c := range cookies {
		req.AddCookie(&http.Cookie{Name: c.Name, Value: c.Value})
	}
	return req
}

func TestConsentBrowser_BindSetsARestrictedCookie(t *testing.T) {
	rs := newFakeRuntimeStore()
	b := NewConsentBrowserBinding(rs, testBaseURL, 28800)

	cookie := bindBrowser(t, b, "auth-1")

	assert.Equal(t, "tid_nsso", cookie.Name, "one cookie per browser, not one per login")
	assert.Equal(t, constants.OAuth2NativeSSOConsentEndpoint, cookie.Path, "only the consent page receives it")
	assert.True(t, cookie.HttpOnly)
	assert.True(t, cookie.Secure)
	assert.Equal(t, http.SameSiteLaxMode, cookie.SameSite)
	assert.Equal(t, 28800, cookie.MaxAge)
	assert.Len(t, cookie.Value, 43, "32 random bytes, unpadded base64url")

	// Only the hash of the value is stored, under a key that does not show the auth id either.
	sum := sha256.Sum256([]byte(cookie.Value))
	key := string(providers.NamespaceNativeSSOSession) + "|" + browserKey("auth-1")
	assert.Equal(t, sum[:], rs.data[key])
	assert.Equal(t, int64(28800), rs.ttls[key])
	assert.NotContains(t, key, "auth-1")
	for _, v := range rs.data {
		assert.NotContains(t, string(v), cookie.Value)
	}
}

func TestConsentBrowser_InsecureCookieOnlyWithoutTLS(t *testing.T) {
	cookie := bindBrowser(t, NewConsentBrowserBinding(newFakeRuntimeStore(), "http://localhost:38088", 60), "auth-1")
	assert.False(t, cookie.Secure)
	assert.Equal(t, constants.OAuth2NativeSSOConsentEndpoint, cookie.Path)
}

// Behind a proxy that publishes the server under a prefix, the browser opens the consent page under that
// prefix, so the cookie path must carry it too.
func TestConsentBrowser_CookiePathKeepsTheBaseURLPrefix(t *testing.T) {
	for _, base := range []string{"https://host/v1/esignet", "https://host/v1/esignet/", "HTTPS://host/v1/esignet"} {
		cookie := bindBrowser(t, NewConsentBrowserBinding(newFakeRuntimeStore(), base, 60), "auth-1")
		assert.Equal(t, "/v1/esignet"+constants.OAuth2NativeSSOConsentEndpoint, cookie.Path, base)
		assert.True(t, cookie.Secure, base)
	}
}

func TestConsentBrowser_PresentOnlyForTheBoundBrowser(t *testing.T) {
	ctx := context.Background()
	b := NewConsentBrowserBinding(newFakeRuntimeStore(), testBaseURL, 28800)
	first := bindBrowser(t, b, "auth-1")
	// A second login in the same browser overwrites the cookie: same name, new value.
	second := bindBrowser(t, b, "auth-2")
	assert.Equal(t, first.Name, second.Name)
	assert.NotEqual(t, first.Value, second.Value)

	cases := []struct {
		name    string
		req     *http.Request
		authID  string
		present bool
	}{
		{"cookie of the login", requestWith(first), "auth-1", true},
		{"cookie of the latest login", requestWith(second), "auth-2", true},
		{"no cookie", requestWith(), "auth-1", false},
		{"cookie of another login", requestWith(second), "auth-1", false},
		{"empty auth id", requestWith(first), "", false},
		{"login never bound", requestWith(first), "auth-3", false},
	}
	for _, c := range cases {
		present, err := b.Present(ctx, c.req, c.authID)
		require.NoError(t, err, c.name)
		assert.Equal(t, c.present, present, c.name)
	}
}

func TestConsentBrowser_RebindReplacesTheOldValue(t *testing.T) {
	ctx := context.Background()
	b := NewConsentBrowserBinding(newFakeRuntimeStore(), testBaseURL, 28800)
	old := bindBrowser(t, b, "auth-1")
	fresh := bindBrowser(t, b, "auth-1")

	present, err := b.Present(ctx, requestWith(old), "auth-1")
	require.NoError(t, err)
	assert.False(t, present)
	present, err = b.Present(ctx, requestWith(fresh), "auth-1")
	require.NoError(t, err)
	assert.True(t, present)
}

func TestConsentBrowser_Errors(t *testing.T) {
	ctx := context.Background()
	rs := newFakeRuntimeStore()
	b := NewConsentBrowserBinding(rs, testBaseURL, 28800)

	assert.ErrorContains(t, b.Bind(ctx, httptest.NewRecorder(), ""), "auth id")
	assert.ErrorContains(t, NewConsentBrowserBinding(rs, testBaseURL, 0).Bind(ctx, httptest.NewRecorder(), "a"),
		"lifetime")

	rs.putErr = errFakeStore
	rec := httptest.NewRecorder()
	assert.ErrorIs(t, b.Bind(ctx, rec, "auth-1"), errFakeStore)
	assert.Empty(t, rec.Result().Cookies(), "no cookie without a stored record")

	rs.putErr = nil
	cookie := bindBrowser(t, b, "auth-1")
	rs.getErr = errFakeStore
	_, err := b.Present(ctx, requestWith(cookie), "auth-1")
	assert.ErrorIs(t, err, errFakeStore)
}
