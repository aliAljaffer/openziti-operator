# Edge routers

A `ZitiRouter` creates the edge router entity in Ziti and hands you its enrollment JWT in a Secret. It does not install or run the router. The router install is still yours (a Helm chart, a Deployment, a VM). This removes the manual step of creating the router in Ziti and copying its JWT.

`ZitiRouter` is cluster-scoped, because a router is network infrastructure. Only cluster admins can create one.

## Create a router

```yaml
apiVersion: alialjaffer.ziti/v1alpha1
kind: ZitiRouter
metadata: {name: edge-1}
spec:
  roleAttributes: [edge]
  tunnelerEnabled: true
  enrollmentSecretRef: {namespace: routers, name: edge-1-enrollment}
```

`kubectl get ztrouter` shows `ENROLLED`, `ONLINE`, and when the JWT expires.

## Give the JWT to the router

```sh
kubectl -n routers get secret edge-1-enrollment -o jsonpath='{.data.enrollment\.jwt}' | base64 -d
```

Pass it to the router installer. The router enrolls with it once.

| State | What the operator does |
|---|---|
| Not enrolled, JWT valid | Keeps the JWT in the Secret. |
| Not enrolled, JWT expired | Asks Ziti for a new JWT and updates the Secret. Event `EnrollmentRenewed`. |
| Enrolled | Removes the JWT from the Secret. |

Without `enrollmentSecretRef` the JWT stays in Ziti and the operator writes no Secret. With the Helm chart, the namespace of the Secret must be in `rbac.secretNamespaces` when that list is set.

## Keys

| Key | Meaning |
|---|---|
| `zitiName` | Router name in Ziti. Default: the resource name. Cannot change later. |
| `roleAttributes` | Groups of the router. Edge router policies select routers by them. Used as written, in every role scope. |
| `tunnelerEnabled` | The router can host and dial services. A router that hosts a `ZitiApp` needs it. |
| `cost`, `noTraversal`, `disabled` | Ziti router settings. |
| `enrollmentSecretRef` | Where the enrollment JWT goes. |
| `deletionPolicy` | `Delete` (default) removes the router from Ziti with the resource. `Orphan` only removes the operator tags. |

## Conditions

| Condition | True when |
|---|---|
| `Synced` | The router exists in Ziti and matches the spec. |
| `Enrolled` | The router has enrolled. |
| `Online` | The router is connected to the controller. |
| `Ready` | `Enrolled` and `Online`. |

## Use the router in a ZitiApp

A `ZitiApp` names routers in `hostedBy` and `entryRouters`, and the connection lists them in `hostingRouters`. Create the `ZitiRouter` first, add its `zitiName` to `hostingRouters`, and reference it from the app.

## Limits

- Tested: the operator creates the router, delivers the JWT Ziti issued, keeps the pending enrollment across updates, and issues a new JWT with re-enroll. It has not enrolled a real router with that JWT, so the Enrolled and Online transitions are tested against a fake controller only.
- A live router that this operator created is deleted from Ziti when you delete its resource (or, with the orphan sweeper in `delete` mode, when the resource is gone). Use `deletionPolicy: Orphan` for routers that must survive.
