# Log in with a certificate

A `ZitiCA` registers a cert-manager CA in Ziti, so workloads can log in with certificates that cert-manager issues and renews. No enrollment, no JWT, no identity file for the operator to keep.

Use a CA made only for Ziti. Anyone who holds its key can log in as any identity that matches a name.

```yaml
apiVersion: cert-manager.io/v1
kind: Issuer
metadata:
  name: ziti-bootstrap
  namespace: cert-manager
spec:
  selfSigned: {}
---
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: ziti-workload-ca
  namespace: cert-manager
spec:
  isCA: true
  commonName: ziti-workload-ca
  secretName: ziti-workload-ca
  privateKey:
    algorithm: ECDSA
    size: 256
  issuerRef:
    name: ziti-bootstrap
    kind: Issuer
---
apiVersion: cert-manager.io/v1
kind: ClusterIssuer
metadata:
  name: ziti-workload-ca
spec:
  ca:
    secretName: ziti-workload-ca
```

Register the CA. The operator signs Ziti's proof with the key from the Secret and keeps the key in memory.

```yaml
apiVersion: ziti.alialjaffer.com/v1alpha1
kind: ZitiCA
metadata:
  name: workloads
spec:
  certificate:
    secretRef:
      namespace: cert-manager
      name: ziti-workload-ca
  verification:
    signWithSecretKey: true
```

```sh
kubectl apply -k .
kubectl get ztca              # VERIFIED must be true
```

The identity matches a certificate by common name, so `externalId` and the common name must be the same string.

```yaml
apiVersion: ziti.alialjaffer.com/v1alpha1
kind: ZitiIdentity
metadata:
  name: web
  namespace: team-a
spec:
  enrollmentMode: None
  externalId: team-a.web
  authPolicy: Default
  roleAttributes:
    - web
  certificate:
    issuerRef:
      name: ziti-workload-ca
      kind: ClusterIssuer
```

The operator creates the workload `Certificate` for you. A free `externalId` needs `roleScope: Global` on the connection.

```sh
kubectl -n team-a get ztid web -o wide
```

Log in with the certificate. Your client must support certificate login with an external CA.

```sh
curl --cert tls.crt --key tls.key -X POST \
  "https://ziti-controller-mgmt.example.com/edge/client/v1/authenticate?method=cert"
```

A renewed certificate logs in again with no operator action, because only the name matters. See [cert-manager](../../docs/cert-manager.md).
