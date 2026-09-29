# Audit a network

The `audit` command reads a whole Ziti network and lists problems. It never writes.

```sh
ZITI_USERNAME=<user> ZITI_PASSWORD=<password> \
  go run ./cmd/audit --url https://<controller>/edge/management/v1 [--ca-file ca.pem] [-o table|json]
```

It needs `ZITI_USERNAME` and `ZITI_PASSWORD` in the environment. Without `--ca-file` it uses the controller's own CA.

## Findings

| Code | Meaning |
|---|---|
| `NoTerminator` | The service has no terminator. |
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
