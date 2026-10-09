# Changelog

## v0.2.0

Adds in-cluster router deployment, a sidecar tunneler, Ingress support, session visibility, port forwarding, and Service selectors. Nothing in `v0.1.2` changes shape: `spec.deployment` and `deployment.serviceType` are new fields, so there is nothing to migrate.

### Notes

**Go and image base moved to 1.27.2.** The build image, `go.mod`, and the linter pin Go 1.27.2 and `golang.org/x/net` 0.60.0 to clear the govulncheck findings in the earlier toolchain. Rebuild custom images from the new tag if you pinned the old digest.

**`ZitiApp.spec.expose.targets` is optional.** It is required only when you do not set `spec.expose.selector`. A `ZitiApp` that sets both `selector` and `targets` is rejected, because the selector already derives the targets.

**`deployment.serviceType` defaults to `ClusterIP`.** A `ClusterIP` is reachable only from inside the cluster. Set `NodePort` with an `advertisedAddress`, or `LoadBalancer`, for clients elsewhere.

### Added

- `ZitiRouter.spec.deployment` runs a router in the cluster on amd64 or arm64 with a `Deployment`, a `PersistentVolumeClaim`, and a `Service`, all owned by the resource, plus an optional JWT from a Secret. It reports `Workload` and `StorageClassLocked`. The JWT re-enrollment race between an issued JWT and `isVerified` is fixed.
- `ZitiRouter.spec.advertisedAddress` is optional. An empty value uses `CHANGE_ME.invalid`.
- `ZitiSidecar` writes the tunneler sidecar patch for a workload into a Secret. It does not mutate workloads and does not run a webhook.
- A `ZitiApp` is created from an Ingress annotated with `ziti.alialjaffer.com/expose: "true"`. One HTTP Service backend is supported; TLS and mixed backends are rejected.
- `ZitiIdentity.status` reports sessions from `GET /sessions`: `activeSessions`, `connectedServices`, `connectedRouters`, and `sessionsObserved`. No tokens are reported.
- `ZitiPortForward` prepares a single-replica proxy `Deployment` for local service debugging. You still run `kubectl port-forward` against it.
- `ZitiApp.spec.expose.selector` derives addresses, ports, protocols, and targets from the selected Services. Explicit `addresses` and `ports` still override the derived values.
- `ZitiRouter` reports `Serving` and `ZitiConfig` reports `InUse` findings.

### Security

- The operator role can write Services cluster-wide, because it creates the Service for an in-cluster router. That is wider than `v0.1.2`, which only read them, so an admission policy may flag the upgrade. Set `rbac.serviceNamespaces` to move the write access into per-namespace Roles. A router outside the list still gets its Deployment and claim and runs, reports `ServiceNamespaceNotAllowed`, and gets the Service manifest written to its enrollment Secret for you to apply. The operator does not take over a Service you applied yourself, so `Workload` then reads `NameConflict` and stays false until you delete it. Reading Services stays cluster-wide.
- `Deployment` and `PersistentVolumeClaim` write access stays cluster-wide. Scoping Services alone limits the blast radius only partly: creating a Deployment in any namespace already lets the operator run a pod there.
- The trivy scan flags `AVD-KSV-0056` on the Service rule. It is listed in `.trivyignore` on purpose, for the reason above.
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