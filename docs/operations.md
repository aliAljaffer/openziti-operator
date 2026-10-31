# Operations

## High availability

The Deployment runs two replicas with leader election. One replica works. The other waits. After the leader stops, the other takes over in about 20 seconds (measured with a killed process).

## Health and readiness

`/healthz` reports that the manager process is alive. `/readyz` also checks the Ziti connection:
the manager is not ready while every `ZitiConnection` reports `Connected=False`. A fresh install with
no `ZitiConnection` is ready, so the first connection can be created. Query the individual checks at
`/readyz` and `/readyz/ziti` on the probe port.

The reconcilers retry on their own schedule. `operator.intervals` and the matching manager flags
control the healthy resync and the faster retries. See [Install](install.md#manager-flags).

## Metrics

The manager serves Prometheus metrics. Enable them with `--metrics-bind-address`.

| Metric | Meaning |
|---|---|
| `ziti_operator_api_requests_total{method,kind,code}` | Calls to the Ziti API. `code` is the HTTP status, or `error`. |
| `ziti_operator_resource_conditions{kind,type,status}` | Resources by condition, for every kind. |
| `ziti_operator_identity_certificate_expires_seconds{namespace,name}` | Seconds until the earliest client certificate of a `ZitiIdentity` expires. Negative when already expired. |
| `ziti_operator_app_service_terminators{namespace,name,service}` | Terminators hosting each Ziti service of a `ZitiApp`. |
| `ziti_operator_managed_entities{kind}` | Ziti entities tagged for this cluster. Set by the sweeper. |
| `ziti_operator_orphaned_entities{kind}` | Tagged entities whose owner is gone. |
| `ziti_operator_sweep_errors_total` | Failed sweeps. |
| `ziti_operator_reconcile_total{kind}` | Reconcile loops per custom resource kind. |
| `ziti_operator_build_info{version,commit,go}` | Version, commit, and Go version of the running manager. The value is always 1. |
| `controller_runtime_reconcile_time_seconds`, `controller_runtime_reconcile_errors_total` | Reconcile duration and errors, from controller-runtime. |

The chart can ship a `PrometheusRule` and a Grafana dashboard for these metrics. Set
`prometheus.enabled=true` for the ServiceMonitor and the alert rules (set `prometheus.rules.enabled=false`
to keep only the ServiceMonitor), and `grafanaDashboard.enabled=true` to install the dashboard as a
ConfigMap for the kube-prometheus-stack sidecar. Both need prometheus-operator, or kube-prometheus-stack
with its dashboard sidecar.

## Import, export, and access review

`manager export` writes the whole control-plane configuration as CR manifests, with `Observe` as the
default. `manager access` prints who can dial or bind each service. Both commands are read-only and
accept `--connection` to read credentials from a `ZitiConnection`. See [Export](export.md) and
[Access report](access-report.md).

## Orphan sweeper
A crash or a removed finalizer can leave Ziti entities without an owner. Every `--orphan-sweep-interval` the leader looks for entities that carry this cluster's tag and an owner UID that no longer exists.

| `--orphan-policy` | Effect |
|---|---|
| `report` (default) | Logs each orphan and sets the metrics. |
| `delete` | Deletes orphans. Adopted identities are released, not deleted. |

Entities that other tools created are never touched, because they do not carry the operator's tags.

## Failure behavior

| Situation | Behavior |
|---|---|
| Ziti controller unreachable | Reconcile fails with the error in status, then retries with backoff. It recovers when the controller returns. |
| Ziti rejects the spec (4xx) | `Synced=False`, `Ready=False`, retry in about 10 minutes. |
| Operator killed mid-reconcile | The next reconcile finishes the work. It creates no duplicates. |
| Entity deleted by hand | Restored at the next resync (10 minutes). |

## Certificates of identities

`OperatorEnrolled` identities renew automatically. Renewal starts when the smaller of 30 days and a third of the lifetime is left. `JwtOnly` identities cannot renew, because the workload holds the key. The operator sets `CertificateValid=False` and sends a Warning event 30 days before expiry.

## Disable an identity

`ZitiIdentity.spec.disabled` calls the Ziti disable endpoint, which rejects new API session requests. Set `disabledUntil` for a timed lock. The operator reports a `Disabled` condition and does not delete the identity. See [Authentication policies](auth-policies.md).
