# ADR 0001: Build a new operator

- Status: Accepted
- Date: 2026-09-29

## Decision

Build a new operator in this repository. Do not fork or contribute to the Miniziti operator. Do not duplicate ziti-k8s-agent.

## Options compared

| | Contribute to Miniziti | Fork Miniziti | Build new |
|---|---|---|---|
| API fit | Needs a new group, a connection CRD, role scope, and tag ownership. This is a rewrite. | Same rewrite, in a fork | Designed for PLAN.md |
| Review path | 1 maintainer, 0 issues or PRs so far | None needed | None needed |
| Reusable parts | Finalizers, envtest, kind e2e | Same | Copy under Apache-2.0 with attribution |

## Facts

[sixfeetup/miniziti-operator](https://github.com/sixfeetup/miniziti-operator), checked 2026-09-29:

- Apache-2.0, kubebuilder v4, Go 1.25, edge-api v0.27.5.
- 47 commits, 1 contributor, no releases. The author calls it early and not used in production.
- API group `ziti.sixfeetup.com/v1alpha1`. Kinds `ZitiIdentity`, `ZitiService`, `ZitiAccessPolicy`, all namespaced.
- No ownership tags. It finds entities by name.
- No connection CRD. Credentials come from one Secret.
- Conditions are `Ready`, `Reconciling`, `Degraded`. None are derived from terminators.
- No Helm chart.

[netfoundry/ziti-k8s-agent](https://github.com/netfoundry/ziti-k8s-agent): `ZitiController`, `ZitiRouter`, and `ZitiWebhook` install components and inject sidecars. They do not manage services or policies.

OpenZiti maintainers ([forum](https://openziti.discourse.group/t/kubernetes-operator/5226)) suggest a personal repository first. They may accept a project later if it is "directionally correct". They plan to generate CRDs from the OpenAPI spec, so our CRD fields stay close to edge-api model names.

## Spike results (controller v2.0.4)

| Question | Result |
|---|---|
| edge-api module | `v0.36.0`, the version in the `openziti/ziti` v2.0.4 `go.mod` |
| Login, list, create, delete | Pass. `internal/ziti/spike_test.go`, tag `integration`. |
| Tag filter | `tags.<key>="<v>"` works only for keys with letters, `-`, or `_`. `tags.alialjaffer.com/uid` and `tags.k8sUid` fail to parse. Keys use the `ziti-operator-` prefix. |
| In-cluster Service DNS name in the management API certificate | Yes for the Helm chart defaults. The SANs include `<release>-mgmt`, `.<ns>`, `.<ns>.svc`, and `.<ns>.svc.cluster.local`. |
| Scoped non-admin identity | Yes. `permissions` on the identity is enforced. The spike test passes as a non-admin identity. |
| Entity name limit | 1024 bytes. Longer names return 500 with `key too large` in the controller log. |

Minimum permissions for Phase 1:

```text
config  service  service-policy  service-edge-router-policy
config-type.read  router.read  identity.read  edge-router-policy.read  terminator.read
```

Phase 2 adds `edge-router-policy`. Phase 3 adds `identity`, `enrollment`, and `auth-policy.read`.

## Consequences

- Tag keys change from `ziti.alialjaffer.com/*` to `ziti-operator-*` (PLAN.md section 6.1).
- `zitiName` has a CEL `maxLength` of 1000.
- The chart documents the permission list above instead of an admin credential.
