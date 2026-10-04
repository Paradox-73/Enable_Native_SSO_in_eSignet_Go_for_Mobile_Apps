// SPDX-License-Identifier: MIT
// Copyright (c) 2026 Team Badmosip

package client

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Registration describes a client created through /client-mgmt/client.
type Registration struct {
	ClientID       string
	RelyingPartyID string
	RedirectURI    string
	Key            *ECKey // the client's private_key_jwt key; its public JWK is registered
	UserClaims     []string
	ACRs           []string
}

// RegisterClient creates a FAPI 2.0 client: private_key_jwt, PAR required, DPoP-bound tokens,
// PKCE required.
func (c *Client) RegisterClient(reg Registration) error {
	pub := reg.Key.PublicJWK()
	pub["kid"] = reg.Key.Kid
	pub["use"] = "sig"
	body := map[string]any{
		"requestTime": time.Now().UTC().Format("2006-01-02T15:04:05.000Z"),
		"request": map[string]any{
			"clientId":          reg.ClientID,
			"clientName":        reg.ClientID,
			"clientNameLangMap": map[string]string{"eng": reg.ClientID},
			"relyingPartyId":    reg.RelyingPartyID,
			"logoUri":           "https://example.com/logo.png",
			"redirectUris":      []string{reg.RedirectURI},
			"userClaims":        reg.UserClaims,
			"authContextRefs":   reg.ACRs,
			"publicKey":         pub,
			"grantTypes":        []string{"authorization_code"},
			"clientAuthMethods": []string{"private_key_jwt"},
			"additionalConfig": map[string]any{
				"require_pushed_authorization_requests": true,
				"dpop_bound_access_tokens":              true,
				"require_pkce":                          true,
			},
		},
	}
	r, err := c.PostJSON(c.Issuer+"/client-mgmt/client", body, nil)
	if err != nil {
		return err
	}
	var out struct {
		Errors []struct {
			ErrorCode    string `json:"errorCode"`
			ErrorMessage string `json:"errorMessage"`
		} `json:"errors"`
		Response *struct {
			ClientID string `json:"clientId"`
			Status   string `json:"status"`
		} `json:"response"`
	}
	if err := r.JSON(&out); err != nil {
		return err
	}
	if len(out.Errors) > 0 || out.Response == nil {
		return fmt.Errorf("client registration: HTTP %d: %s", r.Status, r.Body)
	}
	if out.Response.Status != "ACTIVE" || out.Response.ClientID != reg.ClientID {
		return fmt.Errorf("client registration: unexpected response %s", r.Body)
	}
	return nil
}

// User holds the login inputs for the mock identity system.
type User struct {
	IndividualID string
	OTP          string
	// CaptchaToken is sent with the identifier step. eSignet without a validator URL skips the
	// captcha check but rejects an empty token.
	CaptchaToken string
}

// LoginRequest is one authorization-code login.
type LoginRequest struct {
	Reg     Registration
	Scope   string
	User    User
	DPoPKey *ECKey // key the tokens get bound to (cnf.jkt)
	// ExtraPAR adds parameters to the PAR body (for example claims or acr_values).
	ExtraPAR url.Values
}

// Tokens is the token-endpoint answer plus what the login produced on the way.
type Tokens struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	IDToken      string `json:"id_token"`
	RefreshToken string `json:"refresh_token"`
	Scope        string `json:"scope"`
	// Not from the token response:
	State      string `json:"-"`
	CodeIssuer string `json:"-"` // iss parameter of the authorization response (RFC 9207)
}

