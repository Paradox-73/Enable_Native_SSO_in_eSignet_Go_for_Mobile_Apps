// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package nativesso

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSessionRef(t *testing.T) {
	assert.Equal(t, "", SessionRef(""))
	ref := SessionRef("sid-1")
	assert.Len(t, ref, 16)
	assert.NotContains(t, ref, "sid-1")
	assert.Equal(t, ref, SessionRef("sid-1"))
	assert.NotEqual(t, ref, SessionRef("sid-2"))
	// First 8 bytes of SHA-256("session-1") in hex.
	assert.Equal(t, "84097828fc31a8c8", SessionRef("session-1"))
}

func TestAuditConsentNilTicket(t *testing.T) {
	assert.NotPanics(t, func() { AuditConsent(context.Background(), AuditConsentGranted, nil) })
	assert.NotPanics(t, func() {
		ticket := testTicket()
		AuditConsent(context.Background(), AuditConsentGranted, &ticket)
	})
}
