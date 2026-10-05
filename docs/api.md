# API documentation index

Which API documents exist, what Native SSO adds to eSignet's endpoints, the errors a client can get, and how to try the
requests. The full request and response schemas are in the OpenAPI files; this page is the map. Written 2026-10-05.

## Documents

| Document | What it covers |
|---|---|
| [../esignet/docs/esignet-openapi.yaml](../esignet/docs/esignet-openapi.yaml) | eSignet's public API as served on port 38088 (`/oauth2/par`, `/oauth2/authorize`, `/oauth2/token`, `/oauth2/userinfo`, `/client-mgmt/client`, discovery, login `/flow/execute`), with the Native SSO additions marked in each section and a client registration example for a Native SSO app |
| [../thunder/api/oauth2.yaml](../thunder/api/oauth2.yaml) | Thunder's token endpoint with the Native SSO token exchange (the file has no discovery path) |
| [../esignet/postman-collection/README.md](../esignet/postman-collection/README.md#native-sso) | the Postman folder "Native SSO": 31 requests, how it signs, how to run it |
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

## Token exchange request

Sent by the second app (Health Card) to `POST /oauth2/token`, `application/x-www-form-urlencoded`, with a `DPoP` header
whose proof is signed by the first app's DPoP key.

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

Success: `200` with `access_token` (`token_type` DPoP, `cnf.jkt` = the first app's key), `id_token` (`aud` = second app,
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
| Device secret invalid, tampered or expired; id_token signature, issuer or age; `ds_hash` or `sid` mismatch; clients of another relying party | `invalid_request` | R09, R11, R12, R19, R20 |
| Login session ended (the server restarted, or the secret's lifetime ran out of its session record) | `invalid_grant` | R12 |
| DPoP key differs from the one the secret is bound to | `invalid_grant` | R14 |
| DPoP proof malformed or replayed | `invalid_dpop_proof` | R22 |
| `audience` is not the issuer | `invalid_target` | R18 |
| Requesting client lacks `device_sso` or the token-exchange grant | `unauthorized_client` | R13 |
| `device_sso` in the exchange scope, or no `openid` | `invalid_scope` | R16 |
| Scope beyond the first app's grant (setting `scope_exceeded_error`, default) | `interaction_required` | R15 |
| Any other token exchange while eSignet allows only Native SSO | `invalid_request` | R26 |

On `interaction_required` the second app runs its own authorization-code login with `prompt=consent` for the missing scope
(bonus B1). eSignet Go keeps no browser session, so the user enters the UIN and a new OTP there.

## Try it

| Way | Command | Needs |
|---|---|---|
| Newman (Postman CLI) | `bash scripts/with-stack.sh bash esignet/postman-collection/native-sso/run.sh` | Docker, newman 6; passed 31 requests, 116 assertions, 0 failed on 2026-10-05 |
| Postman app | import the collection and the environment "Go-eSignet Native SSO (local stack)", start the stack with `bash deploy/stack.sh up`, run the folder "Native SSO" top to bottom | Postman desktop; not run by the team (limit L07) for the upstream folders |
| Go client | `bash scripts/with-stack.sh bash e2e/run.sh` | Docker; 29 live tests, `e2e/client` shows each request |

The folder signs its own client assertions and DPoP proofs in pre-request scripts, so no key is pasted by hand. Seeded
user and OTP: [test-data.md](test-data.md).
