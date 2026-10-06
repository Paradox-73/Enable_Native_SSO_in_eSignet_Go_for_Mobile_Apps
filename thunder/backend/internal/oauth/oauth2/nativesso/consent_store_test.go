// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package nativesso

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/thunder-id/thunderid/internal/oauth/oauth2/constants"
	"github.com/thunder-id/thunderid/pkg/thunderidengine/providers"
)

var errFakeStore = errors.New("store down")

// fakeRuntimeStore is a map-backed runtime store with optional failures. Methods the consent store
// does not use panic through the nil embedded interface.
type fakeRuntimeStore struct {
	providers.RuntimeStoreProvider
	data    map[string][]byte
	ttls    map[string]int64
	putErr  error
	getErr  error
	takeErr error
}

func newFakeRuntimeStore() *fakeRuntimeStore {
	return &fakeRuntimeStore{data: map[string][]byte{}, ttls: map[string]int64{}}
}

func (f *fakeRuntimeStore) key(ns providers.RuntimeStoreNamespace, key string) string {
	return string(ns) + "|" + key
}

func (f *fakeRuntimeStore) Put(_ context.Context, ns providers.RuntimeStoreNamespace, key string, value []byte,
	ttl int64) error {
	if f.putErr != nil {
		return f.putErr
	}
	f.data[f.key(ns, key)] = value
	f.ttls[f.key(ns, key)] = ttl
	return nil
}

func (f *fakeRuntimeStore) Get(_ context.Context, ns providers.RuntimeStoreNamespace, key string) ([]byte, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	return f.data[f.key(ns, key)], nil
}

func (f *fakeRuntimeStore) Take(_ context.Context, ns providers.RuntimeStoreNamespace, key string) ([]byte, error) {
	if f.takeErr != nil {
		return nil, f.takeErr
	}
	v := f.data[f.key(ns, key)]
	delete(f.data, f.key(ns, key))
	return v, nil
}

func testTicket() ConsentTicket {
	return ConsentTicket{
		ClientID: "health-card", FirstClientID: "hub", Subject: "psut-1", SessionID: "sid-1",
		Scopes: []string{"health_card_share"}, RedirectURI: "org.example.card://oauth/callback",
		GrantExpiresAt: 2000000000,
	}
}

func TestConsentStore_TicketRoundTrip(t *testing.T) {
	rs := newFakeRuntimeStore()
	store := NewConsentStore(rs)
	ctx := context.Background()

	value, err := store.IssueTicket(ctx, testTicket(), 300)
	require.NoError(t, err)
	raw, err := base64.RawURLEncoding.DecodeString(value)
	require.NoError(t, err)
	assert.Len(t, raw, 32, "256-bit ticket")

	// Only the hash of the ticket is a key, in the Native SSO namespace, with the given lifetime.
	require.Len(t, rs.data, 1)
	for k, v := range rs.data {
		assert.True(t, strings.HasPrefix(k, string(providers.NamespaceNativeSSOSession)+"|consent-ticket:"))
		assert.NotContains(t, k, value)
		assert.NotContains(t, string(v), value)
		assert.Equal(t, int64(300), rs.ttls[k])
	}

	peeked, err := store.PeekTicket(ctx, value)
	require.NoError(t, err)
	assert.Equal(t, testTicket(), *peeked)
	peekedAgain, err := store.PeekTicket(ctx, value)
	require.NoError(t, err)
	assert.NotNil(t, peekedAgain, "peeking does not use the ticket up")

	taken, err := store.TakeTicket(ctx, value)
	require.NoError(t, err)
	assert.Equal(t, testTicket(), *taken)
	again, err := store.TakeTicket(ctx, value)
	require.NoError(t, err)
	assert.Nil(t, again, "a ticket can be answered once")
}

