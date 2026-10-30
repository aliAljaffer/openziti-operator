# Audit a network

The `audit` command reads a whole Ziti network and lists problems. It never writes.

The manager image includes it as a subcommand. With a kubeconfig, point it at a `ZitiConnection` and it
reads the URL, the CA bundle, and the credentials from the cluster:

```sh
go run ./cmd/main.go audit --connection default [-o table|json]
```

The same command runs in the manager pod:

```sh
kubectl -n ziti-operator-system exec deploy/ziti-operator -c manager -- \
  /manager audit --connection default
```

To audit a controller that the operator does not manage, pass the URL and the credentials in the
environment:

```sh
ZITI_USERNAME=<user> ZITI_PASSWORD=<password> \
  go run ./cmd/audit --url https://<controller>/edge/management/v1 [--ca-file ca.pem] [-o table|json]
```

`--ca-file` is a PEM bundle. Without it the command uses the controller's well-known CA.

## Findings

| Code | Meaning |
|---|---|
| `NoTerminator` | The service has no terminator, or the router is in the service edge router policy but has none for it. |
| `NoBind` | No Bind policy selects a hosting identity. |
| `InertBind` | A router binds the service but is not in its router policy, so it makes no terminator. |
| `NoDialer` | No Dial policy selects an identity. |
| `NoCommonRouter` | The dialers' routers and the service routers do not overlap. |
| `OfflinePath` | The only common routers are offline. |
| `ProtocolMismatch` | The intercept captures a protocol that the host config does not allow. Understands `host.v1` and `host.v2`. |
| `MissingConfig` | The service lacks an intercept or a host config. |
| `UnusedConfig` | No service uses the config. |
| `EmptyRoles` | A policy selects nothing. |
| `ExpiredEnrollment` | An identity has an expired one-time token. |

Every finding also shows up in `status` of the resource that owns the entity: `NoTerminator` and `InertBind` on the `ZitiApp` and on the `ZitiRouter` it names, `UnusedConfig` on the `ZitiConfig`. See [Conditions and reasons](conditions.md). The command stays useful for entities no resource owns.
