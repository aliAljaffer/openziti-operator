# Changelog

## Unreleased

Additive, backward-compatible features. Existing fields keep their names, types, and defaults.

### Added

- `ZitiGroup` groups identities in one namespace under one role attribute. It owns no Ziti entity; the identity controller applies the attribute.
- `ZitiAuthPolicy` maps one to one to a Ziti auth policy, including certificate, UPDB, external JWT, TOTP, and lockout settings. `ZitiIdentity` resolves a policy in its namespace by resource name.
- `ZitiIdentity.spec.disabled` and `disabledUntil` disable an identity without deleting it. `importFrom` adopts an identity that enrolled elsewhere by copying an existing identity file into an operator-owned Secret.
- `ZitiPostureCheck` maps one to one to a Ziti posture check. `ZitiApp.spec.allow.postureCheckRoles` attaches posture to the simple Dial policy.
- `ZitiApp.spec.hostedByAnyOf` lets several routers host the same targets. `service`, `additionalConfigs`, `targets[].precedence`, and `targets[].namespace` cover common advanced cases.
- `ZitiRouter.spec.managementPolicy` Observe, plus deployment resources, scheduling, image pull secrets, pod annotations, labels, and extra environment variables.
- `ZitiCA` and `ZitiJwtSigner` Observe modes, and `ZitiJwtSigner.spec.issuer` with `keys.certPEM` for static signers.
- `manager export` writes the control-plane configuration as applyable CRs with `managementPolicy: Observe` by default.
- `manager access` prints the dialers, binders, routers, and posture roles of every service.

### Changed

- TLS Ingresses can be published through an ingress controller with `ziti.alialjaffer.com/via-ingress=true` and `ziti.alialjaffer.com/ingress-backend=<namespace>/<service>:<port>`. Existing HTTP-only behavior is unchanged.
- A cross-namespace `ZitiApp` target needs `ziti.alialjaffer.com/allow-expose=true` on the target Service.
- The existing-resources docs now say that a `ZitiApp` can be adopted, matching the controller.

See [ADR 0004](docs/adr/0004-backward-compatible-roadmap.md) for the breaking cleanups that are deliberately deferred.

## v0.3.2

A quality-of-life release. No API fields change shape and existing resources keep working. Manager
logs now default to JSON, and the chart can set the level and encoder.

### Added

- `manager audit`: the audit command ships in the manager image and can read a `ZitiConnection` from the cluster with `--connection`. `--fail-on any|CODE,...` makes it usable as a CI check.
- `--version` and the `ziti_operator_build_info{version,commit,go}` metric.
- A readiness check for the Ziti connection at `/readyz/ziti`. A fresh install with no `ZitiConnection` is ready.
- Configurable resync and retry intervals through manager flags and `operator.intervals`.
- Remediation hints at the end of failure condition messages, matching the tables in [Conditions and reasons](docs/conditions.md).
- `make check` runs lint, tests, the docs and YAML checks, and the chart checks.
- Descriptions on every CRD status field, so `kubectl explain` covers them.

### Changed

- Manager logs default to production JSON. Set `manager.logFormat=console` or `--zap-encoder=console` for console output; `manager.logLevel` sets the level.
- Chart notes now show the connection check, metrics, webhook state, and uninstall behavior.
- The sample manifests are valid and apply as a set.

### Fixed

- Reconcile failures produce Warning Events, so `kubectl describe` shows the cause.

### Dependencies

- `github.com/go-jose/go-jose/v4` 4.1.6 for CVE-2026-116587.
- `docker/login-action` 4.7.0.

## v0.3.1

Fixes `ZitiSidecar` `Host` mode, which did not intercept traffic. Existing `ZitiSidecar` resources keep working. Re-apply the patch to get the fix.

### Fixed

- `ZitiSidecar` in `Host` mode now runs `ziti tunnel tproxy`. It ran `ziti tunnel host`, which does not intercept, so the workload could not dial Ziti services by name. The patch runs as non-root with `drop: [ALL]` plus `NET_ADMIN` and `NET_RAW`, sets `dnsPolicy: None` and `dnsConfig` with the tunneler (`127.0.0.1`) as the pod resolver, and sets the `net.ipv4.ip_unprivileged_port_start` sysctl. It no longer suggests `hostNetwork`.

