# Log in with a service account token

A workload can log in to Ziti with its Kubernetes service account token. There is no enrollment, no JWT to hand over, and no identity file.

## How it works

1. A `ZitiJwtSigner` tells Ziti to trust tokens from your cluster. The operator reads the cluster issuer and signing keys and creates the Ziti signer and an auth policy.
2. A `ZitiIdentity` with `enrollmentMode: None` and `serviceAccount: <name>` creates an identity that matches the token subject `system:serviceaccount:<namespace>:<name>`.
3. The workload sends its token to the Ziti client API. Ziti checks the signature, issuer, and audience, then finds the identity.

## Steps

1. Create the signer. It is cluster-scoped, so only cluster admins can create it.

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

   `kubectl get ztjwt` shows the issuer and the key id. The auth policy has the same name as the signer.

2. Create an identity for a service account in the same namespace.

   ```yaml
   apiVersion: ziti.alialjaffer.com/v1alpha1
   kind: ZitiIdentity
   metadata:
     name: web
     namespace: team-a
   spec:
     enrollmentMode: None
     serviceAccount: web
     authPolicy: k8s
     roleAttributes:
       - web
   ```

   `Ready` shows reason `TokenLogin`. No Secret is created.

3. Give the pod a token with the audience `ziti`.

   ```yaml
   spec:
     serviceAccountName: web
     volumes:
       - name: ziti-token
         projected:
           sources:
             - serviceAccountToken:
                 audience: ziti
                 expirationSeconds: 3600
                 path: token
   ```

4. The workload logs in with the token.

   ```sh
   curl -X POST "https://<controller>/edge/client/v1/authenticate?method=ext-jwt" \
     -H "Authorization: Bearer $(cat /var/run/secrets/ziti/token)"
   ```

## Keys and rotation

| `keys` | Behavior |
|---|---|
| `kubernetes: {}` | The operator reads the cluster issuer and keys. Ziti takes one key per signer. The operator picks the key that signs its own token. When Kubernetes rotates the key, the next sync (about 10 minutes) updates the signer. Tokens signed by the new key fail until then. |
| `jwksEndpoint: {url, issuer}` | The Ziti controller fetches the keys itself and follows rotation at once. The controller must reach the URL. Use it for other identity providers, or when your API server publishes its JWKS. |

With `kubernetes: {}`, a cluster that publishes several keys and an operator that runs outside the cluster (so it has no token to compare) gives `KeyAmbiguous`. The operator then creates nothing.

## Rules

- Ziti allows one signer per issuer and one per key. Two `ZitiJwtSigner` resources for the same cluster conflict.
- Use an audience made for Ziti, for example `ziti`. Every token from the issuer with that audience is trusted.
- `serviceAccount` is safe in every role scope. A raw `externalId` can name any subject, so it needs `roleScope: Global` on the connection.
- `enrollmentMode: None` needs `authPolicy` and either `serviceAccount` or `externalId`. The auth policy must allow the signer. The one the operator creates does.

## Limits

- Tested: the operator creates the signer, the policy, and the identity, and the Ziti client API accepts a valid token and rejects a wrong audience and another service account.
- Not tested: a Ziti tunneler or SDK that sends the token. Check that your client supports external JWT login.
