// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package nativesso

import (
	"context"
	"crypto/sha256"
	"net/http"
	"net/http/httptest"
	"strings"
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
	b := NewConsentBrowserBinding(rs, true, 28800)

	cookie := bindBrowser(t, b, "auth-1")

	assert.Equal(t, ConsentBrowserCookieName("auth-1"), cookie.Name)
	assert.True(t, strings.HasPrefix(cookie.Name, "tid_nsso_"))
	assert.NotContains(t, cookie.Name, "auth-1", "the auth id is not in the name")
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

func TestConsentBrowser_InsecureCookieOnlyWhenAsked(t *testing.T) {
	cookie := bindBrowser(t, NewConsentBrowserBinding(newFakeRuntimeStore(), false, 60), "auth-1")
	assert.False(t, cookie.Secure)
}

func TestConsentBrowser_PresentOnlyForTheBoundBrowser(t *testing.T) {
	ctx := context.Background()
	b := NewConsentBrowserBinding(newFakeRuntimeStore(), true, 28800)
	first := bindBrowser(t, b, "auth-1")
	second := bindBrowser(t, b, "auth-2")
	assert.NotEqual(t, first.Name, second.Name, "each login has its own cookie")
	assert.NotEqual(t, first.Value, second.Value)

	cases := []struct {
		name    string
		req     *http.Request
		authID  string
		present bool
	}{
		{"cookie of the login", requestWith(first), "auth-1", true},
		{"both cookies, first login", requestWith(first, second), "auth-1", true},
		{"both cookies, second login", requestWith(first, second), "auth-2", true},
		{"no cookie", requestWith(), "auth-1", false},
		{"cookie of another login", requestWith(second), "auth-1", false},
		{"other login's value under this name", requestWith(&http.Cookie{
			Name: first.Name, Value: second.Value}), "auth-1", false},
		{"empty auth id", requestWith(first), "", false},
		{"login never bound", requestWith(&http.Cookie{Name: ConsentBrowserCookieName("auth-3"),
			Value: first.Value}), "auth-3", false},
	}
	for _, c := range cases {
		present, err := b.Present(ctx, c.req, c.authID)
		require.NoError(t, err, c.name)
		assert.Equal(t, c.present, present, c.name)
	}
}

func TestConsentBrowser_RebindReplacesTheOldValue(t *testing.T) {
	ctx := context.Background()
	b := NewConsentBrowserBinding(newFakeRuntimeStore(), true, 28800)
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
	b := NewConsentBrowserBinding(rs, true, 28800)

	assert.ErrorContains(t, b.Bind(ctx, httptest.NewRecorder(), ""), "auth id")
	assert.ErrorContains(t, NewConsentBrowserBinding(rs, true, 0).Bind(ctx, httptest.NewRecorder(), "a"),
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
