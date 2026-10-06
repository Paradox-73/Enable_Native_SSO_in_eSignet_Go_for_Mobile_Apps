// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package nativesso

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/thunder-id/thunderid/pkg/thunderidengine/providers"
)

// consentTicketBytes is the number of random bytes in a consent ticket: 256 bits, as for a pushed
// authorization request_uri, so a ticket cannot be guessed.
const consentTicketBytes = 32

// Key prefixes inside providers.NamespaceNativeSSOSession. Session records of the
// DeviceSecretProvider are keyed by the bare sid (base64url, no colon), so these keys never collide
// with them, and no new runtime-store namespace (and Postgres partition) is needed.
const (
	consentTicketKeyPrefix = "consent-ticket:"
	consentGrantKeyPrefix  = "consent-grant:"
)

// ErrInvalidConsentTicket is returned for a consent ticket that is empty or malformed.
var ErrInvalidConsentTicket = errors.New("invalid consent ticket")

// ConsentTicket is what a consent_uri stands for: the second app of a Native SSO login asked for
// scopes beyond the first app's grant, and the user may approve them without logging in again. It is
// created only after every binding check of the token exchange passed (device secret, id_token,
// ds_hash, sid, same vendor, DPoP key of the device secret).
type ConsentTicket struct {
	// ClientID is the second app, which asked for the extra scopes.
	ClientID string `json:"client_id"`
	// FirstClientID is the app the device secret was issued to.
	FirstClientID string `json:"first_client_id"`
	// Subject is the user of the login.
	Subject string `json:"sub"`
	// SessionID is the sid of the login; the approval is kept for this login session only.
	SessionID string `json:"sid"`
	// Scopes are the extra scopes the user is asked to approve, in the order the app asked for them.
	Scopes []string `json:"scopes"`
	// RedirectURI is where the page sends the browser back to the app; empty when the app has no
	// single registered redirect URI, and the page then asks the user to go back to the app.
	RedirectURI string `json:"redirect_uri,omitempty"`
	// GrantExpiresAt is when an approval stops counting: the expiry of the device secret, in seconds
	// since the Unix epoch.
	GrantExpiresAt int64 `json:"grant_exp"`
}

// consentGrant is the stored approval of extra scopes for one second app in one login session.
type consentGrant struct {
	Subject string   `json:"sub"`
	Scopes  []string `json:"scopes"`
}

// ConsentStoreInterface keeps consent tickets and the approvals given with them.
type ConsentStoreInterface interface {
	// IssueTicket stores ticket for ttlSeconds and returns the value to put in the consent_uri.
	IssueTicket(ctx context.Context, ticket ConsentTicket, ttlSeconds int64) (string, error)
	// PeekTicket returns the ticket for value without using it up; nil when unknown or expired.
	PeekTicket(ctx context.Context, value string) (*ConsentTicket, error)
	// TakeTicket returns the ticket for value and deletes it, so it can be answered once; nil when
	// unknown, expired or already answered.
	TakeTicket(ctx context.Context, value string) (*ConsentTicket, error)
	// GrantedScopes returns the extra scopes the user approved for clientID in the login session
	// sessionID, for subject; nil when none.
	GrantedScopes(ctx context.Context, sessionID, clientID, subject string) ([]string, error)
	// RecordGrant adds scopes to the approval of clientID in the login session sessionID. The
	// approval lives ttlSeconds.
	RecordGrant(ctx context.Context, sessionID, clientID, subject string, scopes []string, ttlSeconds int64) error
}

// consentStore is the runtime-store implementation of ConsentStoreInterface.
type consentStore struct {
	store providers.RuntimeStoreProvider
}

// NewConsentStore returns a ConsentStoreInterface backed by the engine's runtime store, so that
// tickets and approvals work across replicas exactly like the login-session records.
func NewConsentStore(store providers.RuntimeStoreProvider) ConsentStoreInterface {
	return &consentStore{store: store}
}

// IssueTicket implements ConsentStoreInterface. Only the SHA-256 hash of the ticket is used as the
// key, so a copy of the runtime store cannot be turned into usable consent links.
func (s *consentStore) IssueTicket(ctx context.Context, ticket ConsentTicket, ttlSeconds int64) (string, error) {
	if ttlSeconds <= 0 {
		return "", fmt.Errorf("consent ticket lifetime must be positive, got %d", ttlSeconds)
	}
	raw := make([]byte, consentTicketBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("failed to generate consent ticket: %w", err)
	}
	value := base64.RawURLEncoding.EncodeToString(raw)
	data, err := json.Marshal(ticket)
	if err != nil {
		return "", fmt.Errorf("failed to marshal consent ticket: %w", err)
	}
	if err := s.store.Put(ctx, providers.NamespaceNativeSSOSession, ticketKey(value), data, ttlSeconds); err != nil {
		return "", fmt.Errorf("failed to store consent ticket: %w", err)
	}
	return value, nil
}