// Login runs PAR -> authorize -> /flow/execute steps -> /oauth2/auth/callback -> token.
func (c *Client) Login(req LoginRequest) (*Tokens, error) {
	pkce := NewPKCE()
	state := RandomString(12)
	nonce := RandomString(12)

	// 1. PAR (RFC 9126) with private_key_jwt, PKCE, dpop_jkt and a DPoP proof.
	requestURI, err := c.par(req, pkce, state, nonce)
	if err != nil {
		return nil, err
	}

	// 2. Authorize: eSignet answers with a redirect to the login pages carrying authId and
	//    executionId.
	q := url.Values{"client_id": {req.Reg.ClientID}, "request_uri": {requestURI}}
	r, err := c.Get(c.Disc.AuthorizationEndpoint+"?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	if r.Status < 300 || r.Status > 399 {
		return nil, fmt.Errorf("authorize: HTTP %d, want a redirect: %.300s", r.Status, r.Body)
	}
	loc, err := url.Parse(r.Header.Get("Location"))
	if err != nil {
		return nil, fmt.Errorf("authorize Location: %w", err)
	}
	lq := loc.Query()
	if e := lq.Get("error"); e != "" || lq.Get("errorCode") != "" {
		return nil, fmt.Errorf("authorize redirected with an error: %s", loc)
	}
	authID, execID := lq.Get("authId"), lq.Get("executionId")
	if authID == "" || execID == "" {
		return nil, fmt.Errorf("authorize Location lacks authId or executionId: %s", loc)
	}

	// 3. Login steps.
	assertion, err := c.runFlow(execID, req)
	if err != nil {
		return nil, err
	}

	// 4. Callback: exchanges the flow assertion for the client redirect with the code.
	r, err = c.PostJSON(c.Issuer+"/oauth2/auth/callback", map[string]string{"authId": authID, "assertion": assertion}, nil)
	if err != nil {
		return nil, err
	}
	var cb struct {
		RedirectURI  string `json:"redirect_uri"`
		RedirectURI2 string `json:"redirectUri"`
	}
	if err := r.JSON(&cb); err != nil {
		return nil, err
	}
	if cb.RedirectURI == "" {
		cb.RedirectURI = cb.RedirectURI2
	}
	ru, err := url.Parse(cb.RedirectURI)
	if err != nil || cb.RedirectURI == "" {
		return nil, fmt.Errorf("callback: HTTP %d, no redirect_uri: %.300s", r.Status, r.Body)
	}
	rq := ru.Query()
	if rq.Get("error") != "" {
		return nil, fmt.Errorf("callback redirect has an error: %s", ru)
	}
	if got := ru.Scheme + "://" + ru.Host + ru.Path; got != req.Reg.RedirectURI {
		return nil, fmt.Errorf("callback redirect %q, want %q", got, req.Reg.RedirectURI)
	}
	if rq.Get("state") != state {
		return nil, fmt.Errorf("callback state %q, want %q", rq.Get("state"), state)
	}
	code := rq.Get("code")
	if code == "" {
		return nil, fmt.Errorf("callback redirect has no code: %s", ru)
	}

	// 5. Token with private_key_jwt, the PKCE verifier and a DPoP proof.
	tok, err := c.token(req, code, pkce.Verifier)
	if err != nil {
		return nil, err
	}
	tok.State = state
	tok.CodeIssuer = rq.Get("iss")
	return tok, nil
}

func (c *Client) par(req LoginRequest, pkce PKCE, state, nonce string) (string, error) {
	var lastNonce string
	for attempt := 0; attempt < 2; attempt++ {
		assertion, err := ClientAssertion(req.Reg.Key, req.Reg.ClientID, c.Issuer)
		if err != nil {
			return "", err
		}
		form := url.Values{
			"client_id":             {req.Reg.ClientID},
			"redirect_uri":          {req.Reg.RedirectURI},
			"response_type":         {"code"},
			"scope":                 {req.Scope},
			"state":                 {state},
			"nonce":                 {nonce},
			"code_challenge":        {pkce.Challenge},
			"code_challenge_method": {"S256"},
			"dpop_jkt":              {req.DPoPKey.Thumbprint()},
			"client_assertion_type": {"urn:ietf:params:oauth:client-assertion-type:jwt-bearer"},
			"client_assertion":      {assertion},
		}
		for k, vs := range req.ExtraPAR {
			form[k] = vs
		}
		proof, err := DPoPProof(req.DPoPKey, http.MethodPost, c.Disc.PAREndpoint, "", lastNonce)
		if err != nil {
			return "", err
		}
		r, err := c.PostForm(c.Disc.PAREndpoint, form, http.Header{"DPoP": {proof}})
		if err != nil {
			return "", err
		}
		if n := r.Header.Get("DPoP-Nonce"); n != "" && r.Status == http.StatusBadRequest && lastNonce == "" {
			lastNonce = n
			continue
		}
		var out struct {
			RequestURI string `json:"request_uri"`
			ExpiresIn  int    `json:"expires_in"`
		}
		if err := r.JSON(&out); err != nil {
			return "", err
		}
		if r.Status != http.StatusCreated && r.Status != http.StatusOK || out.RequestURI == "" {
			return "", fmt.Errorf("PAR: HTTP %d: %.400s", r.Status, r.Body)
		}
		return out.RequestURI, nil
	}
	return "", fmt.Errorf("PAR: server kept asking for a new DPoP nonce")
}

