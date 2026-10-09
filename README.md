# ziti-operator

[![CI](https://github.com/aliAljaffer/openziti-operator/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/aliAljaffer/openziti-operator/actions/workflows/ci.yml)
[![Dependabot](https://img.shields.io/badge/Dependabot-enabled-025E8C?logo=dependabot)](.github/dependabot.yml)

A Kubernetes operator that makes routine [OpenZiti](https://openziti.io) work simple.

You describe an app, an identity, or a router in YAML. The operator creates the Ziti configs, services, policies, and identities, then checks that the result works and reports it in `status`. Nobody on the team needs to know the OpenZiti model to publish an app, enroll an identity, or start a router.

It drives the Ziti Edge Management API. It does not install or run the Ziti controller.

This is an independent community project. It is not an official OpenZiti or NetFoundry product.

**Status:** `v0.1.2`, API `v1alpha1`. Tested with Ziti controller and router v2.0.4 and Kubernetes v1.36. See [Compatibility](docs/compatibility.md).

## What it does for you

| You want to | You apply |
|---|---|
| Publish an app so authorized people can reach it | A `ZitiApp`. The operator creates the configs, the service, the bind and dial policies, and the router policies. |
| Enroll or remove an identity | A `ZitiIdentity`. The operator enrolls it, writes the identity file to a Secret, and renews its certificate. |
| Start a router on a VM or in a cluster | A `ZitiRouter`. The operator writes a ready-made `docker-compose.yml` and a Kubernetes manifest to a Secret, and reports which services the router terminates. |
| Take over what you built by hand | `managementPolicy: Adopt` or `Observe` on the same resources. |
| Log a workload in through a Ziti tunneler | A `ZitiSidecar`. It writes the patch that adds the tunneler, so you do not hand-write the container, identity mount, or resolver. |
| Expose an HTTP Ingress | Annotate it with `ziti.alialjaffer.com/expose: "true"`; the operator creates a `ZitiApp` for its host and backend Service. |
| Let a workload log in with a certificate or a service account token | `ZitiCA` and `ZitiJwtSigner`, with cert-manager or your cluster issuer. |

Annotate a Kubernetes Service or supported HTTP Ingress with `ziti.alialjaffer.com/expose: "true"` and the operator creates the `ZitiApp` for it. See [Expose an Ingress](docs/ingress.md) for supported Ingress routes.

For full control, there is one resource for every Ziti object too. See [Full control](docs/full-control.md).

## Quick start

You need a Ziti controller with the Edge Management API reachable from the cluster, an account that may use it, and the controller CA certificate.

```sh
kubectl create namespace ziti-operator-system
kubectl -n ziti-operator-system create configmap ziti-root-ca --from-file=ca.crt=ca.pem
kubectl -n ziti-operator-system create secret generic ziti-operator-credential \
  --from-literal=username=<user> --from-literal=password=<password>

helm install ziti-operator oci://ghcr.io/alialjaffer/charts/ziti-operator --version 0.1.2 -n ziti-operator-system \
  --set connection.create=true \
  --set connection.managementUrl=https://<controller>/edge/management/v1 \
  --set 'connection.hostingRouters={<router>}' \
  --set connection.auth.updb.secretName=ziti-operator-credential

kubectl get ztconn        # CONNECTED must show True
```

The chart is `oci://ghcr.io/alialjaffer/charts/ziti-operator` and the image is `ghcr.io/alialjaffer/openziti-operator`. Details are in [Install](docs/install.md).

## Publish your first app

```yaml
apiVersion: ziti.alialjaffer.com/v1alpha1
kind: ZitiApp
metadata:
  name: billing
  namespace: team-a
spec:
  expose:
    addresses:
      - billing.example.com
    ports:
      - 443
      - "8000-8005"
  targets:
    - address: 10.0.0.5
    - address: 10.0.0.6
      cost: 10
  allow:
    groups:
      - team-a
    identities:
      - alice
  entryRouters:
    - edge-1
```

`kubectl get ztapp` shows `Hosted`, `Dialable`, `RoutePath`, and `Ready`. When something is wrong, the condition reason says what. See [Conditions and reasons](docs/conditions.md).

```sh
kubectl get ziti -A            # every kind in one list
kubectl explain ztapp.spec     # field help, also for nested fields
```

## Resources

| Kind | Short name | Scope | What it does |
|---|---|---|---|
| `ZitiConnection` | `ztconn` | cluster | How to reach a Ziti controller. |
| `ZitiApp` | `ztapp` | namespace | Publishes an app: configs, service, bind, router and dial policies. |
| `ZitiIdentity` | `ztid` | namespace | Creates, adopts, or observes an identity. Enrolls it and renews its certificate. Can create the cert-manager `Certificate` for certificate login. |
| `ZitiAccessPolicy` | `ztap` | namespace | One Dial policy and an optional edge router policy. |
| `ZitiRouter` | `ztrouter` | cluster | Creates an edge router and puts its enrollment JWT and ready-made manifests in a Secret. See [Edge routers](docs/routers.md). |
| `ZitiCA` | `ztca` | cluster | Registers a CA (for example a cert-manager CA) so certificates it issues can log in. |
| `ZitiJwtSigner` | `ztjwt` | cluster | Trusts tokens from an issuer (your cluster) and creates the auth policy. |
| `ZitiSidecar` | `ztc` | namespace | Writes the patch that adds a Ziti tunneler to a workload. See [Sidecar tunneler](docs/sidecar.md). |
| `ZitiConfig`, `ZitiService`, `ZitiServicePolicy`, `ZitiEdgeRouterPolicy`, `ZitiServiceEdgeRouterPolicy`, `ZitiTerminator` | `ztcfg`, `ztsvc`, `ztsp`, `zterp`, `ztserp`, `ztterm` | namespace | One resource per Ziti object. |

## Safe by default

- The operator owns only the Ziti entities that carry its ownership tag. It never changes or deletes anything else.
- `Adopt` updates only the fields your resource sets. `Observe` never writes.
- Deleting a resource deletes its Ziti entities. Use `deletionPolicy: Orphan` to keep them.
- `--orphan-policy=report` (the default) only reports entities whose owner is gone. See [Operations](docs/operations.md).
- Two replicas run with leader election.

## Documentation

- [Install](docs/install.md), [First app](docs/first-app.md), [Uninstall](docs/uninstall.md)
- [Role scope](docs/role-scope.md), [Existing Ziti resources](docs/existing-resources.md)
- [Edge routers](docs/routers.md), [Full control](docs/full-control.md)
- [Sidecar tunneler](docs/sidecar.md), [Log in with a service account token](docs/service-account-tokens.md), [Use a cert-manager CA](docs/cert-manager.md)
- [Conditions and reasons](docs/conditions.md), [Audit a network](docs/audit.md), [Operations](docs/operations.md)
- [Compatibility](docs/compatibility.md), [Interop with ziti-k8s-agent](docs/interop-ziti-k8s-agent.md)
- Reference for every kind: [ZitiConnection](docs/reference/ZitiConnection.md), [ZitiApp](docs/reference/ZitiApp.md), [ZitiIdentity](docs/reference/ZitiIdentity.md), [ZitiAccessPolicy](docs/reference/ZitiAccessPolicy.md), [ZitiRouter](docs/reference/ZitiRouter.md), [ZitiCA](docs/reference/ZitiCA.md), [ZitiJwtSigner](docs/reference/ZitiJwtSigner.md), [ZitiTerminator](docs/reference/ZitiTerminator.md)

## Contributing and security

Read [CONTRIBUTING.md](CONTRIBUTING.md). Report vulnerabilities as described in [SECURITY.md](SECURITY.md).

## License

Apache-2.0. The full text is in the `LICENSE` file.