func TestConsentStore_TwoTicketsDiffer(t *testing.T) {
	store := NewConsentStore(newFakeRuntimeStore())
	a, err := store.IssueTicket(context.Background(), testTicket(), 60)
	require.NoError(t, err)
	b, err := store.IssueTicket(context.Background(), testTicket(), 60)
	require.NoError(t, err)
	assert.NotEqual(t, a, b)
}

func TestConsentStore_MalformedTicketValues(t *testing.T) {
	store := NewConsentStore(newFakeRuntimeStore())
	for _, value := range []string{"", "short", strings.Repeat("A", 44), "!!!" + strings.Repeat("A", 40)} {
		_, err := store.PeekTicket(context.Background(), value)
		assert.ErrorIs(t, err, ErrInvalidConsentTicket, value)
		_, err = store.TakeTicket(context.Background(), value)
		assert.ErrorIs(t, err, ErrInvalidConsentTicket, value)
	}
}

func TestConsentStore_UnknownTicket(t *testing.T) {
	store := NewConsentStore(newFakeRuntimeStore())
	value := base64.RawURLEncoding.EncodeToString(make([]byte, 32))
	ticket, err := store.PeekTicket(context.Background(), value)
	require.NoError(t, err)
	assert.Nil(t, ticket)
}

func TestConsentStore_TicketErrors(t *testing.T) {
	ctx := context.Background()
	rs := newFakeRuntimeStore()
	store := NewConsentStore(rs)

	_, err := store.IssueTicket(ctx, testTicket(), 0)
	require.Error(t, err)

	rs.putErr = errFakeStore
	_, err = store.IssueTicket(ctx, testTicket(), 60)
	require.ErrorIs(t, err, errFakeStore)
	rs.putErr = nil

	value, err := store.IssueTicket(ctx, testTicket(), 60)
	require.NoError(t, err)
	rs.getErr = errFakeStore
	_, err = store.PeekTicket(ctx, value)
	require.ErrorIs(t, err, errFakeStore)
	rs.takeErr = errFakeStore
	_, err = store.TakeTicket(ctx, value)
	require.ErrorIs(t, err, errFakeStore)

	// A corrupted record is an error, never an empty ticket.
	rs.getErr, rs.takeErr = nil, nil
	for k := range rs.data {
		rs.data[k] = []byte("not json")
	}
	_, err = store.PeekTicket(ctx, value)
	require.Error(t, err)
}

func TestConsentStore_GrantsMergeAndAreScoped(t *testing.T) {
	ctx := context.Background()
	rs := newFakeRuntimeStore()
	store := NewConsentStore(rs)

	none, err := store.GrantedScopes(ctx, "sid-1", "health-card", "psut-1")
	require.NoError(t, err)
	assert.Nil(t, none)

	require.NoError(t, store.RecordGrant(ctx, "sid-1", "health-card", "psut-1", []string{"a"}, 100))
	require.NoError(t, store.RecordGrant(ctx, "sid-1", "health-card", "psut-1", []string{"b", "a"}, 200))

	got, err := store.GrantedScopes(ctx, "sid-1", "health-card", "psut-1")
	require.NoError(t, err)
	assert.Equal(t, []string{"a", "b"}, got)

	// Another session, another app or another user sees nothing.
	for _, args := range [][3]string{{"sid-2", "health-card", "psut-1"}, {"sid-1", "other", "psut-1"},
		{"sid-1", "health-card", "psut-2"}, {"", "health-card", "psut-1"}} {
		got, err := store.GrantedScopes(ctx, args[0], args[1], args[2])
		require.NoError(t, err)
		assert.Nil(t, got, args)
	}
	for k, ttl := range rs.ttls {
		if strings.Contains(k, "consent-grant:") {
			assert.Equal(t, int64(200), ttl, "the last approval sets the lifetime")
		}
	}

	// A record of another user is replaced, not merged.
	require.NoError(t, store.RecordGrant(ctx, "sid-1", "health-card", "psut-2", []string{"c"}, 100))
	got, err = store.GrantedScopes(ctx, "sid-1", "health-card", "psut-2")
	require.NoError(t, err)
	assert.Equal(t, []string{"c"}, got)
}

