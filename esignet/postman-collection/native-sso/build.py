# SPDX-License-Identifier: MIT
# Copyright (c) 2026 Team Badmosip
"""Adds (or replaces) the "Native SSO" folder in Go-eSignet.postman_collection.json.

    python esignet/postman-collection/native-sso/build.py

The folder's request scripts are written here, the helper library is lib.js (stored in the
collection variable `ns_lib`). The rest of the collection is left as it is. No key is stored: every
key is made by a pre-request script at run time.
"""
import json
import os

HERE = os.path.dirname(os.path.abspath(__file__))
COLLECTION = os.path.join(HERE, "..", "Go-eSignet.postman_collection.json")

PRE = "const L = new Function('CryptoJS', 'pm', pm.collectionVariables.get('ns_lib'))(CryptoJS, pm);\n"
FORM = "application/x-www-form-urlencoded"
GRANT_EX = "urn:ietf:params:oauth:grant-type:token-exchange"
ASSERT_TYPE = "urn:ietf:params:oauth:client-assertion-type:jwt-bearer"
ID_TOKEN_TYPE = "urn:ietf:params:oauth:token-type:id_token"
DS_TYPE = "urn:openid:params:token-type:device-secret"
ISS = "{{ns_issuer}}"


def ev(listen, js):
    return {"listen": listen, "script": {"type": "text/javascript", "exec": js.strip("\n").split("\n")}}


def url(raw):
    return raw


def item(name, method, raw_url, pre=None, test=None, body=None, headers=None, description=None, follow=True):
    req = {"method": method, "header": headers or [], "url": url(raw_url)}
    if body is not None:
        req["body"] = body
    if description:
        req["description"] = description
    out = {"name": name, "request": req, "response": []}
    events = []
    if pre:
        events.append(ev("prerequest", pre))
    if test:
        events.append(ev("test", test))
    if events:
        out["event"] = events
    if not follow:
        out["protocolProfileBehavior"] = {"followRedirects": False}
    return out


def raw_json(text):
    return {"mode": "raw", "raw": text, "options": {"raw": {"language": "json"}}}


def form(pairs):
    return {"mode": "urlencoded", "urlencoded": [{"key": k, "value": v, "type": "text"} for k, v in pairs]}


JSON_HDR = [{"key": "Content-Type", "value": "application/json"}]

# ----------------------------------------------------------------------------------------------
# Setup
# ----------------------------------------------------------------------------------------------

DISCOVERY_TEST = r"""
pm.test('discovery 200', () => pm.response.to.have.status(200));
const d = pm.response.json();
pm.test('issuer is the configured issuer', () => pm.expect(d.issuer).to.eql(pm.environment.get('ns_issuer')));
pm.test('native_sso_supported is true', () => pm.expect(d.native_sso_supported).to.eql(true));
pm.test('scopes_supported lists device_sso', () => pm.expect(d.scopes_supported).to.include('device_sso'));
pm.test('grant_types_supported lists token-exchange', () =>
  pm.expect(d.grant_types_supported).to.include('urn:ietf:params:oauth:grant-type:token-exchange'));
pm.test('DPoP ES256 is supported', () => pm.expect(d.dpop_signing_alg_values_supported).to.include('ES256'));
pm.test('revocation_endpoint is advertised (device secret revocation)', () =>
  pm.expect(d.revocation_endpoint).to.eql(pm.environment.get('ns_issuer') + '/oauth2/revoke'));
pm.test('client authentication is private_key_jwt only', () =>
  pm.expect(d.token_endpoint_auth_methods_supported).to.eql(['private_key_jwt']));
"""

SETUP_PRE = PRE + r"""
// One key pair per client plus the Hub's DPoP key. All are made here, now, and live only in
// environment variables of this run (see "99 Cleanup").
const sfx = L.uuid().slice(0, 8);
pm.environment.set('ns_rp_id', 'ns-rp-' + sfx);
pm.environment.set('ns_other_rp_id', 'ns-rp-other-' + sfx);
['hub', 'card', 'plain', 'other'].forEach((n) => {
  const k = L.newKey();
  pm.environment.set('ns_' + n + '_key', JSON.stringify(k));
  pm.environment.set('ns_' + n + '_pub', JSON.stringify(Object.assign(L.publicJwk(k), { kid: k.kid, use: 'sig' })));
  pm.environment.set('ns_' + n + '_id', 'ns-' + n + '-' + sfx);
});
pm.environment.set('ns_hub_dpop_key', JSON.stringify(L.newKey()));
pm.environment.set('ns_request_time', new Date().toISOString());
const t = pm.environment.get('client_mgmt_token');
if (t) pm.request.headers.upsert({ key: 'Authorization', value: 'Bearer ' + t });
"""

