# Compatibility

| Component | Tested version |
|---|---|
| Ziti controller and router | v2.0.4 |
| Kubernetes | v1.36.2 (Talos), envtest 1.37.0 |
| Go | see `go.mod` |

Other versions are not tested. The CI integration job runs against `openziti/ziti-cli:2.0.4`. Update this table when you test a new controller version.

## Ziti behavior that the operator depends on

| Behavior | Consequence |
|---|---|
| Tag filter keys allow only letters, `-`, and `_` | Tags are `ziti-operator-cluster`, `-kind`, `-namespace`, `-name`, `-uid`, `-adopted`. |
| Roles must use `@<id>`, not `@<name>` | The operator resolves router and identity names to IDs. |
| A `PUT` replaces the entity. A `PATCH` on `tags` replaces the whole tag map | The operator sends the full body, or the full merged tag map. |
| `encryptionRequired` is mandatory when creating a service | Always sent. |
| After an identity certificate is extended, the old certificate stops working | The new `identity.json` must be stored at once. See [operations](operations.md). |
