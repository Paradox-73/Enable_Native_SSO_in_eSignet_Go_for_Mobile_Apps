// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package authz

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	oauth2const "github.com/thunder-id/thunderid/internal/oauth/oauth2/constants"
)

// fakeBrowserBinder records the logins it marked and sets a cookie like nativesso.ConsentBrowserBinding.
type fakeBrowserBinder struct {
	bound []string
	err   error
}

func (f *fakeBrowserBinder) Bind(_ context.Context, w http.ResponseWriter, authID string) error {
	f.bound = append(f.bound, authID)
	if f.err != nil {
		return f.err
	}
	http.SetCookie(w, &http.Cookie{Name: "tid_nsso_test", Value: "v"})
	return nil
}

func (suite *AuthorizeHandlerTestSuite) authorizeWith(deviceSSO bool,
	binder ConsentBrowserBinder) *httptest.ResponseRecorder {
	suite.handler.browserBinder = binder
	result := &AuthorizationInitResult{
		QueryParams: map[string]string{
			oauth2const.AuthID:      testAuthID,
			oauth2const.AppID:       "test-app-id",
			oauth2const.ExecutionID: "test-flow-id",
		},
		DeviceSSO: deviceSSO,
	}
	suite.mockAuthzService.EXPECT().HandleInitialAuthorizationRequest(mock.Anything, mock.Anything).
		Return(result, nil).Once()
	req := httptest.NewRequest("GET", "/oauth2/authorize?client_id=test-client&request_uri=urn:x", nil)
	rr := httptest.NewRecorder()
	suite.handler.HandleAuthorizeGetRequest(rr, req)
	return rr
}

// A device_sso login marks the browser for the Native SSO consent page of that login.
func (suite *AuthorizeHandlerTestSuite) TestHandleAuthorizeGetRequest_DeviceSSOMarksBrowser() {
	binder := &fakeBrowserBinder{}
	rr := suite.authorizeWith(true, binder)

	assert.Equal(suite.T(), http.StatusFound, rr.Code)
	assert.Contains(suite.T(), rr.Header().Get("Location"), "/login")
	assert.Equal(suite.T(), []string{testAuthID}, binder.bound)
	assert.Contains(suite.T(), rr.Header().Get("Set-Cookie"), "tid_nsso_test=v")
}

func (suite *AuthorizeHandlerTestSuite) TestHandleAuthorizeGetRequest_OtherLoginsAreNotMarked() {
	binder := &fakeBrowserBinder{}
	rr := suite.authorizeWith(false, binder)

	assert.Equal(suite.T(), http.StatusFound, rr.Code)
	assert.Empty(suite.T(), binder.bound)
	assert.Empty(suite.T(), rr.Header().Get("Set-Cookie"))
}

// Without the feature there is no binder, and a device_sso login proceeds as before.
func (suite *AuthorizeHandlerTestSuite) TestHandleAuthorizeGetRequest_DeviceSSOWithoutBinder() {
	rr := suite.authorizeWith(true, nil)

	assert.Equal(suite.T(), http.StatusFound, rr.Code)
	assert.Empty(suite.T(), rr.Header().Get("Set-Cookie"))
}

// A failure to mark the browser does not stop the login; only a later consent-only re-consent then
// needs a new login.
func (suite *AuthorizeHandlerTestSuite) TestHandleAuthorizeGetRequest_MarkFailureKeepsLogin() {
	binder := &fakeBrowserBinder{err: errors.New("store down")}
	rr := suite.authorizeWith(true, binder)

	assert.Equal(suite.T(), http.StatusFound, rr.Code)
	assert.Contains(suite.T(), rr.Header().Get("Location"), "/login")
	assert.Equal(suite.T(), []string{testAuthID}, binder.bound)
}
