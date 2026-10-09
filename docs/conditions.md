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
| | `SessionsObserved` | The operator read the controller's active session list. `status.activeSessions`, `status.connectedServices`, and `status.connectedRouters` summarize it. |
| `ZitiJwtSigner` | `Synced` | The signer (and its auth policy) exist and match. |
| | `Ready` | Same as `Synced`. |
| `ZitiConfig`, `ZitiService`, `ZitiServicePolicy`, `ZitiEdgeRouterPolicy`, `ZitiServiceEdgeRouterPolicy`, `ZitiTerminator` | `Synced` | The Ziti object exists and matches the spec. |
| | `Ready` | Same as `Synced`. |
| | `InUse` | (`ZitiConfig` only) Some service uses the config. |
| `ZitiRouter` | `Synced` | The router exists in Ziti and matches the spec. |
| | `Enrolled` | The router has enrolled with its JWT. |
| | `Online` | The router is connected to the controller. |
| | `Serving` | Every service whose service edge router policy names this router has a terminator on it. |
| | `Workload` | The in-cluster router has a ready replica. Present only when `spec.deployment` is set. |
| | `Ready` | `Enrolled` and `Online`. |
| `ZitiCA` | `Synced` | The CA is registered in Ziti and matches the spec. |
| | `Verified` | Ziti accepted the proof that you control the CA. |
| | `Ready` | Same as `Verified`. |
| `ZitiSidecar` | `IdentityReady` | The `ZitiIdentity` has an `identity.json` for the tunneler to use. |
| | `Synced` | The patch matches the spec. |
| | `Ready` | `IdentityReady`, and the patch is written when `manifestSecretRef` is set. |
| `ZitiPortForward` | `IdentityReady` | The referenced identity is enrolled and its identity file exists. |
| | `Workload` | The proxy Deployment has a ready replica. |
| | `Ready` | The identity is ready and the proxy listener is ready. |
| `ZitiAccessPolicy` | `Synced` | The Dial policy (and the edge router policy) exist and match. |
| | `Ready` | The Dial policy selects at least one identity and one service. |

`Dialable` and `RoutePath` can be false before the first client exists. That is not an error.

`Serving` and `InUse` report on the network, not on the resource. They do not fail `Ready`. A router that no policy picked for serves nothing and `Serving` is true.

## Reasons when `Synced` is false

The operator did not finish writing to Ziti. Nothing more is written until the cause is fixed.

| Reason | Meaning | What to do |
|---|---|---|
| `InvalidSpec` | The operator rejected a value. Examples: a malformed port range, a router outside `hostingRouters`, `#all` or `@id` roles under `Namespaced`, an unknown auth policy. | Read the message and fix the spec. Nothing was created. |
| `NameConflict` | An entity with the wanted name exists in Ziti and this operator does not own it. | Set another `zitiName`. Or use `managementPolicy: Adopt` (identities) or `Observe`. Or remove the old entity. |
| `TargetNotFound` | A one-to-one kind names a Ziti object that does not exist yet: a role target (`@name`), or a config of a `ZitiService`. | Create it. The operator checks again after 30 seconds. |
| `NotFound` | `Adopt` or `Observe` found no entity named `zitiName`. | Fix `zitiName`, or create the entity in Ziti. |
| `ZitiRejected` | Ziti answered with a 4xx error. The message has the Ziti error. | Fix the spec. The operator retries in about 10 minutes. |
| `Error` | A temporary error such as a network failure or a 5xx answer. | None. The operator retries with backoff. |
| `NamespaceNotAllowed` | The connection's `allowedNamespaces` excludes this namespace. | Label the namespace, or change the selector. |
| `InvalidConnection` | The `allowedNamespaces` selector on the connection is invalid. | Fix the `ZitiConnection`. |
| `SecretConflict` | A Secret with the wanted name exists and this `ZitiIdentity` does not own it. | Set another `secretName`. |
| `KeyAmbiguous` | `ZitiJwtSigner` with `keys.kubernetes`: the cluster publishes several keys and the operator cannot tell which one signs tokens (it has no token to compare, or none matches). | Run the operator in the cluster, or use `keys.jwksEndpoint`. |
| `SecretNamespaceNotAllowed` | The operator runs with `--secret-namespaces` (chart value `rbac.secretNamespaces`) and this namespace is not in it. | Add the namespace to the list. |
| `ServiceNamespaceNotAllowed` | On `Workload`. The chart narrows where the operator may write Services (`rbac.serviceNamespaces`) and `spec.deployment.namespace` is not in it. The Deployment and the claim are still made, and the router runs without a stable address. | Add the namespace to the list, or apply the `service.yaml` from the enrollment Secret yourself. |
| `NameConflict` on a `Deployment`, `Service`, or `PersistentVolumeClaim` | An object of that name exists in `spec.deployment.namespace` and the operator did not create it. The operator never takes over a workload it does not own. | Rename the router, or delete the object. |

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
| `IdentityFileUnavailable` | `Adopt` with `OperatorEnrolled` found an identity that enrolled elsewhere. The operator has no key, so it stores no Secret and does not delete or re-enroll the identity. Re-enroll it in Ziti, or use `Manage`. |
| `CertExpired` | The client certificate expired. |
| `AwaitingVerification` (`ZitiCA`) | The CA is registered but not proven yet. The message has the token. Set `verification.signWithSecretKey` or verify by hand. |
| `VerificationFailed` (`ZitiCA`) | The proof was rejected or could not be signed (wrong or missing `tls.key`). The message has the cause. |
| `TokenLogin` (true) | `enrollmentMode: None`. The identity logs in with a token. There is nothing to enroll. |

