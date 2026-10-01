# Get the CA bundle for the connection

The `ZitiConnection` needs the CA bundle of the Ziti controller. The operator uses it to trust the management API.

Store the bundle in a ConfigMap with the key `ca.crt`. The default name is `ziti-root-ca`, in the release namespace. See [installation](install.md).

## Extract the bundle from an existing network

1. Reach the controller. Use `kubectl port-forward` when the management API is inside a cluster.
   ```sh
   kubectl -n ziti port-forward svc/ziti-controller-mgmt 18443:443
   ```
2. Fetch the CA list that the controller publishes. Wrap it as PKCS7 and convert it to PEM.
   ```sh
   (echo "-----BEGIN PKCS7-----"; curl -sk https://localhost:18443/.well-known/est/cacerts | fold -w64; echo; echo "-----END PKCS7-----") > cacerts.p7
   openssl pkcs7 -print_certs -in cacerts.p7 > ca.pem
   ```
3. Check that the bundle trusts the controller. Pass the served intermediate certificate as untrusted.
   ```sh
   echo | openssl s_client -connect localhost:18443 -showcerts 2>/dev/null > chain.txt
   awk '/BEGIN CERT/{n++} n==1' chain.txt | awk '/BEGIN CERT/,/END CERT/' > leaf.pem
   awk '/BEGIN CERT/{n++} n==2' chain.txt | awk '/BEGIN CERT/,/END CERT/' > mid.pem
   openssl verify -CAfile ca.pem -untrusted mid.pem leaf.pem
   ```
   The result must be `leaf.pem: OK`.
4. Create the ConfigMap.
   ```sh
   kubectl -n ziti-operator-system create configmap ziti-root-ca --from-file=ca.crt=ca.pem
   ```

The controller serves its own certificate and an intermediate, but not the root. Do not copy the certificates from `s_client` alone. The bundle then lacks the root and the check fails.

Also check that the management URL host is in the certificate names (SAN). For a Service inside the cluster, use a name such as `ziti-controller-mgmt.ziti.svc`.

## Keep the bundle in sync with trust-manager

Set `trustManager.enabled=true` in the chart. The chart then creates a trust-manager `Bundle` that copies the CA from a Secret into the ConfigMap. This follows a CA rotation. See the chart values `trustManager.sources`.

## Create your own CA

Use this for a new Ziti network that has no PKI yet.

1. Create the CA with the `ziti` CLI.
   ```sh
   ziti pki create ca --pki-root ./pki --ca-file my-ca --ca-name "My Ziti CA"
   ```
   The public certificate is `pki/my-ca/certs/my-ca.cert`. The private key is `pki/my-ca/keys/my-ca.key`. Keep the key private.
2. Issue the controller server certificate and the other certificates from this CA. Configure the controller with them.
3. Use `pki/my-ca/certs/my-ca.cert` as the bundle.
   ```sh
   kubectl -n ziti-operator-system create configmap ziti-root-ca --from-file=ca.crt=pki/my-ca/certs/my-ca.cert
   ```

If you issue the certificates with cert-manager, use the `ca.crt` of the issuer Secret as the bundle. See [cert-manager CA](cert-manager.md) for the operator side of that setup.
