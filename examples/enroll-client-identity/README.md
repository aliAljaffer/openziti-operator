# Enroll a client identity

`enrollmentMode: OperatorEnrolled` lets the operator do the enrollment for you. It writes the identity file to a Secret and renews the client certificate before it expires. The workload only has to mount that Secret.

```sh
kubectl apply -k .
```

```yaml
apiVersion: ziti.alialjaffer.com/v1alpha1
kind: ZitiIdentity
metadata:
  name: alice
  namespace: team-a
spec:
  roleAttributes:
    - staff
  enrollmentMode: OperatorEnrolled
  secretName: alice-identity
```

Wait for the Secret. It holds `identity.json`, which contains the private key, so anyone who can read that Secret is this identity.

```sh
kubectl -n team-a get ztid alice      # ENROLLED True, CERT EXPIRES in the future
kubectl -n team-a get secret alice-identity
```

The workload runs as a non-root user with a read-only root filesystem, the same hardening the chart gives the manager.

The client Pod below mounts the Secret and proxies the Ziti service `team-a.web` on its own port 8080. The service name is the ZitiApp name in the form `<namespace>.<name>`.

```yaml
# The Pod stays Pending until the operator writes the identity.json key. That is the hand-over point.
apiVersion: v1
kind: Pod
metadata:
  name: alice-client
  namespace: team-a
spec:
  securityContext:
    runAsNonRoot: true
    seccompProfile:
      type: RuntimeDefault
  containers:
    - name: tunnel
      image: openziti/ziti-cli:2.0.4
      command:
        - ziti
        - tunnel
        - proxy
        - -i
        - /id/identity.json
        - team-a.web:8080
      securityContext:
        readOnlyRootFilesystem: true
        allowPrivilegeEscalation: false
        capabilities:
          drop:
            - ALL
      volumeMounts:
        - name: identity
          mountPath: /id
          readOnly: true
        - name: cache
          mountPath: /tmp
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
  volumes:
    - name: identity
      secret:
        secretName: alice-identity
    - name: cache
      emptyDir: {}
```

Dial the app from the sidecar. The Ziti name resolves only for identities the app allows.

```sh
kubectl -n team-a exec alice-client -c curl -- curl -s localhost:8080
```

`JwtOnly`, the default, writes `enrollment.jwt` instead and leaves the enrollment to another tool.
