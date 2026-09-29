# ADR 0002: Kubernetes service account tokens as Ziti identities

Date: 2026-09-29. Status: built. See `docs/service-account-tokens.md`.

## Question

Can a pod authenticate to Ziti with its service account token, with no enrollment step?

## Result: yes

Tested on controller v2.0.4 with a real token from a Talos cluster (`kubectl create token default --audience ziti-spike`).

| Step | API | Notes |
|---|---|---|
| 1. Signer | `POST /edge/management/v1/external-jwt-signers` | Fields: `issuer` (the token `iss`), `audience` (the token `aud`), `claimsProperty: sub`, `useExternalId: true`, and `certPem` with `kid`. The path is `external-jwt-signers`. The `ext-jwt-signers` path from the public docs page returns 404. |
| 2. Auth policy | `POST /auth-policies` | `primary.extJwt.allowed: true` and `primary.extJwt.allowedSigners: [<signer id>]`. Set `cert`, `updb` and `secondary.requireTotp` to their off values. |
| 3. Identity | `POST /identities` | `externalId` is the token subject, `system:serviceaccount:<namespace>:<name>`. Set `authPolicyId` to the policy above. |
| 4. Login | `POST /edge/client/v1/authenticate?method=ext-jwt` with `Authorization: Bearer <token>` | Returned a session for the matching identity. A token with another audience was rejected. |

## Findings that shape a design

- **Key material.** Kubernetes publishes its signing keys as a JWKS, not as an x509 certificate. The spike wrapped the RSA public key from the JWKS in a self-signed certificate and passed it as `certPem` with the JWKS `kid`. Ziti accepted it. A `jwksEndpoint` would avoid this, but the controller must then reach `https://<api-server>/openid/v1/jwks`. That endpoint needs authentication by default. Not tested.
- **Key rotation.** With `certPem`, a rotated Kubernetes signing key breaks login until the signer is updated. A `jwksEndpoint` follows rotation.
- **No enrollment.** An identity that logs in with a token has no certificate to enroll. `ZitiIdentity` would need a mode with no enrollment.
- **Trust.** The signer trusts every token from that issuer and audience. The audience must be specific to Ziti, and the projected token must request it.

## Decision

Built after the first release work, as these pieces:

1. `ZitiIdentity.spec.externalId`, so an identity can carry the service account subject.
2. An enrollment mode with no enrollment (`None`), so no JWT or `identity.json` is created.
3. Admins create the signer and the auth policy by hand, or a later `ZitiExternalJwtSigner` kind creates them. `spec.authPolicy` already names an auth policy.

Workloads still need a Ziti client (SDK or tunneler) that presents the token. The operator does not provide one.

## Result of building it

- `ZitiJwtSigner` (cluster-scoped) creates the signer and an auth policy. `ZitiIdentity` got `serviceAccount`, `externalId`, and `enrollmentMode: None`.
- Ziti allows one signer per issuer, so several signers cannot cover a key rotation. With `keys.kubernetes` the operator picks the key named in the header of its own token and updates the signer at the next sync. `keys.jwksEndpoint` follows rotation at once.
- The Ziti controller returns `certPem` with an extra newline. The comparison trims whitespace.