### Added

- `ZitiSidecar.spec.image` sets the tunneler image, for a mirror in an air-gapped cluster.
- `ghcr.io/alialjaffer/ziti-tunneler` is `ziti-router` with file capabilities on `ziti` and `iptables`. It is the `Host` mode default at the controller version, so `tproxy` runs as non-root. `Proxy` mode still uses `openziti/ziti-router` at the controller version. Build a mirror from `images/tunneler/Dockerfile`.

### Changed

- `ZitiSidecar.spec.resolver` is now passed as `--dnsUpstream`, and the tunneler listens on `127.0.0.1:53`. It has no default. Set it to the cluster DNS IP, for example `udp://10.96.0.10:53`. When it is empty, the patch omits `--dnsUpstream` and cluster Service names do not resolve in the pod.
- Re-applying a `Host` patch restarts the workload and changes its pod DNS. Remove `hostNetwork: true` if you set it from the old docs.

## v0.3.0

Adds a validating admission webhook, Prometheus alerts and an optional Grafana dashboard, conditions and metrics for every kind, and a portable router bundle. Nothing in `v0.2.0` changes shape: the webhook is off by default, the new `ZitiRouter` fields are optional, and no existing field changes meaning.

### Notes

**The webhook is off by default.** The chart ships `webhook.enabled=false`, so an existing install is unchanged. Set `webhook.enabled=true` to validate `ZitiApp`, `ZitiIdentity`, `ZitiAccessPolicy`, and `ZitiConnection` at admission with the same offline checks the reconciler runs.

**The webhook ships with `failurePolicy: Ignore`.** A webhook outage, or a call that never reaches the operator, falls through to the reconciler, which still reports `InvalidSpec`. Move to `Fail` only after the install is proven, because `Fail` blocks every edit of the covered kinds while the webhook is down.

**The router bundle key is now `router.yaml`.** `ZitiRouter` without `spec.deployment` writes a self-contained bundle to `enrollmentSecretRef` under the key `router.yaml`, replacing `deployment.yaml`. The bundle is portable: the enrollment JWT carries the controller address, and the router image fetches and pins the controller certificate, so no CA bundle or `ZitiConnection` data travel with it. Set `spec.portable.serviceType` to choose the Service type in the bundle.

### Added

- An optional validating admission webhook for the everyday kinds. The chart generates a self-signed serving certificate, mounts it on the manager, and writes the CA into the `ValidatingWebhookConfiguration` `caBundle`. It never calls Ziti, so admission works while the controller is down.
- `ZitiRouter.spec.portable.serviceType` chooses the Service type (`ClusterIP`, `NodePort`, or `LoadBalancer`) in the portable `router.yaml` bundle. The default is `LoadBalancer`.
- A `PrometheusRule` with alerts for resource conditions, identity certificate expiry, orphaned entities, and Ziti API errors, shipped when `prometheus.enabled=true` (set `prometheus.rules.enabled=false` to keep only the ServiceMonitor).
- An optional Grafana dashboard, installed as a ConfigMap for the kube-prometheus-stack sidecar with `grafanaDashboard.enabled=true`.
- `ziti_operator_resource_conditions` now reports all fifteen kinds, up from four, plus `ziti_operator_identity_certificate_expires_seconds` and `ziti_operator_app_service_terminators`. All are computed at scrape time, so no series goes stale.

### Upgrade

```bash
helm upgrade ziti-operator oci://ghcr.io/alialjaffer/charts/ziti-operator --version 0.3.0 -n ziti-operator-system
```

The chart defaults its image tag to the chart app version, so no image override is needed. The webhook stays off until you set `webhook.enabled=true`.

See [Installation](docs/install.md) for a fresh install, [Validation](docs/validation.md) for the webhook, and [Compatibility](docs/compatibility.md) for tested versions.

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