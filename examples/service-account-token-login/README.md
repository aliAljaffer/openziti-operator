# Log in with a service account token

A workload can log in with its Kubernetes service account token. No enrollment, no JWT, no identity file. Ziti checks the signature and matches the token subject to the identity.

```yaml
apiVersion: ziti.alialjaffer.com/v1alpha1
kind: ZitiJwtSigner
metadata:
  name: k8s
spec:
  audience: ziti
  keys:
    kubernetes: {}
```

The signer is cluster scoped, so only a cluster admin creates it. It makes Ziti trust tokens from this cluster with the audience `ziti`. `kubectl get ztjwt` shows the issuer and the key.

```yaml
apiVersion: ziti.alialjaffer.com/v1alpha1
kind: ZitiIdentity
metadata:
  name: web-bot
  namespace: team-a
spec:
  enrollmentMode: None
  serviceAccount: web-bot
  authPolicy: k8s
  roleAttributes:
    - bots
```

`serviceAccount` sets the external id to `system:serviceaccount:team-a:web-bot`, the token subject. `Ready` shows reason `TokenLogin` and no Secret is created.

```sh
kubectl apply -k .
```

The workload runs as a non-root user with a read-only root filesystem, the same hardening the chart gives the manager.

The Pod asks for a token with the audience `ziti` and mounts it.

```yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: web-bot
  namespace: team-a
---
apiVersion: v1
kind: Pod
metadata:
  name: web-bot
  namespace: team-a
spec:
  serviceAccountName: web-bot
  securityContext:
    runAsNonRoot: true
    seccompProfile:
      type: RuntimeDefault
  containers:
    - name: curl
      image: curlimages/curl
      command:
        - sleep
        - "3600"
      securityContext:
        readOnlyRootFilesystem: true
        allowPrivilegeEscalation: false
        capabilities:
          drop:
            - ALL
      volumeMounts:
        - name: ziti-token
          mountPath: /var/run/secrets/ziti
          readOnly: true
  volumes:
    - name: ziti-token
      projected:
        sources:
          - serviceAccountToken:
              audience: ziti
              expirationSeconds: 3600
              path: token
```

Log in with the token. Your client must support external JWT login.

```sh
kubectl -n team-a exec web-bot -c curl -- curl -X POST \
  "https://ziti-controller-mgmt.example.com/edge/client/v1/authenticate?method=ext-jwt" \
  -H "Authorization: Bearer $(cat /var/run/secrets/ziti/token)"
```

Key rotation is the catch. With `kubernetes: {}` the operator updates the signer in about ten minutes, and tokens signed by the new key fail until then. See [Service account tokens](../../docs/service-account-tokens.md).
