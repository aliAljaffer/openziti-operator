# ziti-operator

A Kubernetes operator for OpenZiti. You describe an app, an identity, or an access policy in YAML. The operator creates the Ziti configs, services, policies, and identities for you. It then checks that the result works.

It drives the Ziti Edge Management API. It does not install or run the Ziti controller or routers.

## Resources

| Kind | Short name | Scope | What it does |
|---|---|---|---|
| `ZitiConnection` | `ztconn` | cluster | How to reach a Ziti controller. |
| `ZitiApp` | `ztapp` | namespace | Publishes an app: configs, service, bind, router and dial policies. |
| `ZitiIdentity` | `ztid` | namespace | Creates, adopts, or observes an identity. Enrolls it and renews its certificate. |
| `ZitiAccessPolicy` | `ztap` | namespace | One Dial policy and an optional edge router policy. |

Annotate a Kubernetes Service with `ziti.alialjaffer.com/expose: "true"` and the operator creates a `ZitiApp` for it.

## First app

```yaml
apiVersion: ziti.alialjaffer.com/v1alpha1
kind: ZitiApp
metadata: {name: billing, namespace: team-a}
spec:
  expose: {addresses: [billing.example.com], ports: [443, "8000-8005"]}
  targets:
    - address: 10.0.0.5
    - address: 10.0.0.6
      cost: 10
  allow: {groups: [team-a], identities: [alice]}
  entryRouters: [edge-1]
```

`kubectl get ztapp` shows `Hosted`, `Dialable`, `RoutePath`, and `Ready`.

## More

- [Install](docs/install.md)
- [First app](docs/first-app.md)
- [Role scope](docs/role-scope.md)
- [Existing Ziti resources: Adopt and Observe](docs/existing-resources.md)
- [Audit a network](docs/audit.md)
- [Operations: metrics, sweeper, high availability](docs/operations.md)
- [Uninstall](docs/uninstall.md)
- [Compatibility](docs/compatibility.md)
- [Interop with ziti-k8s-agent](docs/interop-ziti-k8s-agent.md)
- [Contributing](CONTRIBUTING.md), [Security](SECURITY.md)

License: Apache-2.0.
