# Diagrams

Mermaid sources (`.mmd`) and their PNG renderings. GitHub renders the Mermaid copies embedded in
[../design.md](../design.md); the PNGs are for the slides and for viewers without Mermaid.

| Source | Shows | Used in |
|---|---|---|
| [components.mmd](components.mmd) | Apps, eSignet Go with the Thunder engine and its providers, stores | design section 5 |
| [seq-super-app-login.mmd](seq-super-app-login.mmd) | Super App sign-in (PAR, PKCE, DPoP, `private_key_jwt`, OTP, consent) and device secret issuance | design section 6.1 |
| [seq-child-app-sso.mmd](seq-child-app-sso.mmd) | Child App silent sign-in by token exchange, proof signed by the Super App | design section 6.2 |
| [seq-reconsent.mmd](seq-reconsent.mmd) | Re-consent when the Child App asks for more than the Super App's grant and needs new user data (own login) | design section 6.3 |
| [seq-consent-only.mmd](seq-consent-only.mmd) | Consent-only re-consent: eSignet's consent page with no login, then the same token exchange | design section 6.3 |
| [exchange-validation.mmd](exchange-validation.mmd) | Order of checks at the token exchange and the error each one returns | design section 11 |
| [user-flow.mmd](user-flow.mmd) | The statement's user journey, steps 1-11, with the failure branches | design section 6.4 |

Rules:
- The `.mmd` file is the source. A copy embedded in a Markdown file sits right after a
  `<!-- source: diagrams/<name>.mmd -->` line, and `bash scripts/docs-check.sh docs/design.md docs/threat-model.md`
  fails when the copy and the source differ.
- After changing a source, render its PNG again (Docker, Mermaid CLI 12.0.0, image
  `minlag/mermaid-cli@sha256:2c5d27365df71da06c288beebe531ea3637cf9b422b871239a3b016233591d24`, used on 2026-10-04):

  ```bash
  cd docs/diagrams
  for f in *.mmd; do
    MSYS_NO_PATHCONV=1 docker run --rm -v "$(cygpath -m "$PWD" 2>/dev/null || pwd):/data" \
      minlag/mermaid-cli -i "/data/$f" -o "/data/${f%.mmd}.png" -b white -s 2
  done
  ```

  `MSYS_NO_PATHCONV` and `cygpath` matter only in Git Bash on Windows; elsewhere they do nothing.
