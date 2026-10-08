# Forward a local port to a Ziti service

Create a `ZitiPortForward` to run a small proxy workload with an enrolled identity. The operator owns the Deployment. You then use the normal `kubectl port-forward` command from your workstation.

```yaml
apiVersion: ziti.alialjaffer.com/v1alpha1
kind: ZitiPortForward
metadata:
  name: billing
  namespace: team-a
spec:
  identityRef: billing-client
  service: billing
  port: 8080
```

Wait for `Ready=True`, then run:

```sh
kubectl -n team-a port-forward deployment/billing 8080:8080
```

Open `http://localhost:8080` on your workstation. The proxy pod dials the Ziti service through its identity. `port` is both the local proxy listener inside the pod and the port in the Ziti service name. Choose a port the service exposes.

## What the operator creates

- A one-replica Deployment named after the resource. It runs `ziti tunnel proxy` with the `identity.json` from the referenced `ZitiIdentity` Secret.
- The Deployment is removed when you delete the `ZitiPortForward`.
- No Kubernetes Service is created. `kubectl port-forward` connects to the Deployment directly, so the proxy port is not exposed to the cluster network.

The resource waits until the identity is enrolled and its identity file exists. It does not create an identity or grant access to the service; the identity must already be allowed to dial it.

`Ready=True` means the proxy listener is ready. It does not verify that Ziti policies allow the identity to reach the service. Check `kubectl logs deployment/billing` if the service dial fails.

## Keys

| Key | Meaning |
|---|---|
| `connectionRef` | `ZitiConnection` to use. Default `default`. |
| `identityRef` | Enrolled `ZitiIdentity` in the same namespace. Immutable. |
| `service` | Ziti service name to dial. Immutable. |
| `port` | Proxy listener and Ziti service port. Immutable. |

## Conditions

| Condition | True when |
|---|---|
| `IdentityReady` | The identity is enrolled and its identity file exists. |
| `Workload` | The proxy Deployment has a ready replica. |
| `Ready` | The identity is ready and the proxy listener is ready. |

## Limits

- This resource prepares the proxy inside Kubernetes. It cannot open a port on your workstation; you still run `kubectl port-forward` there.
- The proxy Deployment runs one replica and must not be shared by multiple users.
- A proxy listener can be ready while the Ziti identity is not allowed to dial the service. `Ready` reports local readiness only.