func (c *Client) token(req LoginRequest, code, verifier string) (*Tokens, error) {
	var lastNonce string
	for attempt := 0; attempt < 2; attempt++ {
		assertion, err := ClientAssertion(req.Reg.Key, req.Reg.ClientID, c.Issuer)
		if err != nil {
			return nil, err
		}
		form := url.Values{
			"grant_type":            {"authorization_code"},
			"code":                  {code},
			"redirect_uri":          {req.Reg.RedirectURI},
			"client_id":             {req.Reg.ClientID},
			"code_verifier":         {verifier},
			"client_assertion_type": {"urn:ietf:params:oauth:client-assertion-type:jwt-bearer"},
			"client_assertion":      {assertion},
		}
		proof, err := DPoPProof(req.DPoPKey, http.MethodPost, c.Disc.TokenEndpoint, "", lastNonce)
		if err != nil {
			return nil, err
		}
		r, err := c.PostForm(c.Disc.TokenEndpoint, form, http.Header{"DPoP": {proof}})
		if err != nil {
			return nil, err
		}
		if n := r.Header.Get("DPoP-Nonce"); n != "" && r.Status == http.StatusBadRequest && lastNonce == "" {
			lastNonce = n
			continue
		}
		if r.Status != http.StatusOK {
			return nil, fmt.Errorf("token: HTTP %d: %.400s", r.Status, r.Body)
		}
		var tok Tokens
		if err := r.JSON(&tok); err != nil {
			return nil, err
		}
		return &tok, nil
	}
	return nil, fmt.Errorf("token: server kept asking for a new DPoP nonce")
}

// Userinfo calls the userinfo endpoint with a DPoP-bound access token. The body is either JSON
// or a signed JWT (eSignet signs userinfo by default).
func (c *Client) Userinfo(dpopKey *ECKey, accessToken string) (*Response, error) {
	proof, err := DPoPProof(dpopKey, http.MethodGet, c.Disc.UserinfoEndpoint, accessToken, "")
	if err != nil {
		return nil, err
	}
	return c.Get(c.Disc.UserinfoEndpoint, http.Header{
		"Authorization": {"DPoP " + accessToken},
		"DPoP":          {proof},
	})
}

// flowStep is the part of a /flow/execute answer the client reads.
type flowStep struct {
	FlowStatus     string `json:"flowStatus"`
	ChallengeToken string `json:"challengeToken"`
	Assertion      string `json:"assertion"`
	FailureReason  string `json:"failureReason"`
	Data           struct {
		Inputs []struct {
			Identifier string `json:"identifier"`
			Type       string `json:"type"`
			Required   bool   `json:"required"`
		} `json:"inputs"`
		Actions []struct {
			Ref  string `json:"ref"`
			Type string `json:"type"`
		} `json:"actions"`
		AdditionalData map[string]any `json:"additionalData"`
	} `json:"data"`
}

