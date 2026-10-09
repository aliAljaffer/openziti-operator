# Changelog

## v0.2.0

Breaking release. It adds in-cluster router deployment, a sidecar tunneler, Ingress support, session visibility, port forwarding, and Service selectors, and it removes the operator's cluster-wide Service write permission.

### Breaking changes

**The operator no longer creates a Kubernetes Service for an in-cluster router.** `ZitiRouter.spec.deployment` used to create a `Service` alongside the `Deployment` and `PersistentVolumeClaim`. It no longer does. This keeps the RBAC role free of cluster-wide Service write access, which the security scan requires.

Migrate in three steps:

1. Upgrade the operator.
2. Apply the Service manifest that the operator writes to the router enrollment Secret.
3. Remove the Service from your own manifests, because the operator no longer manages it.

```bash
kubectl apply -f <router-enrollment-secret-mount>/service.yaml
```

Set `ZitiRouter.spec.deployment.serviceType` to the Service type you need (`ClusterIP`, `NodePort`, or `LoadBalancer`). The generated manifest carries the same ports the router listens on.

**Go and image base moved to 1.27.2.** The build image, `go.mod`, and the linter now pin Go 1.27.2 and `golang.org/x/net` 0.60.0 to clear the govulncheck findings in the earlier toolchain. Rebuild custom images from the new tag if you pinned the old digest.

**`ZitiApp.spec.expose.targets` is now optional.** It is required only when you do not set `spec.expose.selector`. A `ZitiApp` that sets both `selector` and `targets` is rejected, because the selector already derives the targets.

### Added

- `ZitiRouter.spec.deployment` runs a router in the cluster on amd64 or arm64 with a `PersistentVolumeClaim`, a generated Service manifest, an optional JWT from a Secret, and `Workload` and `StorageClassLocked` conditions. The JWT re-enrollment race between an issued JWT and `isVerified` is fixed.
- `ZitiRouter.spec.advertisedAddress` is optional. An empty value uses `CHANGE_ME.invalid`.
- `ZitiSidecar` writes the tunneler sidecar patch for a workload into a Secret. It does not mutate workloads and does not run a webhook.
- A `ZitiApp` is created from an Ingress annotated with `ziti.alialjaffer.com/expose: "true"`. One HTTP Service backend is supported; TLS and mixed backends are rejected.
- `ZitiIdentity.status` reports sessions from `GET /sessions`: `activeSessions`, `connectedServices`, `connectedRouters`, and `sessionsObserved`. No tokens are reported.
- `ZitiPortForward` prepares a single-replica proxy `Deployment` for local service debugging. You still run `kubectl port-forward` against it.
- `ZitiApp.spec.expose.selector` derives addresses, ports, protocols, and targets from the selected Services. Explicit `addresses` and `ports` still override the derived values.
- `ZitiRouter` reports `Serving` and `ZitiConfig` reports `InUse` findings.

### Security

- The operator role no longer requests cluster-wide `Service` write access. Generated resource-level RBAC still honours `rbac.secretNamespaces` and `clusterWideSecrets`.
- Govulncheck is clean for the released code.
- The sidecar, Ingress, and port-forward docs now publish to the wiki.

### Known limitations

- `ZitiGroup` is not implemented. Group membership would compete with `ZitiIdentity` for ownership of role attributes.
- The `ziti` CLI image is not pinned by digest in the chart.
- No `ZitiSite` support. The Ziti controller API has no stable endpoint for it.

### Upgrade

```bash
helm upgrade ziti-operator oci://ghcr.io/alialjaffer/charts/ziti-operator --version 0.2.0 -n ziti-operator-system
```

The chart defaults its image tag to the chart app version, so no image override is needed.

See [Installation](docs/install.md) for a fresh install and [Compatibility](docs/compatibility.md) for tested versions.