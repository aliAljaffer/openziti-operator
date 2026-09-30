# Use a cert-manager CA with Ziti

Workloads can log in to Ziti with certificates that cert-manager issues and renews. There is no enrollment step, no JWT, and no identity file that the operator must keep. A renewed certificate finds its identity again because Ziti matches the name in the certificate, not the certificate itself.

## How it works

1. A cert-manager CA Issuer keeps a CA certificate and key in a Secret (`tls.crt`, `tls.key`).
2. A `ZitiCA` registers that CA in Ziti. Ziti then asks for proof that you control it. The operator signs the proof with the CA key from the Secret. The key stays in memory. It is never stored, logged, or sent.
3. A `ZitiIdentity` with `enrollmentMode: None` and an `externalId` creates an identity. Ziti reads the common name of a client certificate and matches it to `externalId`.
4. cert-manager issues a client certificate whose common name is that `externalId`.
5. The workload sends the certificate to the Ziti client API (`/edge/client/v1/authenticate?method=cert`).

## Steps

1. Create a CA in cert-manager. Use a CA made only for Ziti, not the CA of the cluster.

   ```yaml
   apiVersion: cert-manager.io/v1
   kind: Issuer
   metadata: {name: ziti-bootstrap, namespace: cert-manager}
   spec: {selfSigned: {}}
   ---
   apiVersion: cert-manager.io/v1
   kind: Certificate
   metadata: {name: ziti-workload-ca, namespace: cert-manager}
   spec:
     isCA: true
     commonName: ziti-workload-ca
     secretName: ziti-workload-ca
     privateKey: {algorithm: ECDSA, size: 256}
     issuerRef: {name: ziti-bootstrap, kind: Issuer}
   ---
   apiVersion: cert-manager.io/v1
   kind: ClusterIssuer
   metadata: {name: ziti-workload-ca}
   spec: {ca: {secretName: ziti-workload-ca}}
   ```

2. Let the operator read the Secret. With the Helm chart, add the namespace to `rbac.secretNamespaces` (for example `cert-manager`). The default is cluster-wide access.

3. Register the CA.

   ```yaml
   apiVersion: ziti.alialjaffer.com/v1alpha1
   kind: ZitiCA
   metadata: {name: workloads}
   spec:
     certificate:
       secretRef: {namespace: cert-manager, name: ziti-workload-ca}
     verification:
       signWithSecretKey: true
   ```

   `kubectl get ztca` must show `VERIFIED` `true`.

4. Create the identity. A free `externalId` needs `roleScope: Global` on the connection. With `certificate`, the operator also creates the cert-manager Certificate (step 5) for you. The Secret is `secretName`, default the name of the identity.

   ```yaml
   apiVersion: ziti.alialjaffer.com/v1alpha1
   kind: ZitiIdentity
   metadata: {name: web, namespace: team-a}
   spec:
     enrollmentMode: None
     externalId: team-a.web
     authPolicy: Default
     roleAttributes: [web]
     certificate:
       issuerRef: {name: ziti-workload-ca, kind: ClusterIssuer}
   ```

   `kubectl get ztid web` shows `Ready` `true` when cert-manager has issued the certificate. Skip step 5 in this case. Use `duration` to set the lifetime. The operator needs no access to the Secret, because cert-manager writes it.

5. Or issue the workload certificate yourself, with the same name.

   ```yaml
   apiVersion: cert-manager.io/v1
   kind: Certificate
   metadata: {name: ziti-web, namespace: team-a}
   spec:
     commonName: team-a.web
     secretName: ziti-web-cert
     usages: [client auth]
     privateKey: {algorithm: ECDSA, size: 256}
     issuerRef: {name: ziti-workload-ca, kind: ClusterIssuer}
   ```

6. The workload logs in with the certificate from `ziti-web-cert`.

   ```sh
   curl --cert tls.crt --key tls.key -X POST "https://<controller>/edge/client/v1/authenticate?method=cert"
   ```

## Verify through cert-manager (no CA key access)

