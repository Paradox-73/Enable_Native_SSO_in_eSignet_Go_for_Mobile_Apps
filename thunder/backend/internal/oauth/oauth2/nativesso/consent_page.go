// Copyright 2026 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package nativesso

import (
	"context"
	"errors"
	"html/template"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/thunder-id/thunderid/internal/oauth/oauth2/constants"
	"github.com/thunder-id/thunderid/internal/system/log"
	"github.com/thunder-id/thunderid/pkg/thunderidengine/providers"
)

// Form fields and values of the consent page, and the query parameter of the redirect back to the app.
const (
	consentFormTicket     = "ticket"
	consentFormDecision   = "decision"
	consentDecisionAllow  = "allow"
	consentDecisionDeny   = "deny"
	consentResultParam    = "native_sso_consent"
	consentResultGranted  = "granted"
	consentResultDenied   = "denied"
	consentMaxFormBytes   = 4096
	inboundClientNameProp = "name"
)

// schemePattern is RFC 3986 3.1 scheme syntax, used before a redirect scheme goes into the CSP.
var schemePattern = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9+.-]*$`)

// ConsentPageHandler serves the consent-only re-consent page: GET shows what the second app asks for,
// POST records the answer and sends the browser back to the app. The consent ticket in the link is the
// only credential: it was issued to the second app after it proved, in the token exchange, that it
// holds the device secret, the id_token and the first app's DPoP key, it is valid for minutes and it
// can be answered once. The approval it records is kept for that login session only and is used only
// by a later token exchange that passes every binding check again.
type ConsentPageHandler struct {
	store         ConsentStoreInterface
	actorProvider providers.ActorProvider
	baseURL       string
	descriptions  map[string]string
	now           func() time.Time
}

// NewConsentPageHandler returns the handler of constants.OAuth2NativeSSOConsentEndpoint.
// scopeDescriptions maps a scope value to the plain-language text the page shows for it
// (NativeSSOConfig.ConsentScopeDescriptions); a scope without an entry is shown by its raw value.
func NewConsentPageHandler(store ConsentStoreInterface, actorProvider providers.ActorProvider,
	baseURL string, scopeDescriptions map[string]string) *ConsentPageHandler {
	return &ConsentPageHandler{store: store, actorProvider: actorProvider, baseURL: baseURL,
		descriptions: scopeDescriptions, now: time.Now}
}

// RegisterConsentRoutes registers the consent page. CORS is not enabled: only the browser opens it.
func RegisterConsentRoutes(mux *http.ServeMux, handler *ConsentPageHandler) {
	mux.HandleFunc("GET "+constants.OAuth2NativeSSOConsentEndpoint, handler.HandleGet)
	mux.HandleFunc("POST "+constants.OAuth2NativeSSOConsentEndpoint, handler.HandlePost)
}

// consentScopeView is one line of the page.
type consentScopeView struct {
	Scope       string
	Description string
	Claims      string
}

type consentPageView struct {
	AppName      string
	FirstAppName string
	Scopes       []consentScopeView
	Ticket       string
	Action       string
}

type consentMessageView struct {
	Title   string
	Message string
}

// HandleGet shows the consent page for the ticket in the query.
func (h *ConsentPageHandler) HandleGet(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	value := r.URL.Query().Get(consentFormTicket)
	ticket, err := h.store.PeekTicket(ctx, value)
	if err != nil && !errors.Is(err, ErrInvalidConsentTicket) {
		h.logger().Error(ctx, "Failed to read a Native SSO consent ticket", log.Error(err))
		h.writeMessage(w, http.StatusInternalServerError, "", msgServerError)
		return
	}
	if ticket == nil {
		h.writeMessage(w, http.StatusBadRequest, "", msgExpired)
		return
	}

	app := h.client(ctx, ticket.ClientID)
	view := consentPageView{
		AppName:      h.clientName(ctx, app, ticket.ClientID),
		FirstAppName: h.clientName(ctx, h.client(ctx, ticket.FirstClientID), ticket.FirstClientID),
		Ticket:       value,
		Action:       constants.OAuth2NativeSSOConsentEndpoint,
	}
	for _, scope := range ticket.Scopes {
		view.Scopes = append(view.Scopes, consentScopeView{
			Scope:       scope,
			Description: strings.TrimSpace(h.descriptions[scope]),
			Claims:      strings.Join(ScopeClaimNames(scope, app), ", "),
		})
	}
	h.writeHeaders(w, ticket.RedirectURI)
	w.WriteHeader(http.StatusOK)
	if err := consentPageTemplate.Execute(w, view); err != nil {
		h.logger().Error(ctx, "Failed to render the Native SSO consent page", log.Error(err))
	}
}

// HandlePost records the user's answer. The ticket is used up first, so a second submission (a double
// tap, a replayed form) finds nothing and changes nothing.
func (h *ConsentPageHandler) HandlePost(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if !h.sameOrigin(r) {
		h.writeMessage(w, http.StatusForbidden, "", msgForbidden)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, consentMaxFormBytes)
	if err := r.ParseForm(); err != nil {
		h.writeMessage(w, http.StatusBadRequest, "", msgBadRequest)
		return
	}
	decision := r.PostForm.Get(consentFormDecision)
	if decision != consentDecisionAllow && decision != consentDecisionDeny {
		h.writeMessage(w, http.StatusBadRequest, "", msgBadRequest)
		return
	}
	ticket, err := h.store.TakeTicket(ctx, r.PostForm.Get(consentFormTicket))
	if err != nil && !errors.Is(err, ErrInvalidConsentTicket) {
		h.logger().Error(ctx, "Failed to take a Native SSO consent ticket", log.Error(err))
		h.writeMessage(w, http.StatusInternalServerError, "", msgServerError)
		return
	}
	if ticket == nil {
		h.writeMessage(w, http.StatusBadRequest, "", msgExpired)
		return
	}

	result := consentResultDenied
	if decision == consentDecisionAllow {
		ttl := ticket.GrantExpiresAt - h.now().Unix()
		if ttl <= 0 {
			h.writeMessage(w, http.StatusBadRequest, "", msgExpired)
			return
		}
		if err := h.store.RecordGrant(ctx, ticket.SessionID, ticket.ClientID, ticket.Subject, ticket.Scopes,
			ttl); err != nil {
			h.logger().Error(ctx, "Failed to record a Native SSO consent", log.Error(err))
			h.writeMessage(w, http.StatusInternalServerError, "", msgServerError)
			return
		}
		AuditConsent(ctx, AuditConsentGranted, ticket)
		result = consentResultGranted
	} else {
		AuditConsent(ctx, AuditConsentDenied, ticket)
	}

	appName := h.clientName(ctx, h.client(ctx, ticket.ClientID), ticket.ClientID)
	if ticket.RedirectURI == "" {
		message := msgDeniedNoRedirect
		if result == consentResultGranted {
			message = msgGrantedNoRedirect
		}
		h.writeMessage(w, http.StatusOK, ticket.RedirectURI, strings.ReplaceAll(message, "{app}", appName))
		return
	}
	target, err := appendQuery(ticket.RedirectURI, consentResultParam, result)
	if err != nil {
		h.logger().Error(ctx, "Registered redirect URI of the second app is not a URI", log.Error(err))
		h.writeMessage(w, http.StatusInternalServerError, "", msgServerError)
		return
	}
	h.writeHeaders(w, ticket.RedirectURI)
	w.Header().Set("Location", target)
	w.WriteHeader(http.StatusSeeOther)
}

// sameOrigin rejects a form posted from another site. Browsers send Origin on every form POST; a
// request without it (an older client or a test tool) still needs the unguessable ticket.
func (h *ConsentPageHandler) sameOrigin(r *http.Request) bool {
	if site := r.Header.Get("Sec-Fetch-Site"); site == "cross-site" || site == "same-site" {
		return false
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	base, err := url.Parse(h.baseURL)
	if err != nil || base.Scheme == "" || base.Host == "" {
		return false
	}
	return strings.EqualFold(origin, base.Scheme+"://"+base.Host)
}

func (h *ConsentPageHandler) client(ctx context.Context, clientID string) *providers.OAuthClient {
	if h.actorProvider == nil || clientID == "" {
		return nil
	}
	app, svcErr := h.actorProvider.GetOAuthClientByClientID(ctx, clientID)
	if svcErr != nil {
		return nil
	}
	return app
}

// clientName returns the display name of a client (eSignet's client_name), or its client id.
func (h *ConsentPageHandler) clientName(ctx context.Context, app *providers.OAuthClient, clientID string) string {
	if h.actorProvider == nil || app == nil {
		return clientID
	}
	id := app.ID
	if id == "" {
		id = clientID
	}
	inbound, svcErr := h.actorProvider.GetInboundClientByID(ctx, id)
	if svcErr != nil || inbound == nil {
		return clientID
	}
	if name, ok := inbound.Properties[inboundClientNameProp].(string); ok && strings.TrimSpace(name) != "" {
		return name
	}
	return clientID
}

// writeHeaders sets the headers of every answer: never cached or framed, no referrer to other sites (the link holds
// the ticket), and a content security policy that allows no script and lets the form go only to this
// server and, through the redirect that answers it, to the app's registered redirect URI.
func (h *ConsentPageHandler) writeHeaders(w http.ResponseWriter, redirectURI string) {
	formAction := "'self'"
	if src := redirectSource(redirectURI); src != "" {
		formAction += " " + src
	}
	header := w.Header()
	header.Set("Content-Type", "text/html; charset=utf-8")
	header.Set("Cache-Control", "no-store")
	header.Set("Pragma", "no-cache")
	header.Set("X-Frame-Options", "DENY")
	header.Set("X-Content-Type-Options", "nosniff")
	header.Set("Referrer-Policy", "same-origin")
	header.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; "+
		"frame-ancestors 'none'; base-uri 'none'; form-action "+formAction)
}

func (h *ConsentPageHandler) writeMessage(w http.ResponseWriter, status int, redirectURI, message string) {
	h.writeHeaders(w, redirectURI)
	w.WriteHeader(status)
	_ = consentMessageTemplate.Execute(w, consentMessageView{Title: "Permission request", Message: message})
}

func (h *ConsentPageHandler) logger() *log.Logger {
	return log.GetLogger().With(log.String(log.LoggerKeyComponentName, "NativeSSOConsent"))
}

// redirectSource returns the CSP source for a redirect URI: the origin of an http(s) URI, the scheme
// of a private-use URI scheme (RFC 8252 7.1), or "" when it is not a usable URI.
func redirectSource(redirectURI string) string {
	if redirectURI == "" {
		return ""
	}
	u, err := url.Parse(redirectURI)
	if err != nil || !schemePattern.MatchString(u.Scheme) {
		return ""
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme == "http" || scheme == "https" {
		if u.Host == "" || strings.ContainsAny(u.Host, " ;'") {
			return ""
		}
		return scheme + "://" + u.Host
	}
	return scheme + ":"
}

// appendQuery adds one query parameter to a registered redirect URI, keeping its own query.
func appendQuery(rawURI, name, value string) (string, error) {
	u, err := url.Parse(rawURI)
	if err != nil {
		return "", err
	}
	q := u.Query()
	q.Set(name, value)
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// Texts of the message pages.
const (
	msgExpired = "This permission request has expired or was already answered. Go back to the app and " +
		"try again."
	msgServerError       = "Something went wrong. Go back to the app and try again later."
	msgBadRequest        = "This request could not be read. Go back to the app and try again."
	msgForbidden         = "This request did not come from this page. Go back to the app and try again."
	msgGrantedNoRedirect = "Done. {app} now has the permission. Go back to {app}."
	msgDeniedNoRedirect  = "Nothing changed. Go back to {app}."
)

const consentPageStyle = `
body{font-family:system-ui,-apple-system,"Segoe UI",Roboto,sans-serif;margin:0;background:#f6f7f9;color:#1b1f24}
main{max-width:30rem;margin:0 auto;padding:1.5rem 1rem}
h1{font-size:1.4rem;margin:0 0 1rem}
p{line-height:1.5}
ul{background:#fff;border:1px solid #d5d9de;border-radius:.5rem;padding:.75rem 1rem .75rem 2rem}
li{margin:.4rem 0}
.claims{color:#4a535d}
.actions{display:flex;flex-direction:column;gap:.75rem;margin-top:1.5rem}
button{font-size:1.05rem;padding:.85rem;border-radius:.5rem;border:1px solid #0b5cad;min-height:48px}
.allow{background:#0b5cad;color:#fff}
.deny{background:#fff;color:#0b5cad}
.note{font-size:.9rem;color:#4a535d}
`

var consentPageTemplate = template.Must(template.New("consent").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Allow more access?</title><style>` + consentPageStyle + `</style></head>
<body><main>
<h1 id="title">Allow more access?</h1>
<p><b>{{.AppName}}</b> asks for more than you allowed when you signed in to <b>{{.FirstAppName}}</b>.</p>
<p>You stay signed in. No new login and no OTP are needed.</p>
<ul aria-labelledby="title">
{{range .Scopes}}<li>{{if .Description}}<b>{{.Description}}</b> <span class="claims">({{.Scope}})</span>
{{- else if .Claims}}Share: {{.Claims}} <span class="claims">({{.Scope}})</span>
{{- else}}Permission: <b>{{.Scope}}</b>{{end}}</li>
{{end}}</ul>
<form method="post" action="{{.Action}}">
<input type="hidden" name="ticket" value="{{.Ticket}}">
<div class="actions">
<button class="allow" type="submit" name="decision" value="allow" id="consent-allow">Allow</button>
<button class="deny" type="submit" name="decision" value="deny" id="consent-deny">Not now</button>
</div>
</form>
<p class="note">{{.AppName}} opened this page on this phone.
The request expires in a few minutes and can be answered once.</p>
</main></body></html>
`))

var consentMessageTemplate = template.Must(template.New("message").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{.Title}}</title><style>` + consentPageStyle + `</style></head>
<body><main><h1>{{.Title}}</h1><p id="message">{{.Message}}</p></main></body></html>
`))
