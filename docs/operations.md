# Operations

## High availability

The Deployment runs two replicas with leader election. One replica works. The other waits. After the leader stops, the other takes over in about 20 seconds (measured with a killed process).

## Metrics

The manager serves Prometheus metrics. Enable them with `--metrics-bind-address`.

| Metric | Meaning |
|---|---|
| `ziti_operator_api_requests_total{method,kind,code}` | Calls to the Ziti API. `code` is the HTTP status, or `error`. |
| `ziti_operator_resource_conditions{kind,type,status}` | Resources by condition. |
| `ziti_operator_managed_entities{kind}` | Ziti entities tagged for this cluster. Set by the sweeper. |
| `ziti_operator_orphaned_entities{kind}` | Tagged entities whose owner is gone. |
| `ziti_operator_sweep_errors_total` | Failed sweeps. |
| `controller_runtime_reconcile_time_seconds`, `controller_runtime_reconcile_errors_total` | Reconcile duration and errors, from controller-runtime. |

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
