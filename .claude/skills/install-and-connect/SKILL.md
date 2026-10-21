---
name: install-and-connect
description: Use when you install or upgrade the operator, create or change a ZitiConnection, get the controller CA bundle, choose chart values, or enable the validation webhook. Gives the steps and the keys.
---

# Install the operator and connect to Ziti

Details: `docs/install.md`, `docs/ca-bundle.md`, `docs/validation.md`, `examples/connect-to-controller`.

## Inputs

- A Ziti controller whose Edge Management API the cluster reaches.
- A user or client certificate that may call the API. An admin identity works.
- The controller CA bundle (key `ca.crt`).

## Steps

1. Create the namespace `ziti-operator-system`.
2. Create ConfigMap `ziti-root-ca` with `ca.crt`. The bundle must include the root. Do not build it from `openssl s_client` alone. Extract it from `/.well-known/est/cacerts` (see `docs/ca-bundle.md`) and run `openssl verify`.
3. Create the credential Secret. Password login: keys `username`, `password`. Certificate login: keys `tls.crt`, `tls.key`.
4. Run `helm install ziti-operator oci://ghcr.io/alialjaffer/charts/ziti-operator -n ziti-operator-system` with `connection.create=true`, `connection.managementUrl`, `connection.hostingRouters`, and exactly one of `connection.auth.updb.secretName` or `connection.auth.cert.secretName`.
5. Run `kubectl get ztconn`. `CONNECTED` must be `True`.

Without Helm: `make build-installer IMG=...`, apply `dist/install.yaml`, then apply a `ZitiConnection` from `config/samples`.

## ZitiConnection keys

| Key | Rule |
|---|---|
| `managementUrl` | Starts with `https://`. The host must be in the controller certificate SAN. |
| `roleScope` | `Namespaced` (default) or `Global`. See `.claude/skills/access-and-scope/SKILL.md`. |
| `hostingRouters` | Routers an app may use as `hostedBy`. The first is the default. |
| `entryRouters`, `defaultEdgeRouters` | Limit and add routers for dial paths. |
| `allowedNamespaces` | Label selector of namespaces that may use the connection. |
| `clusterId` | Separates operators that share one Ziti network. |

## Chart values that bite

- `rbac.secretNamespaces`: list every namespace that holds a `ZitiIdentity` Secret, a `ZitiCA` Secret, or a router enrollment Secret. Empty means the release namespace only. `rbac.clusterWideSecrets=true` grants all namespaces.
- `rbac.serviceNamespaces`: where the operator may write Services for in-cluster routers.
- `webhook.enabled=true`: offline validation at admission. It ships with `failurePolicy: Ignore`. Move to `Fail` only after it is stable.
- `trustManager.enabled`: keeps the CA ConfigMap in sync on CA rotation.
- `crd.keep=true` keeps CRDs on uninstall. See `docs/uninstall.md`.

## If not connected

See `.claude/skills/troubleshoot-resources/SKILL.md`, section ZitiConnection.
