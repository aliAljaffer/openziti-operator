# Groups

Ziti has no group object. A group is a role attribute shared by identities, services, and routers. A
`ZitiGroup` is a Kubernetes-only helper that keeps one role attribute on a set of `ZitiIdentity`
resources in its namespace. It owns no Ziti entity.

```yaml
apiVersion: ziti.alialjaffer.com/v1alpha1
kind: ZitiGroup
metadata:
  name: platform
  namespace: team-a
spec:
  members:
    - alice
  selector:
    matchLabels:
      team: platform
```

The default role attribute is `group.<name>`, here `group.platform`. With the default
`roleScope: Namespaced` on the connection, policies use `#group.platform` and the operator rewrites
it to `#team-a.group.platform`. Set `spec.roleAttribute` to choose another value.

Use the group from an app or a policy:

```yaml
spec:
  allow:
    groups:
      - group.platform
```

```yaml
spec:
  identityRoles:
    - "#group.platform"
```

Members are `ZitiIdentity` names in the same namespace. The selector matches labels on those
identities. The identity controller is the only writer: it merges the group role attribute into the
identity entity. The group controller only reports `status.members` and `Ready`.

A group cannot change identities in another namespace, and it does not write an identity in
`managementPolicy: Observe`. An explicit member name that does not exist is reported with
`Ready=False` and reason `MemberNotFound`; the member is not created.

Deleting a group removes its role attribute from its members at the next reconcile, unless the
attribute is also listed in the identity's own `spec.roleAttributes`.

## Limits

- Group membership is namespace-local. Cross-namespace groups would need a cluster-scoped kind and a
  deliberate role-scope decision.
- The default `group.` prefix is reserved by the operator. An explicit `roleAttribute` without that
  prefix is not removed automatically if the group is deleted.
