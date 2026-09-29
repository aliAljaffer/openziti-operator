# Role scope

Role scope decides how far a namespace can reach in Ziti. Set it on the `ZitiConnection`.

| | `Namespaced` (default) | `Global` |
|---|---|---|
| Groups | `team-a` becomes `<namespace>.team-a` | `team-a` stays `team-a` |
| `#all` and `@id` roles | Rejected | Allowed |
| Use it for | A cluster with many teams | A cluster run by one platform team |

## Effect

With `Namespaced`, namespace `a` cannot grant access to a service that namespace `b` labeled. The operator rewrites every group in `memberOf`, `allow.groups`, and access policy roles.

`ZitiConnection` is cluster-scoped. Only users with cluster-level RBAC can create one, and so only they can choose `Global`.

## Identities by name

`allow.identities` takes Ziti identity names from the whole network. It is not limited by role scope. An app owner can allow any identity to reach the owner's own app. The owner cannot reach other apps this way.

## Identities and groups

An identity gets its role attributes from `ZitiIdentity.spec.roleAttributes`. They follow the same scope. With `Namespaced`, an identity that the operator did not create needs the scoped attribute, for example `team-a.staff`.
