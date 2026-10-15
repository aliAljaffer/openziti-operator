# Validation

The operator rejects a bad spec twice: at admission, when a webhook is installed, and during
reconcile, when it is not. Both use the same offline checks, so the two cannot disagree.

## What the webhook checks

The webhook validates `ZitiApp`, `ZitiIdentity`, `ZitiAccessPolicy`, and `ZitiConnection` on create and
update. It never calls the Ziti controller, so admission works while Ziti is down.

- Every kind: `zitiName` has no `"` or `\`.
- `ZitiApp`: the target grouping errors (a port served by no target, a port served by two target
  groups, a service name over 1024), the expose and target shape, and `hostedBy` when set must name a
  router in the referenced `ZitiConnection.spec.hostingRouters`.
- `ZitiAccessPolicy`: the role checks, including `#all` and `@id` roles under a `Namespaced` scope.
- `ZitiIdentity`: the role attributes and `externalId`, which needs `roleScope: Global`.
- `ZitiConnection`: `spec.allowedNamespaces` parses as a label selector.

When the referenced `ZitiConnection` does not exist yet, the checks that need it are skipped and the
webhook adds a warning. Resources are often applied together, so install order must not fail admission.

Checks that need the network stay in the reconciler: a name that clashes in Ziti, a target that does
not exist, a missing config type or auth policy, an invalid certificate, an offline router. Those keep
their `InvalidSpec`, `TargetNotFound`, `ZitiRejected`, and `NameConflict` reasons and conditions.

## Enabling it

The webhook is off by default, so an existing install is unchanged. In the Helm chart:

```sh
helm upgrade --install ziti-operator oci://ghcr.io/alialjaffer/charts/ziti-operator \
  --set webhook.enabled=true
```

The chart generates a self-signed serving certificate, mounts it on the manager, and writes the CA
into the `ValidatingWebhookConfiguration` `caBundle`. The certificate is regenerated on upgrade unless
the existing Secret is present, and the webhook stays off unless you ask for it.

With `--enable-webhook` on the manager you must also pass `--webhook-cert-path` pointing at a mounted
serving certificate.

## Failure policy

The webhook ships with `failurePolicy: Ignore`. A webhook outage therefore lets edits reach the
reconciler, which still reports the same `InvalidSpec` condition and reason. Nothing is written to
Ziti for a rejected spec either way. Move to `Fail` only after the install has proven stable, because
`Fail` blocks every edit of the covered kinds while the webhook is down.
