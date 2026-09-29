---
name: ziti-api-facts
description: Use when you touch the Ziti REST client, entity bodies, tags, enrollment, certificates, terminators, or external JWT signers. Lists behaviors of the Ziti Edge Management API (controller v2.0.4) that were tested, so you do not guess.
---

# Verified Ziti behavior (controller v2.0.4)

Each line was tested against a real controller. Test a new assumption the same way before you code it.

## Entities and tags

- Tag filter keys allow only letters, `-`, and `_`. Filter: `tags.ziti-operator-uid="<uid>"`. Dots, digits, and slashes fail to parse.
- Role selectors must use `@<id>`. `@<name>` returns 400. Resolve names to IDs first.
- The controller sorts role lists. Compare string lists as sets.
- `PUT` replaces the entity and clears tags left out. A `PATCH` on `tags` replaces the whole map, `{}` clears it, and `null` values are rejected. A `PATCH` on a config needs `data`, so release configs with a `PUT`.
- A duplicate name returns 400 `COULD_NOT_VALIDATE`. Names over 1024 bytes return a 500.
- Creating a service needs `encryptionRequired`.
- An identity's `type` comes back as an object. The identity list shows pending `enrollment` and a non-empty `authenticators` map once enrolled.

## Routers, services, terminators

- A router in tunnel mode has an identity with the same ID as the router. Bind policies use `@<router id>`.
- A bind from a router that is not in the service edge router policy makes no terminator.
- `host.v2` takes a `terminators` list. Two entries on one router give two Ziti terminators within seconds. No controller restart is needed.
- Port ranges work in `intercept.v1` `portRanges` and `host.v2` `allowedPortRanges`. `forwardPort` sends the dialed port on.
- `intercept.v1` capturing UDP while the host config allows only TCP captures the traffic and then refuses it.

## Enrollment and certificates

- `POST /identities` with `enrollment.ott=true` makes one enrollment. Read its JWT with `GET /enrollments?filter=identity="<id>"`. `POST /enrollments` fails with `ENROLLMENT_EXISTS` while one exists. Delete it first.
- Certificate renewal: log in on the client API with the identity certificate (`POST /edge/client/v1/authenticate?method=cert`), then `POST /current-identity/authenticators/{id}/extend` with a CSR, then `.../extend-verify`. After verify, the old certificate is rejected with 401. Store the new file at once.
- The SDK `enroll.Enroll` performs one-time-token enrollment. `identity.json` holds `ztAPI`, and `id.key`, `id.cert`, `id.ca` as `pem:` strings.

## External JWT signers

- Path is `/external-jwt-signers`. Fields: `issuer`, `audience`, `claimsProperty`, `useExternalId`, and `certPem` with `kid` or `jwksEndpoint`.
- The `issuer` must be unique across signers, and so must the certificate fingerprint. One signer per issuer, so several signers cannot cover a key rotation. Follow rotation with a `jwksEndpoint`, or update the one signer when the key changes.
- The controller returns `certPem` with an extra trailing newline.
- An auth policy allows a signer with `primary.extJwt.allowed=true` and `allowedSigners=[<signer id>]`. Identities match the token claim to `externalId`. An identity may have no enrollment.
- Log in with `POST /edge/client/v1/authenticate?method=ext-jwt` and `Authorization: Bearer <token>`.
- Kubernetes publishes signing keys as a JWKS. Wrap the public key in a self-signed certificate to use `certPem`.

## Certificate authorities

- `POST /cas` needs `certPem`, `isAuthEnabled`, `isAutoCaEnrollmentEnabled`, `isOttCaEnrollmentEnabled`, `identityRoles`, and `identityNameFormat`. `externalIdClaim` needs `location`, `matcher`, `matcherCriteria`, `parser`, and `parserCriteria`, all sent, criteria may be empty. A `PUT` also needs `identityNameFormat`.
- A certificate can be registered as a CA once.
- The CA returns `verificationToken` (a short string) and `isVerified: false`. Verify with `POST /cas/{id}/verify`, `Content-Type: text/plain`, body a PEM certificate signed by the CA with common name equal to the token. The token disappears after that.
- `certPem` cannot change. `PUT` and `PATCH` return success and keep the old certificate. Replace the CA to follow a renewed issuer. The fingerprint is the SHA-1 of the DER certificate.
- With `isAuthEnabled` and an `externalIdClaim`, a client certificate signed by a verified CA logs in as the identity whose `externalId` equals the claim value (`POST /edge/client/v1/authenticate?method=cert` over mTLS). A fresh certificate with the same common name works, so renewal needs no re-enrollment. An unknown name gets 401.
- An `externalId` must be unique across identities.