`CertificateValid` is `False` with reason `ExpiresSoon` (fewer than 30 days left) or `Expired`. `OperatorEnrolled` identities renew on their own. With `JwtOnly`, the workload must renew or re-enroll.

## Other reasons

| Reason | Where | Meaning |
|---|---|---|
| `ConnectionFailed` | `ZitiConnection` `Connected` | Login failed. Check the Secret, the CA ConfigMap, and the URL. |
| `NoMatch` | `ZitiAccessPolicy` `Ready` | The Dial policy selects no identity or no service yet. |
| `IdentityNotFound` | `ZitiApp` `AccessResolved` | A name in `allow.identities` is not in Ziti. The policy still uses the identities that exist. |
| `DeploymentUnavailable` | `ZitiRouter` `Workload` | The router pod has no ready replica. Look at the pod in `spec.deployment.namespace`. |
| `ControllerVersionUnknown` | `ZitiRouter` `Workload` | The `ZitiConnection` has not reported a controller version, so the router image cannot be chosen. Set `spec.deployment.image`, or wait for the connection. |
| `StorageClassLocked` | `ZitiRouter` `Workload` | The volume claim was created with another storage class and a bound claim cannot change. Delete the claim, or set `storageClassName` to the class it already has. |
| `IdentityNotEnrolled` | `ZitiSidecar` or `ZitiPortForward` | The referenced `ZitiIdentity` has no enrolled `identity.json` yet. Wait for enrollment or use an identity with operator enrollment. |
| `QueryFailed` | `ZitiIdentity` `SessionsObserved` | The operator could not read the controller's session list. Identity readiness is not affected; the session summary is cleared. |

## Events

| Event | Meaning |
|---|---|
| `Created`, `Updated`, `Deleted` | The operator changed a Ziti entity. |
| `Released` | The operator removed its tags and left the entity in Ziti (`Orphan` or an adopted identity). |
| `Adopted` | The operator took over an existing identity. |
| `Enrolled`, `EnrollmentCreated`, `EnrollmentExpired` | Identity enrollment steps. |
| `EnrollmentRenewed` | A `ZitiRouter` had an expired unused JWT, and Ziti issued a new one. |
| `Verified`, `Renewed` | A `ZitiCA` was proven to Ziti, or its certificate changed and the CA was replaced. |
| `CertificateRenewed`, `RenewalFailed`, `CertificateExpiring` | Certificate lifecycle. |
| `IdentityFileLost` | See above. |
| `DeleteBlocked` | The operator cannot reach Ziti, so the resource cannot finish deleting. |
| `AppConflict`, `InvalidAnnotations`, `Unexposed` | A Kubernetes Service or Ingress with `ziti.alialjaffer.com/expose`. |
