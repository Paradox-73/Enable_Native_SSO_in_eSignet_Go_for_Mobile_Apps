/*
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */

package main

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/mosip/esignet/internal/config"
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
