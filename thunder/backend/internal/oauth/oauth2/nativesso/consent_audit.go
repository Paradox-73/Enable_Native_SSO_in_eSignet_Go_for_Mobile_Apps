// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package nativesso

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"github.com/thunder-id/thunderid/internal/system/log"
)

// Audit events of consent-only re-consent. Each is one log line at INFO level with the component
// name NativeSSOConsent, so the deployment's log pipeline can keep them as audit records.
const (
	// AuditConsentTicketIssued: a token exchange was refused for extra scopes and a consent_uri issued.
	AuditConsentTicketIssued = "native_sso_consent_requested"
	// AuditConsentGranted: the user allowed the extra scopes on the consent page.
	AuditConsentGranted = "native_sso_consent_granted"
	// AuditConsentDenied: the user declined the extra scopes on the consent page.
	AuditConsentDenied = "native_sso_consent_denied"
	// AuditConsentOtherBrowser: the consent page was opened or answered outside the browser of the login
	// (for example by the app that asked), and nothing was shown or recorded.
	AuditConsentOtherBrowser = "native_sso_consent_other_browser"
)

// AuditConsent writes one audit line for ticket. It names the apps, the scopes and a short hash of
// the login session, never the user, the session id itself or the ticket.
func AuditConsent(ctx context.Context, event string, ticket *ConsentTicket) {
	if ticket == nil {
		return
	}
	log.GetLogger().With(log.String(log.LoggerKeyComponentName, "NativeSSOConsent")).Info(ctx,
		"Native SSO consent event",
		log.String("event", event),
		log.String("client_id", ticket.ClientID),
		log.String("first_client_id", ticket.FirstClientID),
		log.String("scopes", strings.Join(ticket.Scopes, " ")),
		log.String("session_ref", SessionRef(ticket.SessionID)))
}

// SessionRef returns the first 16 hex digits of the SHA-256 of a session id: enough to tie the audit
// lines of one login together, not enough to use as the sid claim.
func SessionRef(sessionID string) string {
	if sessionID == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(sessionID))
	return hex.EncodeToString(sum[:8])
}
