# Full control: one resource per Ziti object

`ZitiApp` publishes an app and creates everything it needs. Use the one-to-one kinds when you know OpenZiti and want to manage each object yourself.

| Kind | Short name | Ziti object |
|---|---|---|
| `ZitiConfig` | `ztcfg` | config |
| `ZitiService` | `ztsvc` | service |
| `ZitiServicePolicy` | `ztsp` | service policy (Dial or Bind) |
| `ZitiEdgeRouterPolicy` | `zterp` | edge router policy |
| `ZitiServiceEdgeRouterPolicy` | `ztserp` | service edge router policy |
| `ZitiTerminator` | `ztterm` | terminator |

The keys are the Ziti keys, in camelCase. There are no derived objects and no defaults beyond Ziti's own. Every kind is namespaced.

## Names

The name in Ziti is `zitiName`, or `<namespace>.<name>` without it. `zitiName`, `deletionPolicy`, and the `type` of a config or a service policy cannot change after creation. If an object with that name already exists in Ziti and this operator does not own it, the resource shows `NameConflict` and nothing is written.

## Roles

Role fields (`identityRoles`, `serviceRoles`, `edgeRouterRoles`, `postureCheckRoles`) take three forms:

| Form | Meaning | Namespaced scope | Global scope |
|---|---|---|---|
| `#attribute` | every entity with that role attribute | becomes `#<namespace>.attribute` | passes through |
| `#all` | every entity | rejected | allowed |
| `@name` | one entity, by name | rejected | the operator looks up the ID |

Ziti wants IDs in `@` roles. You write the name and the operator resolves it on every sync, so a recreated identity keeps working. See [role scope](role-scope.md).

`roleAttributes` on a `ZitiService` follow the same scope. `configs` on a `ZitiService` are names of Ziti configs, for example the `zitiName` of a `ZitiConfig`.

## Order does not matter

Apply the resources in any order. A resource that names an object that does not exist yet shows `Ready=False` with reason `TargetNotFound` and is checked again after 30 seconds. It becomes ready when the object appears.

## Example: an app with your own policies

```yaml
apiVersion: ziti.alialjaffer.com/v1alpha1
kind: ZitiConfig
metadata: {name: web-intercept, namespace: team-a}
spec:
  zitiName: web-intercept
  type: intercept.v1
  data:
    protocols: [tcp]
    addresses: [web.example.com]
    portRanges: [{low: 80, high: 80}]
---
apiVersion: ziti.alialjaffer.com/v1alpha1
kind: ZitiConfig
metadata: {name: web-host, namespace: team-a}
spec:
  zitiName: web-host
  type: host.v2
  data:
    terminators:
      - {address: 10.0.0.5, port: 8443, protocol: tcp}
---
apiVersion: ziti.alialjaffer.com/v1alpha1
kind: ZitiService
metadata: {name: web, namespace: team-a}
spec:
  zitiName: web.example.com
  configs: [web-intercept, web-host]
  roleAttributes: [web]
---
apiVersion: ziti.alialjaffer.com/v1alpha1
kind: ZitiServicePolicy
metadata: {name: web-bind, namespace: team-a}
spec:
  zitiName: web-bind
  type: Bind
  identityRoles: ["@router-a"]
  serviceRoles: ["@web.example.com"]
---
apiVersion: ziti.alialjaffer.com/v1alpha1
kind: ZitiServicePolicy
metadata: {name: web-dial, namespace: team-a}
spec:
  zitiName: web-dial
  type: Dial
  identityRoles: ["#staff"]
  serviceRoles: ["#web"]
---
apiVersion: ziti.alialjaffer.com/v1alpha1
kind: ZitiServiceEdgeRouterPolicy
metadata: {name: web-serp, namespace: team-a}
spec:
  zitiName: web-serp
  serviceRoles: ["#web"]
  edgeRouterRoles: ["@router-a"]
---
apiVersion: ziti.alialjaffer.com/v1alpha1
kind: ZitiEdgeRouterPolicy
metadata: {name: web-erp, namespace: team-a}
spec:
  zitiName: web-erp
  identityRoles: ["#staff"]
  edgeRouterRoles: ["@router-a"]
```

The `@router-a` role selects the identity of the router that hosts the service. A router in tunnel mode has an identity with the same name as the router. The bind policy and the service edge router policy must both include the hosting router, or the service gets no terminator.

Everything with `@` roles needs `roleScope: Global` on the connection. With `Namespaced` scope, use `#attribute` roles.

## Terminators

Most services need no terminator resource. The hosting router creates the terminators from the host config, one for each entry in `terminators` of a `host.v2` config.

Use a `ZitiTerminator` for a fixed route: a router that forwards a service to one address.

```yaml
apiVersion: ziti.alialjaffer.com/v1alpha1
kind: ZitiTerminator
metadata: {name: web-via-router-a, namespace: team-a}
spec:
  service: web.example.com
  router: router-a
  address: tcp:10.0.0.5:8443
  cost: 10
```

- `service` and `router` are Ziti names of existing objects. The connection needs `roleScope: Global`.
- A terminator has no name in Ziti. The operator finds it by its ownership tags. Two resources with the same values create two terminators, because Ziti allows duplicates.
- `address`, `cost`, and `precedence` change in place. `service`, `router`, and `binding` cannot change on the resource.
- There is no `Adopt` or `Observe`, because a terminator has no name to match.

## Mixing with ZitiApp

The kinds can live side by side. An entity is owned by one resource only. If a `ZitiApp` and a `ZitiService` want the same Ziti name, the second one shows `NameConflict`.

## Existing objects

Every kind has `managementPolicy`.

| Value | What happens |
|---|---|
| `Manage` (default) | The operator creates the object. An object with the same name that it does not own gives `NameConflict`. |
| `Adopt` | The operator takes over the existing object named `zitiName`. It updates only the fields the resource sets. An empty list counts as not set, so a hand-made value stays. Config `data` is replaced as a whole. Deleting the resource removes the operator tags and keeps the object. |
| `Observe` | The operator only reads the object and reports `Ready`. It never writes to Ziti. Give the resource the same `zitiName`. |

An adopted object cannot be cleared by leaving a field out. Change it in Ziti by hand or set the value.

## Limits

- Deleting a resource deletes the Ziti object, unless `deletionPolicy: Orphan`.
- Tested: the whole example above, applied in one go, created a service that a client reached through Ziti.