OTHER_PRE = PRE + r"""
pm.environment.set('ns_request_time', new Date().toISOString());
const t = pm.environment.get('client_mgmt_token');
if (t) pm.request.headers.upsert({ key: 'Authorization', value: 'Bearer ' + t });
"""


def client_body(idvar, name, rp, redirect, pubvar, grants, scopes):
    cfg = {
        "require_pushed_authorization_requests": True,
        "dpop_bound_access_tokens": True,
        "require_pkce": True,
        "allowed_authorization_scopes": scopes,
    }
    return raw_json(
        "{\n"
        '  "requestTime": "{{ns_request_time}}",\n'
        '  "request": {\n'
        '    "clientId": "{{%s}}",\n'
        '    "clientName": "%s",\n'
        '    "clientNameLangMap": { "eng": "%s" },\n'
        '    "relyingPartyId": "{{%s}}",\n'
        '    "logoUri": "https://example.com/logo.png",\n'
        '    "redirectUris": ["%s"],\n'
        '    "userClaims": ["name", "email", "phone_number"],\n'
        '    "authContextRefs": ["mosip:idp:acr:generated-code"],\n'
        '    "publicKey": {{%s}},\n'
        '    "grantTypes": %s,\n'
        '    "clientAuthMethods": ["private_key_jwt"],\n'
        '    "additionalConfig": %s\n'
        "  }\n}" % (idvar, name, name, rp, redirect, pubvar, json.dumps(grants), json.dumps(cfg))
    )


CREATE_TEST = r"""
pm.test('client registration 200', () => pm.response.to.have.status(200));
const j = pm.response.json();
pm.test('no errors', () => pm.expect(j.errors || []).to.have.lengthOf(0));
pm.test('client is ACTIVE', () => pm.expect(j.response.status).to.eql('ACTIVE'));
pm.test('client_id echoed', () => pm.expect(j.response.clientId).to.eql(pm.environment.get('%s')));
"""

HUB_REDIRECT = "org.badmosip.nativesso.hub://oauth/callback"
CARD_REDIRECT = "org.badmosip.nativesso.card://oauth/callback"
EX_GRANTS = ["authorization_code", GRANT_EX]
SSO_SCOPES = ["openid", "profile", "email", "phone", "device_sso"]

setup = [
    item("00 Discovery shows Native SSO", "GET", ISS + "/.well-known/openid-configuration",
         test=DISCOVERY_TEST,
         description="Checks the discovery fields Native SSO adds: native_sso_supported, device_sso in scopes_supported and the token-exchange grant."),
    item("01 Generate keys and register the Hub (first app)", "POST", ISS + "/client-mgmt/client",
         pre=SETUP_PRE, test=CREATE_TEST % "ns_hub_id", headers=JSON_HDR,
         body=client_body("ns_hub_id", "Native SSO Hub", "ns_rp_id", HUB_REDIRECT, "ns_hub_pub", EX_GRANTS, SSO_SCOPES),
         description="Makes four ES256 key pairs and the Hub's DPoP key in the script, then registers the Hub: grantTypes with token-exchange and allowed_authorization_scopes with device_sso."),
    item("02 Register the Health Card (second app, same relying party)", "POST", ISS + "/client-mgmt/client",
         pre=OTHER_PRE, test=CREATE_TEST % "ns_card_id", headers=JSON_HDR,
         body=client_body("ns_card_id", "Native SSO Health Card", "ns_rp_id", CARD_REDIRECT, "ns_card_pub", EX_GRANTS, SSO_SCOPES)),
    item("03 Register a client that does not allow device_sso", "POST", ISS + "/client-mgmt/client",
         pre=OTHER_PRE, test=CREATE_TEST % "ns_plain_id", headers=JSON_HDR,
         body=client_body("ns_plain_id", "Native SSO Plain", "ns_rp_id", "org.badmosip.nativesso.plain://oauth/callback", "ns_plain_pub", ["authorization_code"], ["openid", "profile", "email"])),
    item("04 Register an app of another relying party", "POST", ISS + "/client-mgmt/client",
         pre=OTHER_PRE, test=CREATE_TEST % "ns_other_id", headers=JSON_HDR,
         body=client_body("ns_other_id", "Native SSO Outsider", "ns_other_rp_id", "org.badmosip.nativesso.other://oauth/callback", "ns_other_pub", EX_GRANTS, SSO_SCOPES)),
]

# ----------------------------------------------------------------------------------------------
# Hub sign-in (PAR with device_sso, authorize, login steps, callback, token)
# ----------------------------------------------------------------------------------------------

