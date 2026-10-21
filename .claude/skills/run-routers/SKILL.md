---
name: run-routers
description: Use when you create a ZitiRouter, get its enrollment JWT, start the router on a VM with Docker Compose or in a cluster, set advertisedAddress, or fix a router that is not enrolled, not online, or not serving.
---

# Edge routers

Details: `docs/routers.md`, `examples/start-an-edge-router`. `ZitiRouter` (`ztrouter`) is cluster-scoped. Cluster admins only.

## Pick a run mode

| Mode | Set | Result |
|---|---|---|
| VM | `enrollmentSecretRef` | Secret gets `enrollment.jwt` and `docker-compose.yml`. Run `docker compose up -d`. |
| Another cluster | `enrollmentSecretRef`, `portable.serviceType` | Apply Secret key `router.yaml` there. Apply within about 3 hours of the JWT. |
| This cluster | `deployment.namespace` | The operator makes Deployment, volume claim, and Service, all owned by the `ZitiRouter`. |

Fetch a key: `kubectl -n <ns> get secret <name> -o jsonpath='{.data.<key with \. >}' | base64 -d`.

## Rules

- `advertisedAddress` is needed only when something dials the router by name (direct clients, router links, `NodePort`). Empty becomes `CHANGE_ME.invalid`. It is fixed at first start. To change it, delete the router in Ziti, delete the volume, and apply again.
- `tunnelerEnabled: true` is required for a router that hosts a `ZitiApp`.
- Add the router `zitiName` to `ZitiConnection.hostingRouters` before a `ZitiApp` uses it.
- The JWT lasts about 3 hours. The operator renews an unused one and removes it after enrollment.
- In-cluster: one replica, `Recreate`, uid 2171. If the volume is deleted, delete and re-create the resource. `storageClassName` cannot change after the claim exists (`StorageClassLocked`).
- Service write access: chart `rbac.serviceNamespaces`. A foreign Service gives `NameConflict` on `Workload`. Otherwise `ServiceNamespaceNotAllowed`. Apply `service.yaml` from the Secret.
- `deletionPolicy: Orphan` keeps the router in Ziti when the resource is deleted. The default `Delete` removes it.

## Check

`kubectl get ztrouter -o wide`: `ENROLLED`, `ONLINE`. Condition `Serving` is `True` when every service that picked this router has a terminator on it. Failures: skill `troubleshoot-resources`.
