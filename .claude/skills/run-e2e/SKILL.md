---
name: run-e2e
description: Use when you must test the operator against a real Ziti controller or a real Kubernetes cluster. Covers the local controller, integration tests, the kind run with the Helm chart, the homelab run, the client-pod dial test, and safe cleanup.
---

# Run real tests

## Local Ziti controller and integration tests

```sh
docker run -d --name ziti-spike -p 1280:1280 -p 3022:3022 openziti/ziti-cli:2.0.4 \
  edge quickstart --ctrl-address localhost --ctrl-port 1280 --router-address localhost --password admin123
# wait about 40 seconds
ZITI_MGMT_URL=https://localhost:1280/edge/management/v1 ZITI_USERNAME=admin ZITI_PASSWORD=admin123 \
  go test -tags integration -count=1 ./internal/...
```

The tests use fixed names (`team-a.backend`). Stop any operator that runs against the same controller first. CI runs the same command (`integration.yml`). The router is named `router-instance-1`.

## In a kind cluster (chart, image, client dial)

1. `kind create cluster --name ziti-e2e`, then `kind get kubeconfig --name ziti-e2e > kind.kubeconfig`.
2. Start the controller on the kind network so pods can resolve it: `docker run -d --name ziti-ctrl --network kind -p 1280:1280 openziti/ziti-cli:2.0.4 edge quickstart --ctrl-address ziti-ctrl --ctrl-port 1280 --router-address ziti-ctrl --password admin123`. Start a target the router can reach: `docker run -d --name ziti-target --network kind nginx:alpine`.
3. Build and load the image: `docker build -t ziti-operator:dev .` then `kind load docker-image ziti-operator:dev --name ziti-e2e`.
4. Get the CA: `openssl s_client -connect localhost:1280 -showcerts </dev/null | awk '/BEGIN CERT/,/END CERT/' > ca.pem`. Create the namespace `ziti-operator-system`, a ConfigMap `ziti-root-ca`, and a Secret `ziti-operator-credential`.
5. `helm install ziti-operator charts/chart -n ziti-operator-system --set manager.image.repository=ziti-operator --set manager.image.tag=dev --set manager.image.pullPolicy=Never --set metrics.secure=false --set connection.create=true --set connection.managementUrl=https://ziti-ctrl:1280/edge/management/v1 --set 'connection.hostingRouters={router-instance-1}' --set connection.roleScope=Global --set connection.auth.updb.secretName=ziti-operator-credential`.
6. Apply a `ZitiIdentity` (`OperatorEnrolled`) and a `ZitiApp` whose target is `ziti-target:80`.
7. Client pod: one container `openziti/ziti-cli:2.0.4` running `ziti tunnel proxy -i /id/identity.json <zitiName>:8080` with the identity Secret mounted at `/id`, one container `curlimages/curl` running `sleep`. `kubectl exec ... curl localhost:8080` must return 200.

The image is `arm64` on Apple silicon. The homelab nodes are `amd64` and have no registry, so the chart run uses kind.

## Homelab cluster (manager on the workstation)

Use `KUBECONFIG=$HOME/.kube/homelab-talos`. Run `make install`, create the namespace, Secret, ConfigMap, and a `ZitiConnection` with `managementUrl` `https://localhost:1280/...`. Start `./bin/manager --leader-elect --leader-election-namespace ziti-operator --metrics-bind-address=:8090 --metrics-secure=false`.

## Cleanup order

1. Delete the custom resources and wait. Their finalizers need the connection.
2. Delete the `ZitiConnection`.
3. Stop the manager. A stale manager keeps port 8081: `lsof -ti tcp:8081 | xargs kill`.
4. `make uninstall`, delete the namespaces, `docker rm -f` the containers.

If the connection is already gone, patch the finalizers off the leftovers.

## Pitfalls

- A `kubectl delete` with a resource name and `--all` fails. Use one or the other.
- Every test run needs a fresh CA in the ConfigMap when the controller container was recreated.
- Date printer columns show `<invalid>` for a future time. Use string columns for expiry dates.
- Never run the operator against the live Ziti network.
