---
name: operate-and-audit
description: Use when you monitor the operator, read its metrics and conditions, run the audit command against a Ziti network, handle orphaned Ziti entities, check certificate expiry, or uninstall the operator.
---

# Operate and audit

Details: `docs/operations.md`, `docs/audit.md`, `docs/conditions.md`, `docs/uninstall.md`.

## Health

- Two replicas, one leader. Failover takes about 20 seconds.
- Ziti unreachable: reconcile fails with the error in status and retries with backoff.
- Ziti rejects a spec (4xx): `Synced=False`, retry in about 10 minutes.
- Entity deleted by hand: restored at the next resync (10 minutes).
- Conditions and reasons per kind: `docs/conditions.md`.

## Metrics

Enable with `metrics.enabled`. `prometheus.enabled=true` ships a ServiceMonitor and alert rules. `grafanaDashboard.enabled=true` ships a dashboard.

Key series: `ziti_operator_resource_conditions`, `ziti_operator_identity_certificate_expires_seconds`, `ziti_operator_app_service_terminators`, `ziti_operator_orphaned_entities`, `ziti_operator_sweep_errors_total`, `ziti_operator_api_requests_total`.

## Orphan sweeper

Finds Ziti entities that carry this cluster's tag but whose owner UID is gone. `operator.orphanPolicy`: `report` (default) logs and sets metrics. `delete` removes them. Adopted identities are released, not deleted. Entities without the operator tags are never touched.

## Audit a whole network (read only)

```sh
ZITI_USERNAME=<u> ZITI_PASSWORD=<p> go run ./cmd/audit --url https://<controller>/edge/management/v1 [--ca-file ca.pem] [-o table|json]
```

Codes: `NoTerminator`, `NoBind`, `InertBind`, `NoDialer`, `NoCommonRouter`, `OfflinePath`, `ProtocolMismatch`, `MissingConfig`, `UnusedConfig`, `EmptyRoles`, `ExpiredEnrollment`. Most also appear in `status` of the owning resource. Use the command for entities no resource owns.

## Certificates

`OperatorEnrolled` identities renew automatically. `JwtOnly` identities cannot. The operator sets `CertificateValid=False` and sends a Warning event 30 days before expiry.

## Uninstall

Follow `docs/uninstall.md`. Delete the custom resources first so finalizers remove the Ziti entities. `crd.keep=true` keeps CRDs.
