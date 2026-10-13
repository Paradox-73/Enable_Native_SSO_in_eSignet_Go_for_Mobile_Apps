// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package nativesso

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"

	"github.com/thunder-id/thunderid/internal/oauth/oauth2/constants"
	"github.com/thunder-id/thunderid/pkg/thunderidengine/providers"
)

// consentBrowserCookiePrefix prefixes the cookie that marks the browser of a device_sso login.
const consentBrowserCookiePrefix = "tid_nsso_"

// consentBrowserKeyPrefix is the key prefix of the browser records inside
// providers.NamespaceNativeSSOSession, next to the consent tickets and approvals.
const consentBrowserKeyPrefix = "consent-browser:"

// consentBrowserBytes is the number of random bytes in the cookie value: 256 bits, as for a ticket.
const consentBrowserBytes = 32

// ConsentBrowserBinding marks the browser in which the user logged in to the first app, so that the
// consent page can tell the user's answer from an answer posted by the app that asked for more scope.
//
// The consent_uri goes to the second app, which can read it and post the form itself. What the app cannot
// do is send a cookie that the authorization endpoint set in the system browser during the first app's
// login: an app's own HTTP client and its web views have their own cookie stores, while a Custom Tab
// shares the browser's. So the consent page shows the choice and records an answer only when the request
// carries the cookie of the login the device secret belongs to (its auth_id).
type ConsentBrowserBinding struct {
	store      providers.RuntimeStoreProvider
	secure     bool
	ttlSeconds int64
}

// NewConsentBrowserBinding returns the binding. secure sets the Secure attribute of the cookie (true
// behind TLS); ttlSeconds is how long the browser can answer, normally the device secret lifetime.
func NewConsentBrowserBinding(store providers.RuntimeStoreProvider, secure bool,
	ttlSeconds int64) *ConsentBrowserBinding {
	return &ConsentBrowserBinding{store: store, secure: secure, ttlSeconds: ttlSeconds}
}

// Bind marks the browser of the login authID: it stores the hash of a new random value and sets the
// value as an HttpOnly cookie that only the consent page receives. Call it on the authorization
// endpoint's answer to the browser, before any device secret exists.
func (b *ConsentBrowserBinding) Bind(ctx context.Context, w http.ResponseWriter, authID string) error {
	if authID == "" {
		return errors.New("consent browser binding needs an auth id")
	}
	if b.ttlSeconds <= 0 {
		return fmt.Errorf("consent browser binding lifetime must be positive, got %d", b.ttlSeconds)
	}
	raw := make([]byte, consentBrowserBytes)
	if _, err := rand.Read(raw); err != nil {
		return fmt.Errorf("failed to generate consent browser value: %w", err)
	}
	value := base64.RawURLEncoding.EncodeToString(raw)
	sum := sha256.Sum256([]byte(value))
	if err := b.store.Put(ctx, providers.NamespaceNativeSSOSession, browserKey(authID), sum[:],
		b.ttlSeconds); err != nil {
		return fmt.Errorf("failed to store consent browser binding: %w", err)
	}
	http.SetCookie(w, &http.Cookie{
		Name:     ConsentBrowserCookieName(authID),
		Value:    value,
		Path:     constants.OAuth2NativeSSOConsentEndpoint,
		MaxAge:   int(b.ttlSeconds),
		HttpOnly: true,
		Secure:   b.secure,
		// Lax: the browser sends it when the app opens the consent page (a top-level navigation) and on
		// the page's own form post, never on a request another site starts in the background.
		SameSite: http.SameSiteLaxMode,
	})
	return nil
}

// Present reports whether r comes from the browser marked for the login authID.
func (b *ConsentBrowserBinding) Present(ctx context.Context, r *http.Request, authID string) (bool, error) {
	if authID == "" {
		return false, nil
	}
	cookie, err := r.Cookie(ConsentBrowserCookieName(authID))
	if err != nil || cookie.Value == "" {
		return false, nil
	}
	want, err := b.store.Get(ctx, providers.NamespaceNativeSSOSession, browserKey(authID))
	if err != nil {
		return false, fmt.Errorf("failed to read consent browser binding: %w", err)
	}
	if len(want) != sha256.Size {
		return false, nil
	}
	got := sha256.Sum256([]byte(cookie.Value))
	return subtle.ConstantTimeCompare(got[:], want) == 1, nil
}

// ConsentBrowserCookieName returns the cookie name for the login authID. Each login has its own cookie,
// so a second login in the same browser does not replace the first one's; the auth id is hashed so it
// does not appear in the name.
func ConsentBrowserCookieName(authID string) string {
	sum := sha256.Sum256([]byte(authID))
	return consentBrowserCookiePrefix + hex.EncodeToString(sum[:])[:16]
}

func browserKey(authID string) string {
	sum := sha256.Sum256([]byte(authID))
	return consentBrowserKeyPrefix + base64.RawURLEncoding.EncodeToString(sum[:])
}