func TestConsentStore_GrantErrors(t *testing.T) {
	ctx := context.Background()
	rs := newFakeRuntimeStore()
	store := NewConsentStore(rs)

	require.Error(t, store.RecordGrant(ctx, "", "c", "u", []string{"a"}, 10))
	require.Error(t, store.RecordGrant(ctx, "s", "", "u", []string{"a"}, 10))
	require.Error(t, store.RecordGrant(ctx, "s", "c", "", []string{"a"}, 10))
	require.Error(t, store.RecordGrant(ctx, "s", "c", "u", []string{"a"}, 0))

	rs.getErr = errFakeStore
	require.ErrorIs(t, store.RecordGrant(ctx, "s", "c", "u", []string{"a"}, 10), errFakeStore)
	_, err := store.GrantedScopes(ctx, "s", "c", "u")
	require.ErrorIs(t, err, errFakeStore)
	rs.getErr = nil

	rs.putErr = errFakeStore
	require.ErrorIs(t, store.RecordGrant(ctx, "s", "c", "u", []string{"a"}, 10), errFakeStore)
	rs.putErr = nil

	require.NoError(t, store.RecordGrant(ctx, "s", "c", "u", []string{"a"}, 10))
	for k := range rs.data {
		rs.data[k] = []byte("{")
	}
	_, err = store.GrantedScopes(ctx, "s", "c", "u")
	require.Error(t, err)
}

func TestGrantKeyDoesNotCollide(t *testing.T) {
	assert.NotEqual(t, grantKey("ab", "c"), grantKey("a", "bc"))
}

func TestConsentOnlyEligible(t *testing.T) {
	app := &providers.OAuthClient{
		Scopes:      []string{"openid", "profile", "email", "phone", "health_card_share", constants.ScopeDeviceSSO},
		ScopeClaims: map[string][]string{"email": {"email"}, "phone": {"phone_number"}},
	}
	released := []string{"email", "name"}

	assert.True(t, ConsentOnlyEligible([]string{"health_card_share"}, app, released), "no claims")
	assert.True(t, ConsentOnlyEligible([]string{"email"}, app, released), "claims already released")
	assert.False(t, ConsentOnlyEligible([]string{"phone"}, app, released), "new user data")
	assert.False(t, ConsentOnlyEligible([]string{"health_card_share", "phone"}, app, released))
	assert.False(t, ConsentOnlyEligible([]string{"not_allowed"}, app, released))
	assert.False(t, ConsentOnlyEligible([]string{"openid"}, app, released))
	assert.False(t, ConsentOnlyEligible([]string{constants.ScopeDeviceSSO}, app, released))
	assert.False(t, ConsentOnlyEligible(nil, app, released))
	assert.False(t, ConsentOnlyEligible([]string{"health_card_share"}, nil, released))
}

func TestScopeClaimNames(t *testing.T) {
	app := &providers.OAuthClient{ScopeClaims: map[string][]string{"email": {"email"}}}
	assert.Equal(t, []string{"email"}, ScopeClaimNames("email", app))
	assert.Nil(t, ScopeClaimNames("health_card_share", app))
	assert.NotEmpty(t, ScopeClaimNames("phone", nil), "standard scope without a client mapping")
}

func TestSessionRef(t *testing.T) {
	assert.Equal(t, "", SessionRef(""))
	ref := SessionRef("sid-1")
	assert.Len(t, ref, 16)
	assert.NotContains(t, ref, "sid-1")
	assert.Equal(t, ref, SessionRef("sid-1"))
}

func TestAuditConsentNilTicket(t *testing.T) {
	assert.NotPanics(t, func() { AuditConsent(context.Background(), AuditConsentGranted, nil) })
	assert.NotPanics(t, func() {
		ticket := testTicket()
		AuditConsent(context.Background(), AuditConsentGranted, &ticket)
	})
}
