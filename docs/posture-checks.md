# Posture checks

A `ZitiPostureCheck` maps one to one to a Ziti posture check. `spec.type` selects the body shape and
`spec.data` carries the type-specific fields from the Edge Management API.

```yaml
apiVersion: ziti.alialjaffer.com/v1alpha1
kind: ZitiPostureCheck
metadata:
  name: managed-laptop
  namespace: team-a
spec:
  type: OS
  roleAttributes:
    - managed
  data:
    operatingSystems:
      - type: Windows
        versions:
          - 10.0.19045
```

Supported types are `OS`, `DOMAIN`, `MAC`, `PROCESS`, `PROCESS_MULTI`, and `MFA`.

Attach the check to a policy with `postureCheckRoles`:

```yaml
spec:
  postureCheckRoles:
    - "#managed"
```

Or on an app's simple Dial policy:

```yaml
spec:
  allow:
    groups:
      - staff
    postureCheckRoles:
      - "#managed"
```

`roleAttributes` follow the connection `roleScope`, like identity and service attributes. With
`Namespaced`, `managed` becomes `team-a.managed`, and `#managed` in a policy becomes
`#team-a.managed`. A cluster-wide posture check with a global attribute will not match under
`Namespaced`; use `roleScope: Global` or set the scoped attribute.

## Data shapes

- `OS`: `operatingSystems: [{type, versions}]`
- `DOMAIN`: `domains: [...]`
- `MAC`: `macAddresses: [...]`
- `PROCESS`: `process: {path, hash, ...}`
- `PROCESS_MULTI`: `processes: [...]`, `semantic: AnyOf|AllOf`
- `MFA`: MFA properties such as `promptOnUnlock`, `promptOnWake`, `timeoutSeconds`, and
  `ignoreLegacyEndpoints`

The check is only enforced for clients that report posture. It does not inspect Kubernetes pods.