// runFlow drives /flow/execute: start, then answer each prompt (identifier and captcha, OTP,
// consent) until the flow is COMPLETE, and returns the flow assertion.
func (c *Client) runFlow(execID string, req LoginRequest) (string, error) {
	step, raw, err := c.flowExecute(map[string]any{"executionId": execID})
	if err != nil {
		return "", err
	}
	for i := 0; i < 10; i++ {
		switch step.FlowStatus {
		case "COMPLETE":
			if step.Assertion == "" {
				return "", fmt.Errorf("flow COMPLETE without an assertion: %.300s", raw)
			}
			return step.Assertion, nil
		case "INCOMPLETE":
		default:
			return "", fmt.Errorf("flow status %q: %.400s", step.FlowStatus, raw)
		}
		body := map[string]any{"executionId": execID, "challengeToken": step.ChallengeToken}
		if len(step.Data.Actions) > 0 {
			body["action"] = step.Data.Actions[0].Ref
		}
		inputs := map[string]string{}
		for _, in := range step.Data.Inputs {
			switch in.Identifier {
			case "username", "individual_id", "individualId":
				inputs[in.Identifier] = req.User.IndividualID
			case "captcha_token":
				inputs[in.Identifier] = req.User.CaptchaToken
			case "otp":
				inputs[in.Identifier] = req.User.OTP
			case "consent_decisions":
				v, err := consentDecisions(req.Reg, step.Data.AdditionalData)
				if err != nil {
					return "", err
				}
				inputs[in.Identifier] = v
			default:
				if in.Required {
					return "", fmt.Errorf("flow asks for input %q (%s), which this client does not know: %.400s", in.Identifier, in.Type, raw)
				}
			}
		}
		if len(inputs) > 0 {
			body["inputs"] = inputs
		}
		step, raw, err = c.flowExecute(body)
		if err != nil {
			return "", err
		}
	}
	return "", fmt.Errorf("flow did not complete after 10 steps: %.400s", raw)
}

func (c *Client) flowExecute(body map[string]any) (*flowStep, []byte, error) {
	r, err := c.PostJSON(c.Issuer+"/flow/execute", body, nil)
	if err != nil {
		return nil, nil, err
	}
	if r.Status != http.StatusOK {
		return nil, r.Body, fmt.Errorf("/flow/execute: HTTP %d: %.400s", r.Status, r.Body)
	}
	var s flowStep
	if err := r.JSON(&s); err != nil {
		return nil, r.Body, err
	}
	if c.Logf != nil {
		ids := make([]string, 0, len(s.Data.Inputs))
		for _, in := range s.Data.Inputs {
			ids = append(ids, in.Identifier)
		}
		c.logf("  flow %s inputs=[%s]%s", s.FlowStatus, strings.Join(ids, ","), ifStr(s.FailureReason != "", " failure="+s.FailureReason))
	}
	return &s, r.Body, nil
}

func ifStr(ok bool, s string) string {
	if ok {
		return s
	}
	return ""
}

// consentDecisions approves every purpose and element the consent prompt lists. When the
// prompt's shape is not recognised it approves the client's registered claims under the purpose
// name eSignet uses ("attributes:<client_id>"), as the upstream Postman flow does.
func consentDecisions(reg Registration, additional map[string]any) (string, error) {
	type element struct {
		Name     string `json:"name"`
		Approved bool   `json:"approved"`
	}
	type purpose struct {
		PurposeName string    `json:"purposeName"`
		Approved    bool      `json:"approved"`
		Elements    []element `json:"elements"`
	}
	var purposes []purpose
	if prompt, ok := additional["consentPrompt"].(string); ok && prompt != "" {
		var p struct {
			Purposes []struct {
				PurposeName string `json:"purposeName"`
				Elements    []struct {
					Name string `json:"name"`
				} `json:"elements"`
			} `json:"purposes"`
		}
		if err := json.Unmarshal([]byte(prompt), &p); err == nil {
			for _, pp := range p.Purposes {
				out := purpose{PurposeName: pp.PurposeName, Approved: true}
				for _, e := range pp.Elements {
					out.Elements = append(out.Elements, element{Name: e.Name, Approved: true})
				}
				purposes = append(purposes, out)
			}
		}
	}
	if len(purposes) == 0 {
		p := purpose{PurposeName: "attributes:" + reg.ClientID, Approved: true}
		for _, n := range reg.UserClaims {
			p.Elements = append(p.Elements, element{Name: n, Approved: true})
		}
		purposes = []purpose{p}
	}
	b, err := json.Marshal(map[string]any{"approved": true, "purposes": purposes})
	return string(b), err
}
