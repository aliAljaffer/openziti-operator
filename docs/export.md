# Export a Ziti network

`manager export` reads a Ziti network and writes Kubernetes manifests that match this operator's
CRDs. It never writes to Ziti.

```sh
go run ./cmd/main.go export --connection default --namespace team-a --output-dir ./ziti-export
```

The manager image has the same subcommand:

```sh
kubectl -n ziti-operator-system exec deploy/ziti-operator -c manager -- \
  /manager export --connection default --namespace team-a --output-dir /tmp/ziti-export
```

The output is a directory with one file per kind, a `kustomization.yaml`, and `import-report.md`.
Every generated resource defaults to `managementPolicy: Observe`, so applying it writes nothing. Flip
individual resources to `Adopt` or `Manage` after review.

## What is exported

- `ZitiConfig`
- `ZitiService`
- `ZitiServicePolicy`
- `ZitiEdgeRouterPolicy`
- `ZitiServiceEdgeRouterPolicy`
- `ZitiIdentity` metadata
- `ZitiRouter` metadata
- `ZitiCA` plus a public certificate Secret
- `ZitiJwtSigner`
- `ZitiAuthPolicy`
- `ZitiPostureCheck`

Runtime terminators are skipped by default. Use `--include-terminators` to snapshot them.

## What is not exported

- Private keys or an `identity.json` for an identity that enrolled elsewhere. The controller does not
  have them. The report lists each identity that needs an identity file before `Adopt`.
- Router enrollment JWTs. They are one-time and gone after enrollment.
- CA private keys.
- Sessions, API sessions, posture data, and enrollments.
- `ZitiConnection` credentials and CA bundle.
- `ZitiApp` bundles. The one-to-one resources are the faithful representation.
- Groups. Ziti has no group object; role attributes are exported on identities and services.

## Flags

| Flag | Default | Meaning |
|---|---|---|
| `--connection` | | `ZitiConnection` to read the URL and credentials from. |
| `--namespace` | `default` | Namespace for the generated namespaced resources. |
| `--output-dir` | `ziti-export` | Directory that receives the manifests. |
| `--management-policy` | `Observe` | `Observe` or `Adopt` for the one-to-one kinds. Identities, routers, CAs, and JWT signers are always `Observe`. |
| `--role-scope` | `Global` | Role scope of the target connection. A `Global` network imports faithfully only with `Global`. |
| `--include-terminators` | off | Export runtime terminators with a warning. |
| `--url`, `--ca-file`, `--kubeconfig` | | Same as the audit command when not using `--connection`. |

## Naming and scope

Ziti names are global and may not be valid Kubernetes names. The exporter keeps the exact name in
`spec.zitiName` and derives a DNS-safe `metadata.name`, adding a short hash on a collision.

If the target connection is `Namespaced`, the operator rewrites `#attribute` to
`#<namespace>.attribute`. Import a network built with global role attributes into a `Global`
connection, or expect to adopt and rewrite the resources. The report states the chosen scope.