# Shared by every request that signs a client assertion and a DPoP proof. `who` is the client name
# (hub, card, plain, other); `dpop` the DPoP key variable.
SIGN_HELPERS = r"""
const issuer = pm.environment.get('ns_issuer');
const sign = (who) => L.clientAssertion(pm.environment.get('ns_' + who + '_id'), L.getKey('ns_' + who + '_key'), issuer);
const dpop = (keyVar, htm, path, ath) => {
  const p = L.dpopProof(L.getKey(keyVar), htm, issuer + path, ath);
  pm.request.headers.upsert({ key: 'DPoP', value: p });
};
"""

PAR_PRE = PRE + SIGN_HELPERS + r"""
const who = '%(who)s';
const pk = L.pkce();
pm.environment.set('ns_code_verifier', pk.verifier);
pm.variables.set('ns_code_challenge', pk.challenge);
pm.environment.set('ns_state', L.uuid().slice(0, 12));
pm.environment.set('ns_nonce', L.uuid().slice(0, 12));
pm.variables.set('ns_par_who_id', pm.environment.get('ns_' + who + '_id'));
pm.variables.set('ns_par_redirect', %(redirect)s);
pm.variables.set('ns_par_scope', '%(scope)s');
pm.variables.set('ns_par_assertion', sign(who));
pm.variables.set('ns_par_jkt', L.thumbprint(L.getKey('%(dpopkey)s')));
dpop('%(dpopkey)s', 'POST', '/oauth2/par');
"""

PAR_BODY = [
    ("client_id", "{{ns_par_who_id}}"), ("redirect_uri", "{{ns_par_redirect}}"), ("response_type", "code"),
    ("scope", "{{ns_par_scope}}"), ("state", "{{ns_state}}"), ("nonce", "{{ns_nonce}}"),
    ("code_challenge", "{{ns_code_challenge}}"), ("code_challenge_method", "S256"), ("dpop_jkt", "{{ns_par_jkt}}"),
    ("client_assertion_type", ASSERT_TYPE), ("client_assertion", "{{ns_par_assertion}}"),
]

PAR_OK_TEST = r"""
pm.test('PAR answers 201', () => pm.response.to.have.status(201));
const r = pm.response.json();
pm.test('request_uri present', () => pm.expect(r.request_uri).to.be.a('string').and.not.empty);
pm.test('expires_in is positive', () => pm.expect(r.expires_in).to.be.above(0));
pm.environment.set('ns_request_uri', r.request_uri);
"""

AUTHORIZE_TEST = PRE + r"""
pm.test('authorize redirects to the login page', () => pm.expect(pm.response.code).to.be.oneOf([302, 303, 307]));
const q = L.parseUrl(pm.response.headers.get('Location')).params;
pm.test('redirect carries no error', () => pm.expect(q.error).to.be.undefined);
pm.test('authId and executionId present', () => {
  pm.expect(q.authId).to.be.a('string').and.not.empty;
  pm.expect(q.executionId).to.be.a('string').and.not.empty;
});
pm.environment.set('ns_auth_id', q.authId);
pm.environment.set('ns_execution_id', q.executionId);
"""

# One /flow/execute request per screen. The login helper in e2e/client/fapi.go answers the same
# screens in the same order; here each screen is its own request so a person can see them.
FLOW_STEP_JS = r"""
pm.test('flow step answers 200', () => pm.response.to.have.status(200));
const j = pm.response.json();
if (j.challengeToken) pm.environment.set('ns_challenge_token', j.challengeToken);
const ids = ((j.data && j.data.inputs) || []).map((i) => i.identifier);
const act = j.data && j.data.actions && j.data.actions[0];
pm.environment.set('ns_action_ref', act ? act.ref : '');
console.log('flow ' + j.flowStatus + ' inputs=[' + ids.join(',') + '] action=' + (act ? act.ref : ''));
"""

FLOW_BODY_HEAD = '{\n  "executionId": "{{ns_execution_id}}"'


def flow_body(inputs=None):
    s = FLOW_BODY_HEAD + ',\n  "challengeToken": "{{ns_challenge_token}}",\n  "action": "{{ns_action_ref}}"'
    if inputs:
        s += ',\n  "inputs": ' + inputs
    return raw_json(s + "\n}")


flow_start = item(
    "Hub 04 Login screen: start the flow", "POST", ISS + "/flow/execute",
    headers=JSON_HDR, body=raw_json(FLOW_BODY_HEAD + "\n}"),
    test=FLOW_STEP_JS + r"""
pm.test('flow is INCOMPLETE and asks for the identifier', () => {
  pm.expect(j.flowStatus).to.eql('INCOMPLETE');
  pm.expect(ids).to.include.members(['username']);
});
""",
    description="The browser takes executionId from the authorize redirect and opens the login page. The headless login helper in e2e/client/fapi.go (runFlow) never opens it and posts the same JSON to /flow/execute; so does this request.")

