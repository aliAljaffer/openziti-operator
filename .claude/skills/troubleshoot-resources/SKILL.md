---
name: troubleshoot-resources
description: Use when a ZitiApp, ZitiIdentity, ZitiAccessPolicy, or ZitiConnection is not Ready, stuck deleting, or a client cannot reach an app. Gives the order of checks and the fix for each reason.
---

# Find out why a resource is not working

The full reason list is in `docs/conditions.md`. Work in this order.

1. Read the conditions.

   ```sh
   kubectl get ztapp <name> -n <ns> -o jsonpath='{range .status.conditions[*]}{.type}={.status} {.reason}: {.message}{"\n"}{end}'
   kubectl describe ztapp <name> -n <ns>
   ```

2. Check the connection: `kubectl get ztconn`. `CONNECTED` must be `True`. If not, check the credential Secret, the CA ConfigMap, and that the URL is reachable from the operator pod.

3. Match the reason.

| Reason | Fix |
|---|---|
| `InvalidSpec` | Read the message. Nothing was created. Fix the value. |
| `NameConflict` | A Ziti entity with the name exists and is not owned. Set `zitiName`, or use `Adopt` (identities) or `Observe`. |
| `NamespaceNotAllowed` | Label the namespace to match the connection's `allowedNamespaces`. |
| `SecretNamespaceNotAllowed` | Add the namespace to `rbac.secretNamespaces`. |
| `IdentityNotFound` | Create the identity in Ziti or fix the name in `allow.identities`. |
| `NoDialer` | Add `allow.groups` or `allow.identities`. |
| `NoCommonRouter` | Set `entryRouters` to a router the clients may use. |
| `NoTerminator` | The hosting router cannot reach the target. Check `targets[].address` from the router pod. |
| `IdentityFileLost` | Wait. The operator re-enrolls. The Ziti ID changes. |

4. A client cannot connect but everything is `Ready`: run the read-only audit and check the client identity's role attributes.

   ```sh
   ZITI_USERNAME=... ZITI_PASSWORD=... go run ./cmd/audit --url https://<controller>/edge/management/v1
   ```

5. Stuck deleting: the operator needs the `ZitiConnection` to finish. Recreate the connection. If it is gone for good, patch the finalizer off: `kubectl patch <res> --type merge -p '{"metadata":{"finalizers":null}}'`. The Ziti entities then stay. The orphan sweeper reports them.

6. Operator logs: `kubectl logs -n ziti-operator-system deploy/ziti-operator-controller-manager`. Only the leader reconciles. The standby logs little.

## Metrics to look at

`ziti_operator_api_requests_total` by code, `ziti_operator_resource_conditions`, `ziti_operator_orphaned_entities`, `controller_runtime_reconcile_errors_total`.