// PeekTicket implements ConsentStoreInterface.
func (s *consentStore) PeekTicket(ctx context.Context, value string) (*ConsentTicket, error) {
	if !validTicketValue(value) {
		return nil, ErrInvalidConsentTicket
	}
	data, err := s.store.Get(ctx, providers.NamespaceNativeSSOSession, ticketKey(value))
	if err != nil {
		return nil, fmt.Errorf("failed to read consent ticket: %w", err)
	}
	return decodeTicket(data)
}

// TakeTicket implements ConsentStoreInterface.
func (s *consentStore) TakeTicket(ctx context.Context, value string) (*ConsentTicket, error) {
	if !validTicketValue(value) {
		return nil, ErrInvalidConsentTicket
	}
	data, err := s.store.Take(ctx, providers.NamespaceNativeSSOSession, ticketKey(value))
	if err != nil {
		return nil, fmt.Errorf("failed to take consent ticket: %w", err)
	}
	return decodeTicket(data)
}

// GrantedScopes implements ConsentStoreInterface. An approval recorded for another user is ignored:
// a session id belongs to one user, so this only guards against a corrupted record.
func (s *consentStore) GrantedScopes(ctx context.Context, sessionID, clientID, subject string) ([]string, error) {
	grant, err := s.readGrant(ctx, sessionID, clientID)
	if err != nil || grant == nil || grant.Subject != subject {
		return nil, err
	}
	return grant.Scopes, nil
}

// RecordGrant implements ConsentStoreInterface.
func (s *consentStore) RecordGrant(ctx context.Context, sessionID, clientID, subject string, scopes []string,
	ttlSeconds int64) error {
	if sessionID == "" || clientID == "" || subject == "" {
		return errors.New("consent grant needs a session id, a client id and a subject")
	}
	if ttlSeconds <= 0 {
		return fmt.Errorf("consent grant lifetime must be positive, got %d", ttlSeconds)
	}
	grant, err := s.readGrant(ctx, sessionID, clientID)
	if err != nil {
		return err
	}
	merged := []string{}
	if grant != nil && grant.Subject == subject {
		merged = append(merged, grant.Scopes...)
	}
	for _, scope := range scopes {
		if !slices.Contains(merged, scope) {
			merged = append(merged, scope)
		}
	}
	data, err := json.Marshal(consentGrant{Subject: subject, Scopes: merged})
	if err != nil {
		return fmt.Errorf("failed to marshal consent grant: %w", err)
	}
	if err := s.store.Put(ctx, providers.NamespaceNativeSSOSession, grantKey(sessionID, clientID), data,
		ttlSeconds); err != nil {
		return fmt.Errorf("failed to store consent grant: %w", err)
	}
	return nil
}

func (s *consentStore) readGrant(ctx context.Context, sessionID, clientID string) (*consentGrant, error) {
	if sessionID == "" || clientID == "" {
		return nil, nil
	}
	data, err := s.store.Get(ctx, providers.NamespaceNativeSSOSession, grantKey(sessionID, clientID))
	if err != nil {
		return nil, fmt.Errorf("failed to read consent grant: %w", err)
	}
	if data == nil {
		return nil, nil
	}
	var grant consentGrant
	if err := json.Unmarshal(data, &grant); err != nil {
		return nil, fmt.Errorf("failed to unmarshal consent grant: %w", err)
	}
	return &grant, nil
}

func decodeTicket(data []byte) (*ConsentTicket, error) {
	if data == nil {
		return nil, nil
	}
	var ticket ConsentTicket
	if err := json.Unmarshal(data, &ticket); err != nil {
		return nil, fmt.Errorf("failed to unmarshal consent ticket: %w", err)
	}
	return &ticket, nil
}

// validTicketValue accepts exactly the shape IssueTicket produces: 32 bytes in unpadded base64url.
func validTicketValue(value string) bool {
	raw, err := base64.RawURLEncoding.DecodeString(value)
	return err == nil && len(raw) == consentTicketBytes
}

func ticketKey(value string) string {
	sum := sha256.Sum256([]byte(value))
	return consentTicketKeyPrefix + base64.RawURLEncoding.EncodeToString(sum[:])
}

// grantKey hashes the session id and client id with a separator byte neither can contain, so two
// different pairs never share a key.
func grantKey(sessionID, clientID string) string {
	sum := sha256.Sum256([]byte(sessionID + "\x00" + clientID))
	return consentGrantKeyPrefix + base64.RawURLEncoding.EncodeToString(sum[:])
}
