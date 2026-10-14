# API documentation index

Which API documents exist, what Native SSO adds to eSignet's endpoints, the errors a client can get, and how to try the
requests. The full request and response schemas are in the OpenAPI files; this page is the map. Written 2026-10-05.

## Documents

| Document | What it covers |
|---|---|
| [../esignet/docs/esignet-openapi.yaml](../esignet/docs/esignet-openapi.yaml) | eSignet's public API as served on port 38088 (`/oauth2/par`, `/oauth2/authorize`, `/oauth2/token`, `/oauth2/revoke`, `/oauth2/userinfo`, `/client-mgmt/client`, discovery, login `/flow/execute`), with the Native SSO additions marked in each section and a client registration example for a Native SSO app |
| [../thunder/api/oauth2.yaml](../thunder/api/oauth2.yaml) | Thunder's token endpoint with the Native SSO token exchange, and its revocation endpoint with device secrets (the file has no discovery path) |
| [../esignet/postman-collection/README.md](../esignet/postman-collection/README.md#native-sso) | the Postman folder "Native SSO": 43 requests, how it signs, how to run it |
| [../esignet/docs/configuration.md](../esignet/docs/configuration.md) | the settings `oauth.native_sso.*` and `native_sso.*` and their `MOSIP_ESIGNET_NATIVE_SSO_*` variables |
| [design.md](design.md) sections 6, 9, 11, 13, 14 | flows, device secret format, validation order, error table, configuration |
| [../android/README.md](../android/README.md) | the binder contract between the two apps (`IFamilySso.aidl`) and the OpenID Connect client library |

Lint results (2026-10-05): the eSignet file `0 errors, 8 warnings` (the import has the same 8), Thunder's `0 errors,
13 warnings` (same as the import): [test-report.md](test-report.md).

## What Native SSO adds

| Endpoint | Addition | Rules |
|---|---|---|
| Discovery `/.well-known/openid-configuration` | `native_sso_supported: true`, `device_sso` in `scopes_supported`, the token-exchange grant in `grant_types_supported` (only when the server has a Device Secret Provider and allows the grant) | R24 |
| Client registration `POST /client-mgmt/client` | grant type `urn:ietf:params:oauth:grant-type:token-exchange`; `additionalConfig.allowed_authorization_scopes` lists `device_sso` | R25, R02 |
| `POST /oauth2/par`, `GET /oauth2/authorize` | scope `device_sso` asks for a device secret; refused with `invalid_scope` for a client not allowed it; a DPoP key (header or `dpop_jkt`) is needed because the secret is bound to it | R01, R02, R07 |
| `POST /oauth2/token`, grant `authorization_code` | with `device_sso` granted: response also has `device_secret`; the id_token has `sid` and `ds_hash`; the access token and the secret are bound to the DPoP key (`cnf.jkt`) | R01, R04-R06 |
| `POST /oauth2/token`, grant `urn:ietf:params:oauth:grant-type:token-exchange` | the device-secret profile below | R09-R23, R26 |
| `GET /oauth2/userinfo` | for a token from an exchange, only the claims of the granted scope | R17 |
| `POST /oauth2/revoke` (RFC 7009) | the first app revokes its `device_secret` (`token_type_hint=device_secret`); the login session ends and every later exchange with it is `invalid_grant`; discovery lists `revocation_endpoint` | R27 |

## Token exchange request

Sent by the second app (Health Card) to `POST /oauth2/token`, `application/x-www-form-urlencoded`, with a `DPoP` header
whose proof is signed by the first app's DPoP key (the key the device secret is bound to).

| Parameter | Value |
|---|---|
| `grant_type` | `urn:ietf:params:oauth:grant-type:token-exchange` |
| `subject_token` | the first app's `id_token` (may be expired; `ds_hash` and `sid` must match the device secret) |
| `subject_token_type` | `urn:ietf:params:oauth:token-type:id_token` (underscore; the hyphen form of the specification example is refused) |
| `actor_token` | the first app's `device_secret` |
| `actor_token_type` | `urn:openid:params:token-type:device-secret` |
| `audience` | exactly the issuer, `http://localhost:38088` on the local stack |
| `scope` | optional; must contain `openid`, must not contain `device_sso`, must lie within the first app's scope; default is that scope without `device_sso` |
| `client_assertion_type`, `client_assertion` | the second app's own `private_key_jwt` (ES256, `aud` = issuer) |
| `dpop_jkt` | optional eSignet profile extension, not sent by the demo apps. RFC 9449 section 10 defines `dpop_jkt` for the authorization request; at the token endpoint it is this profile's own parameter. Value: the SHA-256 thumbprint of the second app's own DPoP key; the issued access token is bound to it, so the second app signs its own proofs for userinfo and its APIs. Without it (the statement's flow) the token is bound to the first app's key, and the first app signs the second app's userinfo proofs. The named key is not proven (see the threat model, section 6). Not a 43-character base64url value: `invalid_request` |

Success: `200` with `access_token` (`token_type` DPoP, `cnf.jkt` = the first app's key, or the `dpop_jkt` key with the extension, and the login's `sid`), `id_token` (`aud` = second app,
same `sub` and `sid`, no `ds_hash`), `issued_token_type`, `expires_in`, `scope`. No `device_secret` and no `ds_hash`.

Example with the values shortened (a worked run with real tokens is the Postman folder, request "Card 01 Token exchange with the device secret"):

```http
POST /oauth2/token HTTP/1.1
Host: localhost:38088
Content-Type: application/x-www-form-urlencoded
DPoP: eyJ0eXAiOiJkcG9wK2p3dCIs...

grant_type=urn%3Aietf%3Aparams%3Aoauth%3Agrant-type%3Atoken-exchange
&subject_token=eyJhbGciOiJFUzI1NiIs...&subject_token_type=urn%3Aietf%3Aparams%3Aoauth%3Atoken-type%3Aid_token
&actor_token=eyJhbGciOiJSU0EtT0FFUC0yNTYiLCJlbmMiOiJBMjU2R0NNIn0...
&actor_token_type=urn%3Aopenid%3Aparams%3Atoken-type%3Adevice-secret
&audience=http%3A%2F%2Flocalhost%3A38088&scope=openid+email
&client_assertion_type=urn%3Aietf%3Aparams%3Aoauth%3Aclient-assertion-type%3Ajwt-bearer&client_assertion=eyJhbGciOiJFUzI1NiIs...
```

## Revoking the device secret

Sent by the first app (Hub) when the user signs out of all apps: `POST /oauth2/revoke`,
`application/x-www-form-urlencoded`, no DPoP header.

| Parameter | Value |
|---|---|
| `token` | the first app's `device_secret` |
| `token_type_hint` | `device_secret` (without a hint, a compact JWE is taken as a device secret) |
| `client_id`, `client_assertion_type`, `client_assertion` | the first app's own `private_key_jwt` (ES256, `aud` = issuer) |

| Answer | When |
|---|---|
| `200`, empty body | revoked; also for a value that is not a valid device secret or whose session already ended (RFC 7009 2.2) |
| `400 invalid_grant` | the device secret was issued to another client, for example the second app sends it; nothing changes |
| `400 unsupported_token_type` | not a device secret: eSignet runs with `oauth.token_revocation.enabled: false`, so access and refresh tokens are not revocable here |
| `401 invalid_client` | client authentication failed |
| `500 server_error` | the session store could not be reached |

The second app can no longer get new tokens, and the access tokens it already got are refused from then on:
they carry the session's `sid`, and userinfo answers `401 invalid_token` (introspection `active: false`) once the
session has ended. A resource server that checks the JWT without asking eSignet accepts them until their `exp`.

## Errors

All errors are JSON `{"error": ..., "error_description": ...}`, HTTP 400 unless noted. The live test of each row is named in
[test-report.md](test-report.md).

| Situation | `error` | Rule |
|---|---|---|
| `device_sso` requested by a client not allowed it (PAR, authorize, code grant) | `invalid_scope` | R02 |
| `device_sso` code grant without DPoP from a client not registered as DPoP-bound | `invalid_request` | R07 |
| Any token request without a DPoP proof from a DPoP-bound client | `invalid_dpop_proof` | R07, R14 |
| Device secret generation fails (500, no tokens) | `server_error` | R08 |
| Exchange: wrong token types, missing `actor_token`, the old `urn:x-oath:` token type | `invalid_request` | R10 |
| Device secret invalid, tampered or expired; id_token signature or issuer; `ds_hash` or `sid` mismatch; clients of another relying party | `invalid_request` | R09, R11, R12, R19, R20 |
| Login session ended (the server restarted, or the secret's lifetime ran out of its session record) | `invalid_grant` | R12 |
| The first app's id_token is older than `id_token_max_age_seconds` (checked after `ds_hash` and `sid`). This follows the Native SSO session rule, not RFC 8693 2.2.2's `invalid_request`: only a new login of the first app helps | `invalid_grant` | R21 |
| DPoP key differs from the one the secret is bound to | `invalid_grant` | R14 |
| DPoP proof malformed or replayed | `invalid_dpop_proof` | R22 |
| `audience` is not the issuer | `invalid_target` | R18 |
| Requesting client lacks `device_sso` or the token-exchange grant | `unauthorized_client` | R13 |
| `device_sso` in the exchange scope, or no `openid` | `invalid_scope` | R16 |
| Scope beyond the first app's grant (setting `scope_exceeded_error`, default) | `interaction_required` | R15 |
| Any other token exchange while eSignet allows only Native SSO | `invalid_request` | R26 |
| Exchange after the first app revoked its device secret | `invalid_grant` | R27 |
| Scope beyond the grant that needs consent only (no new user data) | `interaction_required` with `consent_uri` and `consent_expires_in` | R28 |

When the answer carries `consent_uri` (bonus B1, consent only), the second app opens it in a Custom Tab. It is
eSignet's page `GET /oauth2/native-sso/consent?ticket=...` with "Allow" and "Not now" and no login. The form
(`POST /oauth2/native-sso/consent`, `ticket`, `decision=allow|deny`) answers with a 303 to the app's registered
redirect URI with `native_sso_consent=granted` or `denied`; after `granted` the app sends the same token exchange
again and gets the extra scope. Example answer:

```json
{"error":"interaction_required",
 "error_description":"The requested scope exceeds the scope granted to the first app and can be approved at consent_uri without a new login; user consent is needed for: health_card_share",
 "consent_uri":"http://localhost:38088/oauth2/native-sso/consent?ticket=4n3h0yW2qGvJ8lq9oP0bq1yQx7m0f6Zc1tVdQe8uFh0",
 "consent_expires_in":300}
```

Without `consent_uri` (the missing scope needs new user data, such as `phone`) the second app runs its own
authorization-code login with `prompt=consent` for the missing scope. eSignet Go keeps no browser session, so the
user enters the UIN and a new OTP there. Full description: [oauth2.yaml](../thunder/api/oauth2.yaml).

## Try it

| Way | Command | Needs |
|---|---|---|
| Newman (Postman CLI) | `bash scripts/with-stack.sh bash esignet/postman-collection/native-sso/run.sh` | Docker, newman 6; 43 requests (results: [test-report.md](test-report.md)) |
| Postman app | import the collection and the environment "Go-eSignet Native SSO (local stack)", start the stack with `bash deploy/stack.sh up`, run the folder "Native SSO" top to bottom | Postman desktop; not run by the team (limit L07) for the upstream folders |
| Go client | `bash scripts/with-stack.sh bash e2e/run.sh` | Docker; 32 live tests, `e2e/client` shows each request |

The folder signs its own client assertions and DPoP proofs in pre-request scripts, so no key is pasted by hand. Seeded
user and OTP: [test-data.md](test-data.md).
