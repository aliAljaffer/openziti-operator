# Start an edge router

A `ZitiRouter` creates the edge router in Ziti and hands you its enrollment JWT in a Secret. It does not install the router. That part is still a Helm chart, a Deployment, or a VM.

```yaml
apiVersion: ziti.alialjaffer.com/v1alpha1
kind: ZitiRouter
metadata:
  name: edge-1
spec:
  roleAttributes:
    - edge
  tunnelerEnabled: true
  enrollmentSecretRef:
    namespace: routers
    name: edge-1-enrollment
  advertisedAddress: edge-1.example.com
---
apiVersion: v1
kind: Namespace
metadata:
  name: routers
```

`advertisedAddress` is where clients and other routers reach the router. Leave it out and the generated files say `CHANGE_ME.invalid`, which enrolls but serves nobody. `tunnelerEnabled` is required for a router that hosts a `ZitiApp`.

```sh
kubectl apply -k .
kubectl -n routers get ztrouter edge-1 -o wide
```

Next to the JWT, the operator writes a `docker-compose.yml` for a VM and a `deployment.yaml` for a cluster. Pick one.

```sh
# In a cluster
kubectl -n routers get secret edge-1-enrollment -o jsonpath='{.data.deployment\.yaml}' | base64 -d | kubectl apply -f -

# On a VM
kubectl -n routers get secret edge-1-enrollment -o jsonpath='{.data.docker-compose\.yml}' | base64 -d > docker-compose.yml
docker compose up -d
```

Pass the JWT to the installer. It works once and expires in about three hours, and the operator asks for a new one when it runs out.

```sh
kubectl -n routers get secret edge-1-enrollment -o jsonpath='{.data.enrollment\.jwt}' | base64 -d
```

The operator removes the JWT and both files once the router has enrolled. Open port 3022 to clients, and add `edge-1` to `hostingRouters` and `entryRouters` on the connection. See [Edge routers](../../docs/routers.md).
