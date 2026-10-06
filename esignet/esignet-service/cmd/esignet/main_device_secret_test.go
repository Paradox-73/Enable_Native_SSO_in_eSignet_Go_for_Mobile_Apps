/*
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */

package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/suite"
	"github.com/thunder-id/thunderid/pkg/thunderidengine/providers"

	"github.com/mosip/esignet/internal/config"
	"github.com/mosip/esignet/internal/httpmiddleware"
	"github.com/mosip/esignet/internal/keymanager"
)

type fakeCertificateProvisioner struct {
	err          error
	appID, refID string
}

func (f *fakeCertificateProvisioner) GetCertificate(_ context.Context, appID, refID string) (
	keymanager.KeyPairResponse, error) {
	f.appID, f.refID = appID, refID
	return keymanager.KeyPairResponse{}, f.err
}

type ProvisionDeviceSecretKeyTestSuite struct {
	suite.Suite
}

func TestProvisionDeviceSecretKeyTestSuite(t *testing.T) {
	suite.Run(t, new(ProvisionDeviceSecretKeyTestSuite))
}

func (ts *ProvisionDeviceSecretKeyTestSuite) TestProvisionDeviceSecretKey_ResolvesConfiguredKey() {
	km := &fakeCertificateProvisioner{}
	err := provisionDeviceSecretKey(context.Background(), km, config.NativeSSOConfig{EncryptionKeyRefID: "DEVICE_SECRET"})
	ts.Require().NoError(err)
	ts.Equal(config.OIDCServiceAppID, km.appID)
	ts.Equal("DEVICE_SECRET", km.refID)
}

func (ts *ProvisionDeviceSecretKeyTestSuite) TestProvisionDeviceSecretKey_KeyFailureStopsStartUp() {
	km := &fakeCertificateProvisioner{err: errors.New("no key policy")}
	err := provisionDeviceSecretKey(context.Background(), km, config.NativeSSOConfig{EncryptionKeyRefID: "DEVICE_SECRET"})
	ts.ErrorContains(err, "device secret encryption key DEVICE_SECRET")
	ts.ErrorIs(err, km.err)
}

// fakeUserinfoSigner is never reached in these tests: the filter stops at the unreadable token.
type fakeUserinfoSigner struct{}

func (fakeUserinfoSigner) GetPublicKeys(context.Context, providers.PublicKeyFilter) ([]providers.PublicKeyInfo, error) {
	return nil, errors.New("not used")
}

func (fakeUserinfoSigner) Sign(context.Context, providers.KeyRef, string, []byte) ([]byte, error) {
	return nil, errors.New("not used")
}

type NewHandlerTestSuite struct {
	suite.Suite
}

func TestNewHandlerTestSuite(t *testing.T) {
	suite.Run(t, new(NewHandlerTestSuite))
}

// serveUserinfo sends a userinfo request without an access token through newHandler, in front of
// an engine stub that answers 200 with a signed userinfo token.
func (ts *NewHandlerTestSuite) serveUserinfo(enabled bool) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	mux.HandleFunc(httpmiddleware.UserinfoPath, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/jwt")
		_, _ = w.Write([]byte("header.payload.signature"))
	})
	appCfg := &config.AppConfig{}
	appCfg.NativeSSO.Enabled = enabled
	rr := httptest.NewRecorder()
	newHandler(mux, appCfg, fakeUserinfoSigner{}).ServeHTTP(rr,
		httptest.NewRequest(http.MethodGet, httpmiddleware.UserinfoPath, nil))
	return rr
}

func (ts *NewHandlerTestSuite) TestNativeSSOOff_UserinfoAnswersAsUpstream() {
	rr := ts.serveUserinfo(false)
	ts.Equal(http.StatusOK, rr.Code)
	ts.Equal("header.payload.signature", rr.Body.String())
	ts.NotEmpty(rr.Header().Get(httpmiddleware.CorrelationIDHeader))
}

func (ts *NewHandlerTestSuite) TestNativeSSOOn_UserinfoFilterInstalled() {
	rr := ts.serveUserinfo(true)
	// The filter refuses a 200 answer whose access token it cannot read.
	ts.NotEqual(http.StatusOK, rr.Code)
	ts.NotEqual("header.payload.signature", rr.Body.String())
}
