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
   apiVersion: alialjaffer.ziti/v1alpha1
   kind: ZitiCA
   metadata: {name: workloads}
   spec:
     certificate:
       secretRef: {namespace: cert-manager, name: ziti-workload-ca}
     verification:
       signWithSecretKey: true
   ```

   `kubectl get ztca` must show `VERIFIED` `true`.

4. Create the identity. A free `externalId` needs `roleScope: Global` on the connection.

   ```yaml
   apiVersion: alialjaffer.ziti/v1alpha1
   kind: ZitiIdentity
   metadata: {name: web, namespace: team-a}
   spec:
     enrollmentMode: None
     externalId: team-a.web
     authPolicy: Default
     roleAttributes: [web]
   ```

5. Issue the workload certificate with the same name.

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

## Verify by hand instead

Leave out `signWithSecretKey` if the operator must not read the CA key. The status then shows a token.

```sh
kubectl get ztca workloads -o jsonpath='{.status.verificationToken}'
```

Sign a certificate with that common name using the CA key, and send it as plain text to `POST /cas/<id>/verify` on the Edge Management API. `kubectl get ztca workloads -o jsonpath='{.status.caId}'` gives the ID. `Verified` turns true at the next check.

## Renewal

- Workload certificates: cert-manager renews them. Nothing else happens, because the common name stays the same.
- CA certificate: Ziti cannot change the certificate of a CA. When the certificate in the Secret changes, the operator deletes the CA in Ziti, creates it again, and verifies it again. `signWithSecretKey` makes this automatic. Without it, the CA is `Ready=False` until you verify it by hand. Identities matched by `externalId` keep working.

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

- Tested: the operator registers and verifies the CA, and the Ziti client API accepts a certificate of the CA for the matching identity and rejects a name without an identity. A fresh certificate with the same name logs in each time.
- Not tested: a Ziti tunneler or SDK that uses the certificate and key directly. Check that your client supports certificate login with an external CA.
- The operator does not create the cert-manager objects for you.
