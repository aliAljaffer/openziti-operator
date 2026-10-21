---
name: manage-identities
description: Use when you create, enroll, adopt, or remove a ZitiIdentity, or log a workload in with a certificate (ZitiCA, cert-manager), a service account token (ZitiJwtSigner), a sidecar tunneler (ZitiSidecar), or a port-forward proxy (ZitiPortForward).
---

# Identities and workload login

Details: `docs/reference/ZitiIdentity.md`, `docs/cert-manager.md`, `docs/service-account-tokens.md`, `docs/sidecar.md`, `docs/port-forward.md`, `docs/existing-resources.md`, and the matching `examples/` folders.

## Choose the identity mode (`enrollmentMode`, immutable)

| Mode | Result | Use for |
|---|---|---|
| `JwtOnly` (default) | `enrollment.jwt` in Secret `secretName`. The holder enrolls. | A person or device. Cannot auto-renew. |
| `OperatorEnrolled` | Operator enrolls, writes `identity.json`, renews the certificate. | A pod that mounts the file. |
| `None` | No enrollment. Logs in by token or certificate. Needs `authPolicy` and `serviceAccount` or `externalId`. | Token or certificate login. |

`serviceAccount` and `externalId` are exclusive. A raw `externalId` needs `roleScope: Global`. `serviceAccount` is safe in every scope.

## Pick a login method for a workload

1. **Identity file** (`OperatorEnrolled`) plus a tunneler: `ZitiSidecar` or `ZitiPortForward`.
2. **Service account token**: `ZitiJwtSigner` (cluster-scoped) with `keys.kubernetes: {}`, then `ZitiIdentity` with `enrollmentMode: None`, `serviceAccount`, `authPolicy` = signer name. Pod needs a projected token with the signer `audience`. One signer per issuer.
3. **Certificate**: `ZitiCA` from a cert-manager CA Secret with `verification.signWithSecretKey: true`, then `ZitiIdentity` with `enrollmentMode: None`, `externalId`, and `certificate.issuerRef`. The operator creates the Certificate whose CN is `externalId`. Use a CA made only for Ziti. Add the CA Secret namespace to `rbac.secretNamespaces`. `kubectl get ztca` `VERIFIED` must be `true`.

## Sidecar and port-forward

- `ZitiSidecar` (`ztc`) writes a patch to `manifestSecretRef`. It does not patch your Deployment. Apply key `sidecar-patch.yaml` with `kubectl patch --patch-file --type=strategic`. `Host` mode (tproxy) needs `NET_ADMIN`, `NET_RAW`, and `resolver` set to the cluster DNS (`udp://<ip>:53`). `Proxy` mode needs no capability and does not resolve names.
- `ZitiPortForward`: needs an enrolled identity and a `service` and `port`. Wait for `Ready`, then `kubectl -n <ns> port-forward deployment/<name> <port>:<port>`. `Ready` does not prove the identity may dial the service.

## Existing identity

`managementPolicy: Adopt` plus the exact `zitiName` takes over an identity. `Observe` only reads. Delete never removes an adopted identity.

## Checks

`kubectl get ztid`: `ENROLLED`, `READY`, cert expiry, sessions. Certificates renew when the smaller of 30 days and one third of the lifetime remains. Failures: `.claude/skills/troubleshoot-resources/SKILL.md`.
