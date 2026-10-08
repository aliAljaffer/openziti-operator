# Grant access by group

A `ZitiAccessPolicy` is one Dial policy between two sets of roles. Identities join a role with `roleAttributes`, services join one with `memberOf`. Roles are Ziti semantic selectors, so they carry a `#`.

```sh
kubectl apply -k .
```

```yaml
apiVersion: ziti.alialjaffer.com/v1alpha1
kind: ZitiAccessPolicy
metadata:
  name: ops
  namespace: team-a
spec:
  identityRoles:
    - "#ops"
  serviceRoles:
    - "#ops"
  postureCheckRoles:
    - "#posix"
  edgeRouters:
    - edge-1
```

The operator never creates posture checks, so `#posix` has to exist in Ziti already. Drop that field to admit every member of `#ops`.

Here are the members. The app sets no `allow`, so this policy is the only way in.

```yaml
apiVersion: ziti.alialjaffer.com/v1alpha1
kind: ZitiIdentity
metadata:
  name: ops-runner
  namespace: team-a
spec:
  roleAttributes:
    - ops
  enrollmentMode: OperatorEnrolled
---
apiVersion: ziti.alialjaffer.com/v1alpha1
kind: ZitiApp
metadata:
  name: ops-console
  namespace: team-a
spec:
  expose:
    addresses:
      - ops-console.team-a.example.com
    ports:
      - 22
  targets:
    - address: 10.0.0.9
      port: 22
```

The policy names no member, because the roles do the work. Add a role to any identity or app and access follows.

```sh
kubectl -n team-a get ztap ops -o wide    # IDENTITIES and SERVICES must both be 1
```

With the default `roleScope: Namespaced`, both roles become `#team-a.ops`, so no other namespace can match them.