flow_uin = item(
    "Hub 05 Login screen: enter the UIN", "POST", ISS + "/flow/execute", headers=JSON_HDR,
    body=flow_body('{\n    "username": "{{ns_individual_id}}",\n    "captcha_token": "{{ns_captcha_token}}"\n  }'),
    test=FLOW_STEP_JS + r"""
pm.test('flow is INCOMPLETE and asks for the OTP', () => {
  pm.expect(j.flowStatus).to.eql('INCOMPLETE');
  pm.expect(ids).to.include('otp');
});
""")

flow_otp = item(
    "Hub 06 Login screen: enter the OTP", "POST", ISS + "/flow/execute", headers=JSON_HDR,
    body=flow_body('{\n    "otp": "{{ns_otp}}"\n  }'),
    test=FLOW_STEP_JS + r"""
pm.test('flow is INCOMPLETE and asks for consent', () => {
  pm.expect(j.flowStatus).to.eql('INCOMPLETE');
  pm.expect(ids).to.include('consent_decisions');
});
// The consent prompt lists the claim names the user is asked to release.
const prompt = j.data && j.data.additionalData && j.data.additionalData.consentPrompt;
pm.environment.set('ns_consent_prompt', prompt || '');
""")

flow_consent = item(
    "Hub 07 Consent screen: allow", "POST", ISS + "/flow/execute", headers=JSON_HDR,
    pre=r"""
// Approves every purpose and element the consent screen listed; falls back to the registered claims.
let purposes;
try {
  const p = JSON.parse(pm.environment.get('ns_consent_prompt'));
  purposes = p.purposes.map((x) => ({ purposeName: x.purposeName, approved: true, elements: (x.elements || []).map((e) => ({ name: e.name, approved: true })) }));
} catch (e) {
  purposes = [{ purposeName: 'attributes:' + pm.environment.get('ns_hub_id'), approved: true,
    elements: ['name', 'email', 'phone_number'].map((n) => ({ name: n, approved: true })) }];
}
pm.variables.set('ns_consent_decisions', JSON.stringify(JSON.stringify({ approved: true, purposes: purposes })).slice(1, -1));
""",
    body=flow_body('{\n    "consent_decisions": "{{ns_consent_decisions}}"\n  }'),
    test=r"""
pm.test('consent answers 200', () => pm.response.to.have.status(200));
const j = pm.response.json();
pm.test('flow is COMPLETE with an assertion', () => {
  pm.expect(j.flowStatus).to.eql('COMPLETE');
  pm.expect(j.assertion).to.be.a('string').and.not.empty;
});
pm.environment.set('ns_assertion', j.assertion);
""")

callback = item(
    "Hub 08 Callback: flow assertion to authorization code", "POST", ISS + "/oauth2/auth/callback", headers=JSON_HDR,
    body=raw_json('{\n  "authId": "{{ns_auth_id}}",\n  "assertion": "{{ns_assertion}}"\n}'),
    test=PRE + r"""
pm.test('callback answers 200', () => pm.response.to.have.status(200));
const j = pm.response.json();
const ru = L.parseUrl(j.redirect_uri || j.redirectUri);
pm.test('redirect goes to the Hub app scheme', () => pm.expect(ru.base).to.eql('org.badmosip.nativesso.hub://oauth/callback'));
pm.test('state is echoed', () => pm.expect(ru.params.state).to.eql(pm.environment.get('ns_state')));
pm.test('iss is the issuer (RFC 9207)', () => pm.expect(ru.params.iss).to.eql(pm.environment.get('ns_issuer')));
pm.test('authorization code present', () => pm.expect(ru.params.code).to.be.a('string').and.not.empty);
pm.environment.set('ns_code', ru.params.code);
""",
    description="In the app the browser redirects to the app's custom scheme and the app reads code and state. Here the server's answer to the callback call carries the same URL.")

TOKEN_PRE = PRE + SIGN_HELPERS + r"""
pm.variables.set('ns_tok_assertion', sign('hub'));
dpop('ns_hub_dpop_key', 'POST', '/oauth2/token');
"""

