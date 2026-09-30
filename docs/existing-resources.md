# Existing Ziti resources

The operator ignores every Ziti entity it did not create. To manage or watch an existing one, set `managementPolicy`.

| Policy | `ZitiIdentity` | `ZitiApp` | What happens |
|---|---|---|---|
| `Manage` (default) | yes | yes | The operator creates the entity and keeps it in sync. |
| `Adopt` | yes | no | The operator takes over the identity named `zitiName`. |
| `Observe` | yes | yes | The operator reads the entity and reports status. It never writes to Ziti. |

## Observe

Use it first. It shows the state of an existing service or identity with no risk.

```yaml
apiVersion: alialjaffer.com/v1alpha1
kind: ZitiApp
metadata: {name: legacy, namespace: team-a}
spec:
  zitiName: legacy.example.com
  managementPolicy: Observe
```

`expose` and `targets` are not needed. Status shows the IDs, the terminators, and the conditions.

## Adopt an identity

```yaml
apiVersion: alialjaffer.com/v1alpha1
kind: ZitiIdentity
metadata: {name: alice, namespace: team-a}
spec:
  zitiName: alice
  managementPolicy: Adopt
  roleAttributes: [staff]
```

The identity must exist and must not belong to another resource. The operator sets role attributes and the auth policy. It adds ownership tags next to your tags. It keeps fields such as `externalId`.

Deleting the resource never deletes an adopted identity. The operator removes only its own tags.

## Limits

- An app cannot be adopted. Changing an app from `Observe` to `Manage` gives `NameConflict`, because the service already exists.
