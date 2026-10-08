# Install

## Before you start

- A Ziti controller with the Edge Management API reachable from the cluster.
- A Ziti identity that may call the management API. An admin identity works.
- The controller CA certificate. See [CA bundle](ca-bundle.md) to extract it or to create your own.

## Install with Helm

1. Store the controller CA in a ConfigMap and the credential in a Secret, both in the release namespace.

   ```sh
   kubectl create namespace ziti-operator-system
   kubectl -n ziti-operator-system create configmap ziti-root-ca --from-file=ca.crt=ca.pem
   kubectl -n ziti-operator-system create secret generic ziti-operator-credential \
     --from-literal=username=<user> --from-literal=password=<password>
   ```

   For certificate login, create a Secret with the keys `tls.crt` and `tls.key` instead. cert-manager writes this format.

2. Install the chart. It creates the CRDs, the RBAC, a Deployment with two replicas, and the `ZitiConnection`. The chart is published for every release. The image is `ghcr.io/alialjaffer/openziti-operator`, tag from the chart `appVersion`. To install from a clone of the repository instead, use `charts/chart` in place of the `oci://` address. Use `--set manager.image.repository=<registry>/ziti-operator --set manager.image.tag=<tag>` for your own build.

   ```sh
   helm install ziti-operator oci://ghcr.io/alialjaffer/charts/ziti-operator --version 0.1.2 -n ziti-operator-system \
     --set connection.create=true \
     --set connection.managementUrl=https://<controller>/edge/management/v1 \
     --set 'connection.hostingRouters={<router>}' \
     --set connection.auth.updb.secretName=ziti-operator-credential
   ```

3. Check the connection.

   ```sh
   kubectl get ztconn
   ```

   `CONNECTED` must show `True`.

### Chart values

| Value | Default | Meaning |
|---|---|---|
| `manager.replicas` | 2 | One leader and one standby. |
| `operator.zitiRequestsPerSecond` | 10 | Rate limit for the Ziti API. |
| `operator.orphanPolicy` | `report` | `report` or `delete`. See [operations](operations.md). |
| `operator.orphanSweepInterval` | `1h` | How often to look for orphaned entities. |
| `connection.create` | `false` | Create the `ZitiConnection`. Without it, create your own. |
| `connection.managementUrl`, `connection.hostingRouters` | none | Required when `connection.create` is true. |
| `connection.auth.updb.secretName` or `connection.auth.cert.secretName` | none | Set exactly one. |
| `connection.caBundle.configMapName` | `ziti-root-ca` | ConfigMap in the release namespace. |
| `connection.roleScope`, `entryRouters`, `defaultEdgeRouters`, `clusterId`, `allowedNamespaces` | see below | Same as the `ZitiConnection` keys. |
| `trustManager.enabled`, `trustManager.sources` | off | Copy the Ziti root CA into the release namespace with a trust-manager `Bundle`. |
| `rbac.secretNamespaces` | empty | Namespaces for Secret access, in addition to the release namespace. List every namespace that holds a `ZitiIdentity`. |
| `rbac.clusterWideSecrets` | `false` | Set `true` to grant Secret access in all namespaces with a ClusterRole. Upgrade note: before this default, access was cluster-wide. |
| `metrics.enabled`, `metrics.secure`, `prometheus.enabled` | on, on, off | Metrics endpoint and an optional ServiceMonitor. |
| `crd.keep` | `true` | Keep the CRDs when the release is uninstalled. |

## Install without Helm

1. Build the installer.

   ```sh
   make build-installer IMG=<registry>/ziti-operator:<tag>
   ```

2. Apply it. It creates the CRDs, the RBAC, and a Deployment with two replicas.

   ```sh
   kubectl apply -f dist/install.yaml
   ```

3. Store the CA and the credential as above, in the namespace `ziti-operator-system`.

4. Create the connection. Start from `config/samples/ziti_v1alpha1_ziticonnection.yaml`.

   ```sh
   kubectl apply -f config/samples/ziti_v1alpha1_ziticonnection.yaml
   kubectl get ztconn
   ```

## Connection options

| Key | Meaning |
|---|---|
| `managementUrl` | Edge Management API URL. Must start with `https://`. |
| `auth.updb` or `auth.cert` | Set exactly one. |
| `roleScope` | `Namespaced` (default) or `Global`. See [role scope](role-scope.md). |
| `hostingRouters` | Routers an app may run behind. The first one is the default. |
| `entryRouters` | Routers that an app `entryRouters` and an access policy `edgeRouters` may name, with `defaultEdgeRouters`. Empty means any router. |
| `defaultEdgeRouters` | Routers added to every app's router policy. |
| `allowedNamespaces` | Label selector. Only these namespaces may use the connection. Empty means all. |
| `clusterId` | Separates operators that share one Ziti network, for example staging and production. |

## Manager flags

| Flag | Default | Meaning |
|---|---|---|
| `--leader-elect` | off in the binary, on in the Deployment | One active replica. |
| `--ziti-requests-per-second` | 10 | Rate limit for the Ziti API. |
| `--secret-namespaces` | empty | Namespaces where the operator may use Secrets. Empty means all. |
| `--orphan-policy` | `report` | `report` or `delete`. See [operations](operations.md). |
| `--orphan-sweep-interval` | 1h | How often to look for orphaned entities. |
