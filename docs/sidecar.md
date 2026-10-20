# Sidecar tunneler

A `ZitiSidecar` writes the patch that adds a Ziti tunneler to a workload, so you do not hand-write the container, the volume, the identity mount, or the resolver.

It does **not** patch your Deployment for you. The operator owns the Ziti entities it creates, and a Deployment it did not create is not its to change: patching one would be undone by the next `kubectl apply` from your GitOps tool, and would fight any other controller. The patch goes to a Secret and you apply it.

```yaml
apiVersion: ziti.alialjaffer.com/v1alpha1
kind: ZitiSidecar
metadata:
  name: billing-tunneler
  namespace: team-a
spec:
  identityRef: billing-client
  mode: Host
  manifestSecretRef:
    namespace: team-a
    name: billing-tunneler-patch
```

Then apply the patch once:

```sh
kubectl -n team-a get secret billing-tunneler-patch -o jsonpath='{.data.sidecar-patch\.yaml}' \
  | base64 -d > sidecar-patch.yaml
kubectl -n team-a patch deployment billing --patch-file sidecar-patch.yaml --type=strategic
```

`kubectl get ztc` shows `IDENTITY READY`. The patch appears only once the identity has an `identity.json`, because a tunneler with nothing to log in with cannot start.

## Modes

| Mode | What it does | What the workload needs |
|---|---|---|
| `Host` (default) | tproxy. Intercepts traffic in the pod network namespace and resolves names through Ziti. | `NET_ADMIN` and `NET_RAW`. The patch adds them, drops all other capabilities, runs as non-root, and sets `dnsPolicy: None` with the tunneler as the pod resolver. Do not set `hostNetwork`. |
| `Proxy` | Transparent proxy. Forwards to the addresses Ziti hands it. | No capabilities, no `hostNetwork`. Does not resolve names. |

In `Host` mode the patch runs `ziti tunnel tproxy`. The tunneler listens on `127.0.0.1:53` and passes names that are not Ziti services to your `resolver` through `--dnsUpstream`.

## Keys

| Key | Meaning |
|---|---|
| `identityRef` | The `ZitiIdentity` in the same namespace whose `identity.json` the tunneler uses. Immutable. |
| `mode` | `Host` or `Proxy`. Immutable. |
| `resolver` | `--dnsUpstream` for `Host` mode. Set it to the cluster DNS IP, for example `udp://10.96.0.10:53`. Without it, cluster Service names do not resolve in the pod. |
| `image` | Tunneler image. `Host` default is `ghcr.io/alialjaffer/ziti-tunneler` at the controller version, `ziti-router` with file capabilities on `ziti` and `iptables`, so it runs as non-root. `Proxy` default is `openziti/ziti-router` at the controller version. Set it to use a mirror in an air-gapped cluster. A `Host` mirror must keep the file capabilities: build it from `images/tunneler/Dockerfile`. |
| `servicePollRate` | Seconds between polls of Ziti for service changes. Default 15. |
| `manifestSecretRef` | Secret that receives `sidecar-patch.yaml`. Without it the operator only reports. |

## Conditions

| Condition | True when |
|---|---|
| `IdentityReady` | The identity has an `identity.json` for the tunneler. |
| `Synced` | The patch matches the spec. |
| `Ready` | `IdentityReady`, and the patch is written when `manifestSecretRef` is set. |

## Limits

- The patch is generated, not applied. Nothing watches your Deployment, so the sidecar does not come back if someone removes it. GitOps and this do not mix unless you commit the patched spec.
- Tested by generating the patch and checking it is valid YAML that adds exactly one container. It has not been applied to a live workload yet.