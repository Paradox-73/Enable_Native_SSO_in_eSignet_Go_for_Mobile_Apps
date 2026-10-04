// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package par

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"

	oauth2const "github.com/thunder-id/thunderid/internal/oauth/oauth2/constants"
)

// PARNativeSSOTestSuite covers the device_sso checks of the pushed authorization request
// (OpenID Connect Native SSO for Mobile Apps 1.0, statement task 2). It reuses the fixtures of
// ServiceTestSuite without embedding it, so the upstream cases do not run twice.
type PARNativeSSOTestSuite struct {
	suite.Suite
	base *ServiceTestSuite
}

func TestPARNativeSSOSuite(t *testing.T) {
	suite.Run(t, new(PARNativeSSOTestSuite))
}

func (s *PARNativeSSOTestSuite) SetupTest() {
	s.base = &ServiceTestSuite{}
	s.base.SetT(s.T())
	s.base.SetupTest()
}

func (s *PARNativeSSOTestSuite) TearDownTest() {
	s.base.TearDownTest()
}

func (s *PARNativeSSOTestSuite) push(enabled bool, allowedScopes []string, scope string) (bool, string, string) {
	store := newParStoreInterfaceMock(s.T())
	store.EXPECT().Store(mock.Anything, mock.Anything, mock.Anything).Return("test-uri", nil).Maybe()
	cfg := s.base.testCfg
	cfg.NativeSSOEnabled = enabled
	svc := newPARService(store, s.base.newPermissiveResourceMock(), cfg)
	app := s.base.newTestApp()
	app.Scopes = allowedScopes
	params := s.base.newValidParams()
	params[oauth2const.RequestParamScope] = scope

	resp, errCode, errDesc := svc.HandlePushedAuthorizationRequest(s.base.ctx, params, nil, app, testJKT)
	return resp != nil, errCode, errDesc
}

func (s *PARNativeSSOTestSuite) TestDeviceSSOAllowedIsStoredWithTheOIDCScopes() {
	store := newParStoreInterfaceMock(s.T())
	var stored pushedAuthorizationRequest
	store.EXPECT().Store(mock.Anything, mock.Anything, mock.Anything).
		Run(func(_ context.Context, req pushedAuthorizationRequest, _ int64) { stored = req }).
		Return("test-uri", nil)
	cfg := s.base.testCfg
	cfg.NativeSSOEnabled = true
	svc := newPARService(store, s.base.newPermissiveResourceMock(), cfg)
	app := s.base.newTestApp()
	app.Scopes = []string{"openid", "profile", oauth2const.ScopeDeviceSSO}
	params := s.base.newValidParams()
	params[oauth2const.RequestParamScope] = "openid profile device_sso"

	resp, errCode, _ := svc.HandlePushedAuthorizationRequest(s.base.ctx, params, nil, app, testJKT)

	s.Empty(errCode)
	s.NotNil(resp)
	assert.Equal(s.T(), []string{"openid", "profile", "device_sso"}, stored.OAuthParameters.StandardScopes)
	assert.Empty(s.T(), stored.OAuthParameters.PermissionScopes)
}

func (s *PARNativeSSOTestSuite) TestDeviceSSOClientNotAllowedIsInvalidScope() {
	ok, errCode, errDesc := s.push(true, []string{"openid", "profile"}, "openid device_sso")

	s.False(ok)
	s.Equal(oauth2const.ErrorInvalidScope, errCode)
	s.Contains(errDesc, "not allowed")
}

func (s *PARNativeSSOTestSuite) TestDeviceSSOWithoutProviderIsInvalidScope() {
	ok, errCode, errDesc := s.push(false, []string{"openid", oauth2const.ScopeDeviceSSO}, "openid device_sso")

	s.False(ok)
	s.Equal(oauth2const.ErrorInvalidScope, errCode)
	s.Contains(errDesc, "not supported")
}

func (s *PARNativeSSOTestSuite) TestDeviceSSOWithoutOpenIDIsInvalidScope() {
	ok, errCode, _ := s.push(true, []string{"openid", oauth2const.ScopeDeviceSSO}, "profile device_sso")

	s.False(ok)
	s.Equal(oauth2const.ErrorInvalidScope, errCode)
}

func (s *PARNativeSSOTestSuite) TestNoDeviceSSOIsUnchangedWhenNativeSSOIsOff() {
	ok, errCode, _ := s.push(false, nil, "openid profile")

	s.True(ok)
	s.Empty(errCode)
}
