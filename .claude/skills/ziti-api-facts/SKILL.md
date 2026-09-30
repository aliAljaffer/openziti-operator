---
name: ziti-api-facts
description: Use when you touch the Ziti REST client, entity bodies, tags, enrollment, certificates, terminators, external JWT signers, or a Ziti router running in a container. Lists behaviors of the Ziti Edge Management API (controller v2.0.4) that were tested, so you do not guess.
---

# Verified Ziti behavior (controller v2.0.4)

Each line was tested against a real controller. Test a new assumption the same way before you code it.

## Entities and tags

- The session token from `POST /authenticate?method=password` goes in the `zt-session` header. `Authorization: Bearer <token>` is ignored and every call returns 401 `UNAUTHORIZED`.
- A `PATCH` with the full policy body (name, type, semantic, roles, tags) keeps fields it leaves out, such as `postureCheckRoles`. A `PUT` clears them. A `PATCH` on a config replaces `data` as a whole (unset keys like `dialOptions` are dropped). `maxIdleTime` is not a service field in v2.0.4: a `PATCH` returns success and stores nothing.
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

## Edge routers

- `POST /edge-routers` takes `name`, `roleAttributes`, `isTunnelerEnabled`, `cost`, `noTraversal`, `disabled`, `tags`. It returns the ID only.
- The detail and the list show `enrollmentJwt`, `enrollmentToken`, and `enrollmentExpiresAt` until the router has enrolled (`isVerified: true`). The JWT expires after about three hours. `List` strips the JWT. Read it with `Client.Enrollment`.
- `PUT` and `PATCH` both work and keep the pending enrollment. `POST /edge-routers/{id}/re-enroll` issues a new JWT.
- A router in tunnel mode has an identity with the same name and ID as the router.
- The router enrollment JWT carries `iss` (the controller address), `em: erott`, `sub` (the router ID), and `exp` three hours out. The controller address comes from the token, so nothing else has to tell the router where the controller is.
- `enrollmentJwt` (about 1000 characters) and `enrollmentToken` (36 characters) are different things. `openziti/ziti-router` needs the JWT. The short token is not used by the container.
- After the router enrolls, `enrollmentJwt` is gone from both the detail and the list.

## Running a router in a container (spike 2026-09-30)

Tested with `openziti/ziti-router:2.0.4` against controller v2.0.4. The image tag has no `v` prefix. `v2.0.4` does not exist.

- `ZITI_ENROLL_TOKEN=<router enrollment JWT>`, `ZITI_BOOTSTRAP=true`, `ZITI_BOOTSTRAP_CONFIG=true`, `ZITI_BOOTSTRAP_ENROLLMENT=true`, and `ZITI_AUTO_RENEW_CERTS=true` bring a router to `isVerified: true` and `isOnline: true` with no other input.
- No CA bundle is needed. The image fetches the controller certificate itself and writes `router.cas`. It pins that certificate on first contact, so the first connection is the trust decision.
- `ZITI_CTRL_ADVERTISED_ADDRESS` can be empty. The image writes `endpoints.yml` from the `iss` claim of the JWT.
- The image writes `config.yml` from env vars. A plain Deployment needs no ConfigMap and no hand-written router configuration.
- Data directory `/ziti-router` holds `config.yml`, `endpoints.yml`, `router.cert`, `router.key`, `router.cas`, and `router.server.chain.cert`. There is no `identity.json`.
- The process runs as uid 2171 (`ziggy`). A fresh volume is root-owned and uid 2171 cannot write to it. In Docker, chown the volume first. In Kubernetes, set `fsGroup: 2171` on the pod.
- A router restarts from its data volume with no token at all. Delete the volume and the router needs a new enrollment.
- The generated `config.yml` binds the link listener and the edge listener to the same port when `ZITI_ROUTER_PORT` is set. It works.
- `ziti agent stats` runs in the container and works as a healthcheck.
- `ZITI_ROUTER_ADVERTISED_ADDRESS` becomes the router `hostname` in the management API and lands in the CSR SANs.
