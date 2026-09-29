# Interop with ziti-k8s-agent

[ziti-k8s-agent](https://github.com/netfoundry/ziti-k8s-agent) injects a tunneler sidecar and manages one Ziti identity per pod. This operator does not do sidecar injection. The two tools meet at role attributes.

What the agent's documentation states:

- The annotation `identity.openziti.io/role-attributes` on a pod sets the role attributes of its identity.
- Without the annotation, the agent derives a default role from the pod's `app` label.
- Pods reach services through service policies that match their identity roles. A pod that binds a service needs a `host.v1` address config such as `127.0.0.1:443`.

## Let agent pods dial a ZitiApp

1. Choose a group name, for example `team-a`.
2. Add `allow.groups: [team-a]` to the `ZitiApp`.
3. Add the annotation `identity.openziti.io/role-attributes: <attribute>` to the pods.

The `<attribute>` depends on the connection `roleScope`:

| roleScope | Pod annotation value | Why |
|---|---|---|
| `Global` | `team-a` | `allow.groups` passes through as `#team-a`. |
| `Namespaced` | `<namespace>.team-a` | The operator rewrites `#team-a` to `#<namespace>.team-a`. |

Use `Global` when the agent's pods live in other namespaces than the apps. Otherwise annotate with the scoped name.

## Do not mix ownership

The agent creates and deletes its own identities. Do not create a `ZitiIdentity` with the same name. To watch an agent-made identity without owning it, use `managementPolicy: Observe` with `zitiName` set to its Ziti name.

## Not covered

The operator does not read agent pods. It cannot list them or report their state.
