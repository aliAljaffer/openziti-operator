---
name: access-and-scope
description: Use when you grant or restrict access with ZitiAccessPolicy, choose Namespaced or Global role scope, isolate teams on one controller, or manage single Ziti objects with ZitiConfig, ZitiService, ZitiServicePolicy, ZitiEdgeRouterPolicy, ZitiServiceEdgeRouterPolicy, or ZitiTerminator.
---

# Access, role scope, and full control

Details: `docs/role-scope.md`, `docs/full-control.md`, `examples/group-based-access`, `examples/isolate-teams-on-one-controller`, `examples/full-control`.

## Role scope (set on `ZitiConnection`)

| | `Namespaced` (default) | `Global` |
|---|---|---|
| `#team-a` | becomes `#<namespace>.team-a` | unchanged |
| `#all`, `@name` | rejected | allowed |
| Use | many teams | one platform team |

`allow.identities` takes Ziti identity names from the whole network and ignores role scope. `roleScope` can be set by cluster admins only (`ZitiConnection` is cluster-scoped). Hand-made policies that select `#group` will not match `Namespaced` groups. Use `Global` or add the scoped attribute (`team-a.staff`).

## ZitiAccessPolicy

Grants `identityRoles` Dial access to `serviceRoles`, optionally through `edgeRouters`. Roles are `#attribute`. Identities get attributes from `ZitiIdentity.spec.roleAttributes`. Services get them from `ZitiApp.spec.memberOf`.

## Isolate teams

Use `Namespaced`, one namespace per team, and `allowedNamespaces` on the connection. Check that identities in one namespace cannot dial the other.

## One kind per Ziti object

Use only when you know OpenZiti and `ZitiApp` is too coarse. Kinds: `ZitiConfig` (`ztcfg`), `ZitiService` (`ztsvc`), `ZitiServicePolicy` (`ztsp`), `ZitiEdgeRouterPolicy` (`zterp`), `ZitiServiceEdgeRouterPolicy` (`ztserp`), `ZitiTerminator` (`ztterm`).

- Order does not matter. A missing target shows `TargetNotFound` and retries after 30 seconds.
- `zitiName` (default `<namespace>.<name>`), `deletionPolicy`, and `type` are immutable.
- An existing Ziti name the operator does not own gives `NameConflict`. Nothing is written.
- Role forms: `#attr`, `#all`, `@name` (name resolved to ID on each sync).