token_hub = item(
    "Hub 09 Token: code for tokens and device_secret", "POST", ISS + "/oauth2/token", pre=TOKEN_PRE,
    body=form([
        ("grant_type", "authorization_code"), ("code", "{{ns_code}}"), ("redirect_uri", HUB_REDIRECT),
        ("client_id", "{{ns_hub_id}}"), ("code_verifier", "{{ns_code_verifier}}"),
        ("client_assertion_type", ASSERT_TYPE), ("client_assertion", "{{ns_tok_assertion}}")]),
    test=PRE + r"""
pm.test('token answers 200', () => pm.response.to.have.status(200));
const t = pm.response.json();
pm.test('token_type is DPoP', () => pm.expect(t.token_type.toLowerCase()).to.eql('dpop'));
pm.test('device_secret is returned', () => pm.expect(t.device_secret).to.be.a('string').and.not.empty);
pm.test('scope includes device_sso', () => pm.expect(t.scope.split(' ')).to.include('device_sso'));
const idt = L.decodeJwt(t.id_token).payload;
pm.test('id_token aud is the Hub', () => pm.expect([].concat(idt.aud)).to.include(pm.environment.get('ns_hub_id')));
pm.test('id_token carries sid', () => pm.expect(idt.sid).to.be.a('string').and.not.empty);
pm.test('id_token ds_hash matches the device secret (left 128 bits of SHA-256, base64url)', () =>
  pm.expect(idt.ds_hash).to.eql(L.b64u(L.sha256(L.utf8(t.device_secret)).slice(0, 16))));
pm.test('id_token nonce matches', () => pm.expect(idt.nonce).to.eql(pm.environment.get('ns_nonce')));
let at = null;
try { at = L.decodeJwt(t.access_token).payload; } catch (e) { /* opaque token */ }
if (at) pm.test('access token is bound to the Hub DPoP key (cnf.jkt)', () =>
  pm.expect(at.cnf.jkt).to.eql(L.thumbprint(L.getKey('ns_hub_dpop_key'))));
pm.environment.set('ns_hub_access_token', t.access_token);
pm.environment.set('ns_hub_id_token', t.id_token);
pm.environment.set('ns_device_secret', t.device_secret);
pm.environment.set('ns_hub_sub', idt.sub);
pm.environment.set('ns_hub_sid', idt.sid);
""")

USERINFO_PRE = PRE + SIGN_HELPERS + r"""
const at = pm.environment.get('%(tokenvar)s');
pm.request.headers.upsert({ key: 'Authorization', value: 'DPoP ' + at });
dpop('ns_hub_dpop_key', 'GET', '/oauth2/userinfo', at);
"""

USERINFO_TEST = PRE + r"""
pm.test('userinfo answers 200', () => pm.response.to.have.status(200));
// eSignet signs the userinfo response (a JWT); a JSON body is accepted too.
const body = pm.response.text().trim();
const claims = body.split('.').length === 3 ? L.decodeJwt(body).payload : JSON.parse(body);
pm.test('userinfo sub equals the sub of the id_token', () => pm.expect(claims.sub).to.eql(pm.environment.get('%(subvar)s')));
%(extra)s
"""

userinfo_hub = item(
    "Hub 10 Userinfo with the Hub token", "GET", ISS + "/oauth2/userinfo",
    pre=USERINFO_PRE % {"tokenvar": "ns_hub_access_token"},
    test=USERINFO_TEST % {"subvar": "ns_hub_sub", "extra": "pm.test('userinfo has the email claim', () => pm.expect(claims.email).to.be.a('string').and.not.empty);"})

hub = [
    item("Hub 01 PAR with device_sso", "POST", ISS + "/oauth2/par",
         pre=PAR_PRE % {"who": "hub", "redirect": "'" + HUB_REDIRECT + "'", "scope": "openid profile email device_sso", "dpopkey": "ns_hub_dpop_key"},
         test=PAR_OK_TEST, body=form(PAR_BODY),
         description="Pushed authorization request (RFC 9126) with scope `openid profile email device_sso`, PKCE, `dpop_jkt` (thumbprint of the Hub's DPoP key), a DPoP proof and a private_key_jwt whose aud is the issuer."),
    item("Hub 02 Authorize", "GET", ISS + "/oauth2/authorize?client_id={{ns_hub_id}}&request_uri={{ns_request_uri}}",
         test=AUTHORIZE_TEST, follow=False,
         description="Redirects to the login page with authId and executionId. The request does not follow the redirect."),
    item("Hub 03 Login pages metadata", "GET", ISS + "/flow/meta?type=APP&id={{ns_hub_id}}&language=en&namespace=auth",
         test="pm.test('flow meta answers 200', () => pm.response.to.have.status(200));"),
    flow_start, flow_uin, flow_otp, flow_consent, callback, token_hub, userinfo_hub,
]

# ----------------------------------------------------------------------------------------------
# Health Card: token exchange with the device secret, and its negative cases
# ----------------------------------------------------------------------------------------------