Use `issuerRef` when the operator must not read the CA key. The operator asks the cert-manager issuer for a short-lived certificate whose common name is Ziti's verification token. It reads that certificate from a Secret and sends it to Ziti. The CA key stays with cert-manager. The proof Certificate and its Secret are removed once Ziti has verified the CA.

```yaml
apiVersion: ziti.alialjaffer.com/v1alpha1
kind: ZitiCA
metadata: {name: workloads}
spec:
  certificate:
    secretRef: {namespace: cert-manager, name: ziti-workload-ca}
  verification:
    issuerRef: {name: ziti-workload-ca, kind: ClusterIssuer, namespace: cert-manager}
```

- `issuerRef.name` must be the issuer whose CA certificate this `ZitiCA` registers.
- `issuerRef.namespace` is where the proof Certificate and its Secret live. With `rbac.secretNamespaces`, add that namespace to the list.
- Set only one of `signWithSecretKey` and `issuerRef`.
- The operator still reads the CA certificate (`tls.crt`) from the Secret of `certificate.secretRef`. It does not use `tls.key`.
- If the CA certificate changes, the operator replaces the CA in Ziti and verifies it again the same way.

## Verify by hand instead

Leave out `signWithSecretKey` if the operator must not read the CA key. The status then shows a token.

```sh
kubectl get ztca workloads -o jsonpath='{.status.verificationToken}'
```

Sign a certificate with that common name using the CA key, and send it as plain text to `POST /cas/<id>/verify` on the Edge Management API. `kubectl get ztca workloads -o jsonpath='{.status.caId}'` gives the ID. `Verified` turns true at the next check.

## Renewal

- Workload certificates: cert-manager renews them. Nothing else happens, because the common name stays the same.
- CA certificate: Ziti cannot change the certificate of a CA. When the certificate in the Secret changes, the operator finds it within about two minutes, deletes the CA in Ziti, creates it again, and verifies it again. `signWithSecretKey` makes this automatic. Without it, the CA is `Ready=False` until you verify it by hand. Identities matched by `externalId` keep working.

## Options

| Field | Meaning |
|---|---|
| `certificate.certKey` | Key of the certificate in the Secret. Default `tls.crt`. The first certificate is used. |
| `authEnabled` | Identities may log in with certificates of this CA. Default true. |
| `externalIdClaim` | Which certificate field names the identity: `COMMON_NAME` (default), `SAN_URI`, or `SAN_EMAIL`. With a matcher (`ALL`, `PREFIX`, `SUFFIX`, `SCHEME`) and a parser (`NONE`, `SPLIT`) to cut the value. |
| `autoEnrollment` | Create an identity the first time a certificate of the CA logs in, with fixed role attributes. Leave it out to create identities yourself. |

## Security

- Anyone who holds the CA key can log in as any identity that matches a name. Use a CA made only for Ziti.
- `signWithSecretKey` gives the operator read access to that key. Limit it with `rbac.secretNamespaces`, or verify by hand.
- `autoEnrollment` turns every certificate of the CA into an identity with the configured roles. Use it only for a CA that you fully control.
- `ZitiCA` is cluster-scoped. Only cluster admins can create it.

## Limits

- Tested with cert-manager in a cluster: the operator registers and verifies the CA. A certificate that cert-manager issued logs in as the identity, and a request without a certificate is rejected. A reissued workload certificate (new key) logs in with no operator action. After cert-manager rotates the CA, the operator replaces the CA in Ziti, verifies it again, and a certificate from the new CA logs in.
- Not tested: a Ziti tunneler or SDK that uses the certificate and key directly. Check that your client supports certificate login with an external CA.
- The operator creates only the workload `Certificate` (with `certificate` on the identity). It does not create the issuer or the CA. cert-manager must be installed, or the identity shows the reason `CertManagerMissing`.
- Tested with real cert-manager on a cluster: the Certificate that the operator builds is accepted and issued. The full chain (operator, `ZitiCA`, login with the issued certificate) was tested by hand with the certificate from step 5.
