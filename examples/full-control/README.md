# Full control

`ZitiApp` creates everything an app needs. When you know OpenZiti, use one resource per Ziti object instead. Nothing is derived and nothing is defaulted.

Seven resources replace the `ZitiApp` above: two configs, the service, a bind policy, a dial policy, a service edge router policy, and an edge router policy.

```yaml
apiVersion: ziti.alialjaffer.com/v1alpha1
kind: ZitiConfig
metadata:
  name: web-intercept
  namespace: team-a
spec:
  zitiName: web-intercept
  type: intercept.v1
  data:
    protocols:
      - tcp
    addresses:
      - web.team-a.example.com
    portRanges:
      - low: 8080
        high: 8080
---
apiVersion: ziti.alialjaffer.com/v1alpha1
kind: ZitiConfig
metadata:
  name: web-host
  namespace: team-a
spec:
  zitiName: web-host
  type: host.v2
  data:
    terminators:
      - address: 10.0.0.5
        port: 8080
        protocol: tcp
---
apiVersion: ziti.alialjaffer.com/v1alpha1
kind: ZitiService
metadata:
  name: web
  namespace: team-a
spec:
  zitiName: web.team-a.example.com
  configs:
    - web-intercept
    - web-host
  roleAttributes:
    - web
---
apiVersion: ziti.alialjaffer.com/v1alpha1
kind: ZitiServicePolicy
metadata:
  name: web-bind
  namespace: team-a
spec:
  zitiName: web-bind
  type: Bind
  identityRoles:
    - "@edge-1"
  serviceRoles:
    - "@web.team-a.example.com"
---
apiVersion: ziti.alialjaffer.com/v1alpha1
kind: ZitiServicePolicy
metadata:
  name: web-dial
  namespace: team-a
spec:
  zitiName: web-dial
  type: Dial
  identityRoles:
    - "#staff"
  serviceRoles:
    - "#web"
---
apiVersion: ziti.alialjaffer.com/v1alpha1
kind: ZitiServiceEdgeRouterPolicy
metadata:
  name: web-serp
  namespace: team-a
spec:
  zitiName: web-serp
  serviceRoles:
    - "#web"
  edgeRouterRoles:
    - "@edge-1"
---
apiVersion: ziti.alialjaffer.com/v1alpha1
kind: ZitiEdgeRouterPolicy
metadata:
  name: web-erp
  namespace: team-a
spec:
  zitiName: web-erp
  identityRoles:
    - "#staff"
  edgeRouterRoles:
    - "@edge-1"
```

Order does not matter. A resource that names an object which does not exist yet reports `TargetNotFound` and is retried.

A `@name` role names one object, so it needs `roleScope: Global`. A `#attribute` role is namespaced.

```sh
kubectl apply -k web.yaml
```

For a fixed route, add a terminator that forwards the service to one address.

```yaml
apiVersion: ziti.alialjaffer.com/v1alpha1
kind: ZitiTerminator
metadata:
  name: web-via-edge-1
  namespace: team-a
spec:
  service: web.team-a.example.com
  router: edge-1
  address: tcp:10.0.0.5:8080
  cost: 10
```

```sh
kubectl apply -f terminator.yaml
```

The bind policy and the service edge router policy must both name the hosting router, or the service gets no terminator. See [Full control](../../docs/full-control.md).
