# Existing Ziti resources

The operator ignores every Ziti entity it did not create. To manage or watch an existing one, set `managementPolicy`.

| Policy | `ZitiIdentity` | `ZitiApp` | What happens |
|---|---|---|---|
| `Manage` (default) | yes | yes | The operator creates the entity and keeps it in sync. |
| `Adopt` | yes | yes | The operator takes over the entity named `zitiName`. |
| `Observe` | yes | yes | The operator reads the entity and reports status. It never writes to Ziti. |

## Observe

Use it first. It shows the state of an existing service or identity with no risk.

```yaml
apiVersion: ziti.alialjaffer.com/v1alpha1
kind: ZitiApp
metadata:
  name: legacy
  namespace: team-a
spec:
  zitiName: legacy.example.com
  managementPolicy: Observe
```

`expose` and `targets` are not needed. Status shows the IDs, the terminators, and the conditions.

## Adopt an identity

```yaml
apiVersion: ziti.alialjaffer.com/v1alpha1
kind: ZitiIdentity
metadata:
  name: alice
  namespace: team-a
spec:
  zitiName: alice
  managementPolicy: Adopt
  roleAttributes:
    - staff
```

The identity must exist and must not belong to another resource. The operator sets role attributes and the auth policy. It adds ownership tags next to your tags. It keeps fields such as `externalId`.

Deleting the resource never deletes an adopted identity. The operator removes only its own tags.

## Limits

- The operator never creates a Secret for an identity that is already enrolled, unless you give it the
  existing identity file with `importFrom`. Without that, `Observe` and `Adopt` keep the key where the
  identity enrolled. With `Adopt` and `OperatorEnrolled`, `Ready` is `False` with reason
  `IdentityFileUnavailable`, and the identity stays untouched.
- To adopt an enrolled identity, set `managementPolicy: Adopt`, `enrollmentMode: OperatorEnrolled`, and
  `importFrom.identityFileSecretRef` to a Secret in the identity namespace. The operator copies the
  identity file into its own Secret, verifies it, and renews it from there. The source Secret is read
  only.

```yaml
spec:
  zitiName: alice
  managementPolicy: Adopt
  enrollmentMode: OperatorEnrolled
  importFrom:
    identityFileSecretRef:
      namespace: team-a
      name: alice-legacy
    key: identity.json
```

- A `ZitiApp` can be adopted. Changing an app from `Observe` to `Adopt` takes over the entities that
  have the names the app would create.
