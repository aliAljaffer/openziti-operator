# ADR 0003: Cluster-wide Service write access for in-cluster routers

Date: 2026-10-09. Status: built. See `docs/routers.md`.

## Question

`ZitiRouter.spec.deployment` creates a `Deployment`, a `PersistentVolumeClaim`, and a `Service`. The first two were always cluster-wide writes. Should the Service be too, or should the operator hand the Service to the user?

## Result: yes, with opt-in scoping

The operator creates the Service itself, and the chart may narrow where it may do that.

An earlier draft wrote a `service.yaml` manifest to the enrollment Secret and asked the user to apply it. That kept cluster-wide Service write out of the role, but it made a one-field resource need a manual step, and it was the whole of the release's breaking change. `spec.deployment` is new in v0.2.0, so no one was relying on the old behaviour and nothing had to be broken to remove it.

## Why the write verbs stay on by default

- The Service is what makes the router reachable. Without it the router has only a Pod address, which changes on every reschedule.
- The user asked for the operator to own the whole Ziti resource set rather than half of it.
- Istio, External Secrets, and Argo CD all ship cluster-wide access by default and let the administrator narrow it. This follows that pattern.

## Why scoping is opt-in rather than the default

A namespaced `Role` is still flagged by trivy `AVD-KSV-0056`, so scoping does not satisfy the scanner. It only limits blast radius, and it costs a second values key plus a per-namespace template. Making that the default would take the capability away from the common case and still leave the finding.

`rbac.serviceNamespaces` is therefore empty by default. When set, Service write moves to per-namespace Roles and the ClusterRole keeps read access, because the router controller, the Service controller, the Ingress controller, and the `ZitiApp` selector all list and watch Services in every namespace. A router outside the list still gets its Deployment and claim, and its `Workload` condition reports `ServiceNamespaceNotAllowed`, because a router without a stable address is still a working router. A Service applied by hand is left alone and reports `NameConflict`: the operator never adopts one it did not create, and the router keeps running.

## What this does not fix

Scoping Services is partial. The operator can already create a `Deployment` in any namespace, and creating a workload in a namespace implicitly allows running a pod under any ServiceAccount there, which reaches every Secret mounted by that pod. That is a larger escalation than writing a Service, and `hack/sync-chart.sh` does not scope `deployments` or `persistentvolumeclaims` today.

Closing that means scoping all three kinds, which is a separate change with its own migration for anyone running routers in several namespaces.

## Consequences

- `.trivyignore` lists `AVD-KSV-0056`, deliberately, for the reason above.
- `hack/sync-chart.sh` maps each resource to its own guard expression, because controller-gen folds `services` into the same rule as `persistentvolumeclaims` and `secrets`.
- `hack/test-chart.sh` renders a scoped install and asserts the ClusterRole can still watch Services, so the read path cannot regress.
