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
| `ZitiConfig`, `ZitiService`, `ZitiServicePolicy`, `ZitiEdgeRouterPolicy`, `ZitiServiceEdgeRouterPolicy` | `ztcfg`, `ztsvc`, `ztsp`, `zterp`, `ztserp` | namespace | One resource per Ziti object, for full control. See [Full control](docs/full-control.md). |
| `ZitiRouter` | `ztrouter` | cluster | Creates an edge router in Ziti and puts its enrollment JWT in a Secret. See [Edge routers](docs/routers.md). |
| `ZitiCA` | `ztca` | cluster | Registers a CA (for example a cert-manager CA) so certificates it issues can log in. |
| `ZitiJwtSigner` | `ztjwt` | cluster | Trusts tokens from an issuer (your cluster) and creates the auth policy. Lets service accounts log in. |

Annotate a Kubernetes Service with `alialjaffer.ziti/expose: "true"` and the operator creates a `ZitiApp` for it.

## First app

```yaml
apiVersion: alialjaffer.ziti/v1alpha1
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

```sh
kubectl get ziti -A            # every kind in one list (the category "ziti")
kubectl explain ztapp.spec     # field help, also for nested fields: ztapp.spec.expose.ports
```

## More

- [Install](docs/install.md)
- [First app](docs/first-app.md)
- [Role scope](docs/role-scope.md)
- [Existing Ziti resources: Adopt and Observe](docs/existing-resources.md)
- [Log in with a service account token](docs/service-account-tokens.md)
- [Use a cert-manager CA](docs/cert-manager.md)
- [Full control: one resource per Ziti object](docs/full-control.md)
- [Edge routers](docs/routers.md)
- Resource reference: [ZitiConnection](docs/reference/ZitiConnection.md), [ZitiApp](docs/reference/ZitiApp.md), [ZitiIdentity](docs/reference/ZitiIdentity.md), [ZitiAccessPolicy](docs/reference/ZitiAccessPolicy.md), [ZitiJwtSigner](docs/reference/ZitiJwtSigner.md), [ZitiCA](docs/reference/ZitiCA.md)
- [Conditions and reasons (troubleshooting)](docs/conditions.md)
- [Audit a network](docs/audit.md)
- [Operations: metrics, sweeper, high availability](docs/operations.md)
- [Uninstall](docs/uninstall.md)
- [Compatibility](docs/compatibility.md)
- [Interop with ziti-k8s-agent](docs/interop-ziti-k8s-agent.md)
- [Contributing](CONTRIBUTING.md), [Security](SECURITY.md)

License: Apache-2.0.
