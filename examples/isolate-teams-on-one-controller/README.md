# Isolate teams on one controller

Two teams share one Ziti network. The connection is limited to the namespaces allowed to use it, and each team works only inside its own namespace.

```sh
kubectl apply -k .
```

```yaml
apiVersion: v1
kind: Namespace
metadata:
  name: team-a
  labels:
    ziti-network: shared
---
apiVersion: v1
kind: Namespace
metadata:
  name: team-b
  labels:
    ziti-network: shared
```

```yaml
apiVersion: ziti.alialjaffer.com/v1alpha1
kind: ZitiConnection
metadata:
  name: shared
spec:
  managementUrl: https://ziti-controller-mgmt.example.com/edge/management/v1
  caBundle:
    configMapRef:
      namespace: ziti-operator-system
      name: ziti-root-ca
      key: ca.crt
  auth:
    updb:
      secretRef:
        namespace: ziti-operator-system
        name: ziti-operator-credential
  clusterId: teams
  roleScope: Namespaced
  allowedNamespaces:
    matchLabels:
      ziti-network: shared
  hostingRouters:
    - edge-1
  entryRouters:
    - edge-1
```

`allowedNamespaces` is a label selector, so only labeled namespaces may set `connectionRef: shared`. `clusterId` keeps these identity names apart from other operators on the same network.

Team A:

```yaml
apiVersion: ziti.alialjaffer.com/v1alpha1
kind: ZitiIdentity
metadata:
  name: alice
  namespace: team-a
spec:
  connectionRef: shared
  roleAttributes:
    - staff
  enrollmentMode: OperatorEnrolled
---
apiVersion: ziti.alialjaffer.com/v1alpha1
kind: ZitiApp
metadata:
  name: web
  namespace: team-a
spec:
  connectionRef: shared
  expose:
    addresses:
      - web.team-a.example.com
    ports:
      - 8080
  targets:
    - address: 10.0.0.5
      port: 8080
  allow:
    groups:
      - staff
  entryRouters:
    - edge-1
```

Team B, the same file with the namespace, the address, and the target changed:

```yaml
apiVersion: ziti.alialjaffer.com/v1alpha1
kind: ZitiIdentity
metadata:
  name: alice
  namespace: team-b
spec:
  connectionRef: shared
  roleAttributes:
    - staff
  enrollmentMode: OperatorEnrolled
---
apiVersion: ziti.alialjaffer.com/v1alpha1
kind: ZitiApp
metadata:
  name: web
  namespace: team-b
spec:
  connectionRef: shared
  expose:
    addresses:
      - web.team-b.example.com
    ports:
      - 8080
  targets:
    - address: 10.0.0.6
      port: 8080
  allow:
    groups:
      - staff
  entryRouters:
    - edge-1
```

Both teams grant `#staff`. `roleScope: Namespaced` rewrites it to `#team-a.staff` and `#team-b.staff`, so neither dial policy can select the other team. The two identities share a name and still get distinct Ziti names, `teams-team-a-alice` and `teams-team-b-alice`.

```sh
kubectl get ztapp -A -o wide
```

A team cannot loosen `roleScope`, because the connection is cluster scoped. Only cluster admins create it.
