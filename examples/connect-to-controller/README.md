# Connect to a Ziti controller

Every other example starts here. A `ZitiConnection` tells the operator where the Edge Management API is, how to log in, and which routers the resources in your namespaces may name.

The operator trusts only the certificates in `caBundle`. It is required, and the ConfigMap key must hold PEM.

## Get the CA bundle

Ask the controller for its CA list. The `ziti` CLI has no command that reads one from a running controller, and `ziti pki create ca` makes a new one instead.

```sh
host=ziti-controller-mgmt.example.com
(echo "-----BEGIN PKCS7-----"; curl -sk "https://$host/.well-known/est/cacerts" | fold -w64; echo; echo "-----END PKCS7-----") > cacerts.p7
openssl pkcs7 -print_certs -in cacerts.p7 > ca.pem
```

Or read it from cert-manager, when it issued the controller certificate. The issuer Secret keeps the CA certificate in `tls.crt`.

```sh
kubectl -n ziti get secret ziti-ca-cert -o jsonpath='{.data.tls\.crt}' | base64 -d > ca.pem
```

Store it and the credentials where the operator can read them.

```sh
kubectl create namespace ziti-operator-system
kubectl -n ziti-operator-system create configmap ziti-root-ca --from-file=ca.crt=ca.pem
kubectl -n ziti-operator-system create secret generic ziti-operator-credential \
  --from-literal=username=<user> --from-literal=password=<password>
```

The bundle needs the root, not only the intermediate the controller serves. [CA bundle](../../docs/ca-bundle.md) has the check. With `trustManager.enabled=true`, a trust-manager Bundle keeps the ConfigMap in sync on rotation.

## Log in

```yaml
apiVersion: ziti.alialjaffer.com/v1alpha1
kind: ZitiConnection
metadata:
  name: default
spec:
  managementUrl: https://ziti-controller-mgmt.example.com/edge/management/v1
  caBundle:
    configMapRef:
      namespace: ziti-operator-system
      name: ziti-root-ca
      key: ca.crt
  auth:
    updb:
      secretRef:
        namespace: ziti-operator-system
        name: ziti-operator-credential
  roleScope: Namespaced
  hostingRouters:
    - edge-1
  entryRouters:
    - edge-1
```

The Secret holds `username` and `password`. Certificate login reads `tls.crt` and `tls.key` instead, which cert-manager can write for you. Set one of `updb` or `cert`, never both. The certificate file is the alternative, not part of the kustomization.

```yaml
# Certificate login instead of username and password. Apply on its own, not with the file above.
apiVersion: ziti.alialjaffer.com/v1alpha1
kind: ZitiConnection
metadata:
  name: default
spec:
  managementUrl: https://ziti-controller-mgmt.example.com/edge/management/v1
  caBundle:
    configMapRef:
      namespace: ziti-operator-system
      name: ziti-root-ca
      key: ca.crt
  auth:
    cert:
      secretRef:
        namespace: ziti-operator-system
        name: ziti-operator-cert
  roleScope: Namespaced
  hostingRouters:
    - edge-1
  entryRouters:
    - edge-1
```

## Apply

```sh
kubectl apply -k .
kubectl get ztconn        # CONNECTED must show True
```

`kubectl explain ziticonnection.spec` lists every field.
