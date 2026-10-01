---
name: new-ziti-app
description: Use when you must add a new ZitiApp, for example a service that copies an existing hand-made Ziti service with a new name and address. Gives the order of checks, the field mapping from the Ziti entities, a template, and the checks after apply.
---

# Add a new ZitiApp

A `ZitiApp` creates the service, the intercept and host configs, the bind policy, the dial policy, and the router policy. It never touches entities it did not create.

## 1. Check the connection

```sh
kubectl get ztconn
```

`CONNECTED` must be `True`. If the new app must join groups that hand-made policies already select (for example `#team`), the connection needs `roleScope: Global`. With `Namespaced`, the group becomes `<namespace>.team` and the old policies do not match.

`hostedBy` must be in the connection `hostingRouters`. It defaults to the first entry.

## 2. Inspect the service to copy (read only)

Skip this step for a brand new service. Reach the management API with `kubectl port-forward`, log in, and use GET calls only.

Read these for the service to copy:

| Ziti object | Field | Goes to |
|---|---|---|
| service | `roleAttributes` | `memberOf` |
| `intercept.v1` config | `addresses`, `portRanges`, `protocols` | `expose.addresses`, `expose.ports`, `expose.protocols` |
| `host.v1` or `host.v2` config | `address`, `port` | `targets[].address`, `targets[].port` |
| Dial policy | `identityRoles` (`#group`) | `allow.groups` |
| Dial policy | `identityRoles` (`@id`) | `allow.identities` (use identity names) |
| Bind policy | `@<router id>` | `hostedBy` (router name) |
| edge router policy for the group | already exists | nothing, unless the app needs its own `entryRouters` |

List the identities that the Dial roles select. They are the people with access. Check that the old policies still select the new service through `memberOf`.

If the original intercepts `udp` but the host config allows only `tcp`, use `tcp` only. The extra `udp` is captured and then refused.

## 3. Write the app

```yaml
apiVersion: ziti.alialjaffer.com/v1alpha1
kind: ZitiApp
metadata: {name: <short-name>, namespace: <apps-namespace>}
spec:
  zitiName: <service name in Ziti>
  expose:
    addresses: [<dns name clients dial>]
    ports: [443]
    protocols: [tcp]
  targets:
    - address: <ip or host reachable from the hosting router>
      port: 443
  memberOf: [<group>]
  allow:
    groups: [<group>, <zitiName>]
```

The target must be reachable from the hosting router pod, not from the operator.

## 4. Apply

1. Run `kubectl apply --dry-run=server -f app.yaml`.
2. For a name that may already exist in Ziti, apply a copy with `managementPolicy: Observe` first. It reads the service and writes nothing.
3. Ask the owner before you write to a live network.
4. Apply the app.

## 5. Verify

```sh
kubectl -n <ns> get ztapp
```

`HOSTED`, `DIALABLE`, `ROUTEPATH`, and `READY` must be `True`. If not, use the `troubleshoot-resources` skill.

Compare the new service with the original through the management API: role attributes, configs, policies, router policy, and one terminator on the hosting router.

## Pitfalls

- A `ZitiApp` whose `zitiName` exists and is not owned gives `NameConflict`. Apps cannot be adopted.
- Check the spelling of names you copy. A typo in the source name finds nothing.
- Namespaces with `allowedNamespaces` on the connection must carry the matching label.
- Keep internal names, hosts, and IDs out of samples and docs in this repository.