# Sets the form fields of an exchange as local variables, then lets `overrides` change them. A
# value of null removes the field. DPoP is signed by `dpopkey` (the Hub's key unless overridden).
EXCHANGE_PRE = PRE + SIGN_HELPERS + r"""
const f = {
  grant_type: '%(grant)s',
  subject_token: pm.environment.get('ns_hub_id_token'),
  subject_token_type: '%(idtype)s',
  actor_token: pm.environment.get('ns_device_secret'),
  actor_token_type: '%(dstype)s',
  audience: issuer,
  scope: 'openid email',
  client_id: pm.environment.get('ns_card_id'),
  client_assertion_type: '%(atype)s',
  client_assertion: sign('card'),
};
let dpopKey = 'ns_hub_dpop_key';
let sendDpop = true;
%(overrides)s
Object.keys(f).forEach((k) => pm.variables.set('ns_ex_' + k, f[k] === null ? '' : f[k]));
Object.keys(f).filter((k) => f[k] === null).forEach((k) => pm.request.body.urlencoded.remove(k));
if (sendDpop) dpop(dpopKey, 'POST', '/oauth2/token');
"""

EX_FIELDS = ["grant_type", "subject_token", "subject_token_type", "actor_token", "actor_token_type", "audience",
             "scope", "client_id", "client_assertion_type", "client_assertion"]
EX_BODY = form([(k, "{{ns_ex_%s}}" % k) for k in EX_FIELDS])


def exchange(name, overrides, test, description=None):
    pre = EXCHANGE_PRE % {"grant": GRANT_EX, "idtype": ID_TOKEN_TYPE, "dstype": DS_TYPE, "atype": ASSERT_TYPE,
                          "overrides": overrides}
    return item(name, "POST", ISS + "/oauth2/token", pre=pre, test=test, body=EX_BODY, description=description)


def expect_error(status, *errors):
    return r"""
pm.test('answers %(status)d', () => pm.response.to.have.status(%(status)d));
const e = pm.response.json();
pm.test('error is %(names)s', () => pm.expect(e.error).to.be.oneOf(%(arr)s));
pm.test('no token is issued', () => pm.expect(e.access_token).to.be.undefined);
pm.test('error_description is present', () => pm.expect(e.error_description).to.be.a('string').and.not.empty);
""" % {"status": status, "names": ", ".join(errors), "arr": json.dumps(list(errors))}


EXCHANGE_OK_TEST = PRE + r"""
pm.test('exchange answers 200', () => pm.response.to.have.status(200));
const t = pm.response.json();
pm.test('token_type is DPoP', () => pm.expect(t.token_type.toLowerCase()).to.eql('dpop'));
pm.test('issued_token_type is access_token', () => pm.expect(t.issued_token_type).to.eql('urn:ietf:params:oauth:token-type:access_token'));
pm.test('scope has no device_sso', () => pm.expect(t.scope.split(' ')).to.not.include('device_sso'));
pm.test('scope is the requested subset', () => pm.expect(t.scope.split(' ').sort()).to.eql(['email', 'openid']));
const idt = L.decodeJwt(t.id_token).payload;
pm.test('id_token aud is the Health Card, not the Hub', () => {
  pm.expect([].concat(idt.aud)).to.include(pm.environment.get('ns_card_id'));
  pm.expect([].concat(idt.aud)).to.not.include(pm.environment.get('ns_hub_id'));
});
pm.test('id_token has the same sub as the Hub id_token', () => pm.expect(idt.sub).to.eql(pm.environment.get('ns_hub_sub')));
pm.test('id_token has the same sid as the Hub id_token', () => pm.expect(idt.sid).to.eql(pm.environment.get('ns_hub_sid')));
pm.test('no new device_secret is issued', () => pm.expect(t.device_secret).to.be.undefined);
pm.environment.set('ns_card_access_token', t.access_token);
pm.environment.set('ns_card_id_token', t.id_token);
"""

exchange_ok = exchange(
    "Card 01 Token exchange with the device secret", "", EXCHANGE_OK_TEST,
    description="Grant `urn:ietf:params:oauth:grant-type:token-exchange`: subject_token = the Hub id_token, actor_token = device_secret, audience = the issuer, scope `openid email`. The Health Card signs its own client assertion; the DPoP proof is signed with the Hub's key, the one the device secret is bound to. In the Android apps the Hub signs this proof for the Health Card.")

userinfo_card = item(
    "Card 02 Userinfo with the Health Card token", "GET", ISS + "/oauth2/userinfo",
    pre=USERINFO_PRE % {"tokenvar": "ns_card_access_token"},
    test=USERINFO_TEST % {"subvar": "ns_hub_sub", "extra": """pm.test('userinfo has the email claim (scope email)', () => pm.expect(claims.email).to.be.a('string').and.not.empty);
pm.test('userinfo has no phone_number (scope phone not granted)', () => pm.expect(claims.phone_number).to.be.undefined);
pm.test('userinfo has no name claim (scope profile not granted to the Health Card)', () => pm.expect(claims.name).to.be.undefined);"""},
    description="The Health Card token is bound to the Hub's DPoP key, so the userinfo proof is signed with that key and carries `ath`.")

