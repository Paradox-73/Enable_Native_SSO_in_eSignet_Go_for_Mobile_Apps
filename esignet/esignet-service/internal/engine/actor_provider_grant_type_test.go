/*
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */

package engine

import (
	"context"
	"testing"

	"github.com/stretchr/testify/suite"
	"github.com/thunder-id/thunderid/pkg/thunderidengine/providers"

	"github.com/mosip/esignet/internal/config"
)

// ActorProviderGrantTypeTestSuite covers how a client's registered grant types reach the engine.
type ActorProviderGrantTypeTestSuite struct {
	suite.Suite
}

func TestActorProviderGrantTypeTestSuite(t *testing.T) {
	suite.Run(t, new(ActorProviderGrantTypeTestSuite))
}

func (ts *ActorProviderGrantTypeTestSuite) TestGetGrantTypes() {
	cases := []struct {
		name       string
		registered []string
		want       []providers.GrantType
	}{
		{name: "authorization code", registered: []string{"authorization_code"},
			want: []providers.GrantType{providers.GrantTypeAuthorizationCode}},
		{name: "authorization code and token exchange",
			registered: []string{"authorization_code", string(providers.GrantTypeTokenExchange)},
			want:       []providers.GrantType{providers.GrantTypeAuthorizationCode, providers.GrantTypeTokenExchange}},
		{name: "token exchange only", registered: []string{string(providers.GrantTypeTokenExchange)},
			want: []providers.GrantType{providers.GrantTypeTokenExchange}},
		{name: "unknown value dropped", registered: []string{"implicit", "authorization_code"},
			want: []providers.GrantType{providers.GrantTypeAuthorizationCode}},
		{name: "none registered keeps authorization code", registered: nil,
			want: []providers.GrantType{providers.GrantTypeAuthorizationCode}},
		{name: "only unknown values keeps authorization code", registered: []string{"implicit"},
			want: []providers.GrantType{providers.GrantTypeAuthorizationCode}},
	}
	for _, tc := range cases {
		ts.Run(tc.name, func() {
			ts.Equal(tc.want, getGrantTypes(tc.registered))
		})
	}
}

func (ts *ActorProviderGrantTypeTestSuite) TestGetOAuthClientByClientID_MapsRegisteredGrantTypes() {
	row := testClientRow()
	row.GrantTypes = `["authorization_code","urn:ietf:params:oauth:grant-type:token-exchange"]`
	p := NewActorProvider(newActorTestService(row), &config.AppConfig{})

	client, svcErr := p.GetOAuthClientByClientID(context.Background(), "client-001")
	ts.Require().Nil(svcErr)
	ts.Equal([]providers.GrantType{providers.GrantTypeAuthorizationCode, providers.GrantTypeTokenExchange},
		client.GrantTypes)
}
