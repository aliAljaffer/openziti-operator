# Conditions and reasons

Every resource reports its state in `status.conditions`. Read them with:

```sh
kubectl get ztapp billing -o jsonpath='{range .status.conditions[*]}{.type}={.status} {.reason}: {.message}{"\n"}{end}'
```

`kubectl get -o wide` shows the `Ready` message. `kubectl describe` shows Events for every create, update, and delete of a Ziti entity.

## Conditions by kind

| Kind | Condition | True when |
|---|---|---|
| `ZitiConnection` | `Connected` | The operator logged in and read the controller version. |
| `ZitiApp` | `Synced` | Every Ziti entity exists and matches the spec. |
| | `Hosted` | At least one terminator exists. |
| | `Dialable` | A Dial policy selects at least one identity. |
| | `RoutePath` | A client identity has an online router that the app may use. |
| | `AccessResolved` | Every name in `allow.identities` exists in Ziti. Present only when `allow.identities` is set. |
| | `Ready` | `Hosted`, `Dialable`, and `RoutePath` are true. |
| `ZitiIdentity` | `Synced` | The identity exists and matches the spec. |
| | `Ready` | The identity is enrolled and its certificate is valid. |
| | `CertificateValid` | The certificate has more than 30 days left. Present once the identity is enrolled. |
| `ZitiAccessPolicy` | `Synced` | The Dial policy (and the edge router policy) exist and match. |
| | `Ready` | The Dial policy selects at least one identity and one service. |

`Dialable` and `RoutePath` can be false before the first client exists. That is not an error.

## Reasons when `Synced` is false

The operator did not finish writing to Ziti. Nothing more is written until the cause is fixed.

| Reason | Meaning | What to do |
|---|---|---|
| `InvalidSpec` | The operator rejected a value. Examples: a malformed port range, a router outside `hostingRouters`, `#all` or `@id` roles under `Namespaced`, an unknown auth policy. | Read the message and fix the spec. Nothing was created. |
| `NameConflict` | An entity with the wanted name exists in Ziti and this operator does not own it. | Set another `zitiName`. Or use `managementPolicy: Adopt` (identities) or `Observe`. Or remove the old entity. |
| `NotFound` | `Adopt` or `Observe` found no entity named `zitiName`. | Fix `zitiName`, or create the entity in Ziti. |
| `ZitiRejected` | Ziti answered with a 4xx error. The message has the Ziti error. | Fix the spec. The operator retries in about 10 minutes. |
| `Error` | A temporary error such as a network failure or a 5xx answer. | None. The operator retries with backoff. |
| `NamespaceNotAllowed` | The connection's `allowedNamespaces` excludes this namespace. | Label the namespace, or change the selector. |
| `InvalidConnection` | The `allowedNamespaces` selector on the connection is invalid. | Fix the `ZitiConnection`. |
| `SecretConflict` | A Secret with the wanted name exists and this `ZitiIdentity` does not own it. | Set another `secretName`. |
| `SecretNamespaceNotAllowed` | The operator runs with `--secret-namespaces` (chart value `rbac.secretNamespaces`) and this namespace is not in it. | Add the namespace to the list. |

## Reasons on `Hosted`, `Dialable`, and `RoutePath`

| Condition | Reason | Meaning |
|---|---|---|
| `Hosted` | `NoTerminator` | The service has no terminator. The hosting router has not reached the target, or the bind policy does not select it. |
| | `NoBind` | No Bind policy selects a hosting identity. |
| | `InertBind` | A router binds the service but is not in its router policy, so it makes no terminator. |
| | `MissingConfig` | The service lacks an intercept or a host config. |
| | `ProtocolMismatch` | The intercept captures a protocol that the host config does not allow. |
| `Dialable` | `NoDialer` | No Dial policy selects an identity. Add `allow.groups` or `allow.identities`, or give identities the right role attributes. |
| `RoutePath` | `NoCommonRouter` | The routers that the dialing identities may use do not overlap with the routers the service may use. Set `entryRouters`. |
| | `OfflinePath` | The only common routers are offline. |

## Reasons on `ZitiIdentity` `Ready`

| Reason | Meaning |
|---|---|
| `Enrolled` (true) | The identity is enrolled. |
| `PendingEnrollment` | The identity is not enrolled yet. With `JwtOnly`, the enrollment JWT is in the Secret. |
| `IdentityFileLost` | The Secret has no working `identity.json`. The operator deleted the Ziti identity and enrolls a new one. Its Ziti ID changes. |
| `CertExpired` | The client certificate expired. |

`CertificateValid` is `False` with reason `ExpiresSoon` (fewer than 30 days left) or `Expired`. `OperatorEnrolled` identities renew on their own. With `JwtOnly`, the workload must renew or re-enroll.

## Other reasons

| Reason | Where | Meaning |
|---|---|---|
| `ConnectionFailed` | `ZitiConnection` `Connected` | Login failed. Check the Secret, the CA ConfigMap, and the URL. |
| `NoMatch` | `ZitiAccessPolicy` `Ready` | The Dial policy selects no identity or no service yet. |
| `IdentityNotFound` | `ZitiApp` `AccessResolved` | A name in `allow.identities` is not in Ziti. The policy still uses the identities that exist. |

## Events

| Event | Meaning |
|---|---|
| `Created`, `Updated`, `Deleted` | The operator changed a Ziti entity. |
| `Released` | The operator removed its tags and left the entity in Ziti (`Orphan` or an adopted identity). |
| `Adopted` | The operator took over an existing identity. |
| `Enrolled`, `EnrollmentCreated`, `EnrollmentExpired` | Identity enrollment steps. |
| `CertificateRenewed`, `RenewalFailed`, `CertificateExpiring` | Certificate lifecycle. |
| `IdentityFileLost` | See above. |
| `DeleteBlocked` | The operator cannot reach Ziti, so the resource cannot finish deleting. |
| `AppConflict`, `InvalidAnnotations`, `Unexposed` | A Kubernetes Service with `alialjaffer.ziti/expose`. |