negatives = [
    exchange("Neg 01 Exchange: scope wider than the Hub grant -> interaction_required", "f.scope = 'openid email phone';",
             expect_error(400, "interaction_required") + r"""
pm.test('error_description names the missing scope', () => pm.expect(pm.response.json().error_description).to.include('phone'));
""",
             "The Hub was granted `openid profile email device_sso`. `phone` needs the user's consent, so the server asks the app to run its own authorization request (PAR with `prompt=consent`) instead of issuing tokens. Server setting `oauth.native_sso.scope_exceeded_error` can switch this error to `invalid_scope`."),
    exchange("Neg 02 Exchange: device_sso in the requested scope -> invalid_scope", "f.scope = 'openid email device_sso';",
             expect_error(400, "invalid_scope")),
    exchange("Neg 03 Exchange: audience is not the issuer -> invalid_target", "f.audience = 'https://other.example.com';",
             expect_error(400, "invalid_target")),
    exchange("Neg 04 Exchange: old device-secret token type -> invalid_request", "f.actor_token_type = 'urn:x-oath:params:oauth:token-type:device-secret';",
             expect_error(400, "invalid_request")),
    exchange("Neg 05 Exchange: tampered device secret -> invalid_request", "f.actor_token = f.actor_token.slice(0, -4) + (f.actor_token.slice(-4) === 'AAAA' ? 'BBBB' : 'AAAA');",
             expect_error(400, "invalid_request")),
    exchange("Neg 06 Exchange: DPoP proof from another key -> invalid_grant", "dpopKey = 'ns_card_key';",
             expect_error(400, "invalid_grant"),
             "The device secret is bound to the Hub's DPoP key (cnf.jkt). A proof signed by another key must not unlock it."),
    exchange("Neg 07 Exchange: no DPoP proof -> invalid_dpop_proof", "sendDpop = false;",
             expect_error(400, "invalid_dpop_proof")),
    exchange("Neg 08 Exchange: no actor_token -> invalid_request", "f.actor_token = null; f.actor_token_type = null;",
             expect_error(400, "invalid_request"),
             "With `oauth.native_sso.exchange_only: true` the token-exchange grant works only with a device secret."),
    exchange("Neg 09 Exchange: client of another relying party -> invalid_request",
             "f.client_id = pm.environment.get('ns_other_id'); f.client_assertion = sign('other');",
             expect_error(400, "invalid_request"),
             "The Hub and the app must belong to one relying party."),
    exchange("Neg 10 Exchange: client without the token-exchange grant -> unauthorized_client",
             "f.client_id = pm.environment.get('ns_plain_id'); f.client_assertion = sign('plain');",
             expect_error(400, "unauthorized_client")),
    item("Neg 11 PAR with device_sso from a client that does not allow it -> invalid_scope", "POST", ISS + "/oauth2/par",
         pre=PAR_PRE % {"who": "plain", "redirect": "'org.badmosip.nativesso.plain://oauth/callback'", "scope": "openid email device_sso", "dpopkey": "ns_hub_dpop_key"},
         body=form(PAR_BODY),
         test=r"""
pm.test('PAR is refused with 400', () => pm.response.to.have.status(400));
const e = pm.response.json();
pm.test('error is invalid_scope', () => pm.expect(e.error).to.eql('invalid_scope'));
pm.test('no request_uri is returned', () => pm.expect(e.request_uri).to.be.undefined);
""",
         description="`device_sso` is accepted only for a client whose additionalConfig.allowed_authorization_scopes lists it."),
    item("Neg 12 Userinfo without a DPoP proof -> 401", "GET", ISS + "/oauth2/userinfo",
         pre="pm.request.headers.upsert({ key: 'Authorization', value: 'DPoP ' + pm.environment.get('ns_card_access_token') });",
         test=r"""
pm.test('userinfo without a proof answers 401', () => pm.response.to.have.status(401));
"""),
    item("Neg 13 Userinfo with the Bearer scheme for a DPoP-bound token -> 401", "GET", ISS + "/oauth2/userinfo",
         pre="pm.request.headers.upsert({ key: 'Authorization', value: 'Bearer ' + pm.environment.get('ns_card_access_token') });",
         test=r"""
pm.test('a DPoP-bound token used as Bearer is refused with 401', () => pm.response.to.have.status(401));
"""),
]

# ----------------------------------------------------------------------------------------------
# Revocation of the device secret (RFC 7009): only the Hub may end its session
# ----------------------------------------------------------------------------------------------

