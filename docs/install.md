# Install

## Before you start

- A Ziti controller with the Edge Management API reachable from the cluster.
- A Ziti identity that may call the management API. An admin identity works.
- The controller CA certificate.

## Steps

1. Build the installer.

   ```sh
   make build-installer IMG=<registry>/ziti-operator:<tag>
   ```

2. Apply it. It creates the CRDs, the RBAC, and a Deployment with two replicas.

   ```sh
   kubectl apply -f dist/install.yaml
   ```

3. Store the controller CA in a ConfigMap.

   ```sh
   kubectl -n ziti-operator-system create configmap ziti-root-ca --from-file=ca.crt=ca.pem
   ```

4. Store the credential in a Secret. Choose one form.

   User name and password:

   ```sh
   kubectl -n ziti-operator-system create secret generic ziti-operator-credential \
     --from-literal=username=<user> --from-literal=password=<password>
   ```

   Certificate. The Secret needs the keys `tls.crt` and `tls.key`. cert-manager writes this format.

5. Create the connection. Start from `config/samples/ziti_v1alpha1_ziticonnection.yaml`.

   ```sh
   kubectl apply -f config/samples/ziti_v1alpha1_ziticonnection.yaml
   kubectl get ztconn
   ```

   `CONNECTED` must show `True`.

## Connection options

| Key | Meaning |
|---|---|
| `managementUrl` | Edge Management API URL. Must start with `https://`. |
| `auth.updb` or `auth.cert` | Set exactly one. |
| `roleScope` | `Namespaced` (default) or `Global`. See [role scope](role-scope.md). |
| `hostingRouters` | Routers an app may run behind. The first one is the default. |
| `defaultEdgeRouters` | Routers added to every app's router policy. |
| `allowedNamespaces` | Label selector. Only these namespaces may use the connection. Empty means all. |
| `clusterId` | Separates operators that share one Ziti network, for example staging and production. |

## Manager flags

| Flag | Default | Meaning |
|---|---|---|
| `--leader-elect` | off in the binary, on in the Deployment | One active replica. |
| `--ziti-requests-per-second` | 10 | Rate limit for the Ziti API. |
| `--orphan-policy` | `report` | `report` or `delete`. See [operations](operations.md). |
| `--orphan-sweep-interval` | 1h | How often to look for orphaned entities. |