REVOKE_PRE = PRE + SIGN_HELPERS + r"""
const who = '%(who)s';
pm.variables.set('ns_rev_client_id', pm.environment.get('ns_' + who + '_id'));
pm.variables.set('ns_rev_assertion', sign(who));
"""

REVOKE_BODY = form([("token", "{{ns_device_secret}}"), ("token_type_hint", "device_secret"),
                    ("client_id", "{{ns_rev_client_id}}"), ("client_assertion_type", ASSERT_TYPE),
                    ("client_assertion", "{{ns_rev_assertion}}")])

revocation = [
    item("Rev 01 Revoke the Hub device secret as the Health Card -> invalid_grant", "POST", ISS + "/oauth2/revoke",
         pre=REVOKE_PRE % {"who": "card"}, body=REVOKE_BODY,
         test=r"""
pm.test('answers 400', () => pm.response.to.have.status(400));
pm.test('error is invalid_grant', () => pm.expect(pm.response.json().error).to.eql('invalid_grant'));
""",
         description="The Health Card holds the device secret but it was issued to the Hub. Only the Hub may end the session (RFC 7009 2.1); nothing changes."),
    item("Rev 02 Revoke the device secret as the Hub (sign out of all apps) -> 200", "POST", ISS + "/oauth2/revoke",
         pre=REVOKE_PRE % {"who": "hub"}, body=REVOKE_BODY,
         test=r"""
pm.test('answers 200', () => pm.response.to.have.status(200));
pm.test('body is empty (RFC 7009 2.2)', () => pm.expect(pm.response.text()).to.eql(''));
""",
         description="What Hub's \"Sign out of all apps\" sends. The server ends the login session of the device secret."),
    exchange("Rev 03 Exchange after the revocation -> invalid_grant", "",
             expect_error(400, "invalid_grant"),
             "The session of the device secret has ended, so the Health Card can no longer sign in with it."),
]

cleanup = item(
    "99 Cleanup: remove keys and tokens from the environment", "GET", ISS + "/oauth2/jwks",
    pre=r"""
['hub', 'card', 'plain', 'other'].forEach((n) => ['key', 'pub', 'id'].forEach((s) => pm.environment.unset('ns_' + n + '_' + s)));
['ns_hub_dpop_key', 'ns_device_secret', 'ns_hub_access_token', 'ns_hub_id_token', 'ns_card_access_token',
 'ns_card_id_token', 'ns_code', 'ns_code_verifier', 'ns_assertion', 'ns_challenge_token', 'ns_request_uri',
 'ns_auth_id', 'ns_execution_id', 'ns_hub_sub', 'ns_hub_sid', 'ns_consent_prompt'].forEach((k) => pm.environment.unset(k));
""",
    test="pm.test('JWKS answers 200', () => pm.response.to.have.status(200));")

FOLDER = {
    "name": "Native SSO",
    "description": "OpenID Connect Native SSO for Mobile Apps 1.0 on eSignet Go: Hub sign-in with device_sso, device_secret, token exchange for the Health Card, re-consent error, negative cases, revocation of the device secret. Run top to bottom with the environment `Go-eSignet Native SSO (local stack)`. See README.md, section Native SSO.",
    "item": [
        {"name": "Setup", "item": setup},
        {"name": "Hub sign-in (first app)", "item": hub},
        {"name": "Health Card sign-in (second app)", "item": [exchange_ok, userinfo_card]},
        {"name": "Negative cases", "item": negatives},
        {"name": "Revocation", "item": revocation},
        {"name": "Cleanup", "item": [cleanup]},
    ],
}


def main():
    with open(COLLECTION, encoding="utf-8") as fh:
        c = json.load(fh)
    with open(os.path.join(HERE, "lib.js"), encoding="utf-8") as fh:
        lib = fh.read()
    assert "{{" not in lib, "lib.js must not contain {{ (Postman would substitute it)"
    c["item"] = [i for i in c["item"] if i["name"] != "Native SSO"]
    # The card counts the requests directly under "Native SSO": keep them flat and numbered by name.
    flat = []
    for sub in FOLDER["item"]:
        flat.extend(sub["item"])
    folder = dict(FOLDER)
    folder["item"] = flat
    c["item"].append(folder)
    vars_ = [v for v in c.get("variable", []) if v["key"] != "ns_lib"]
    vars_.append({"key": "ns_lib", "value": lib, "type": "string"})
    c["variable"] = vars_
    with open(COLLECTION, "w", encoding="utf-8", newline="\n") as fh:
        json.dump(c, fh, indent=2, ensure_ascii=True)
        fh.write("\n")
    print("Native SSO: %d requests" % len(flat))


if __name__ == "__main__":
    main()
