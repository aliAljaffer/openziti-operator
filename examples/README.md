# Examples

Use cases for the operator. Each folder holds YAML files and a README that explains what the operator does with them and how to check the result.

## Before you start

Install the chart, apply [`connect-to-controller`](connect-to-controller) once, and create the namespace the other examples use.

```sh
kubectl create namespace team-a
```

Every folder has a `kustomization.yaml`, so one command applies a folder in order.

## Examples

| Folder | Use case |
|---|---|
| [connect-to-controller](connect-to-controller) | Reach the management API, log in with a password or a client certificate, and name the routers resources may use. |
| [publish-an-app](publish-an-app) | Turn an app in the cluster into a Ziti service with one `ZitiApp`. |
| [start-an-edge-router](start-an-edge-router) | Create a router in Ziti, get its enrollment JWT, and run it from the generated manifest. |
| [enroll-client-identity](enroll-client-identity) | Enroll a client, hand the identity file to a Pod through a Secret, and dial an app. |
| [service-account-token-login](service-account-token-login) | Log in with a projected service account token, with no enrollment. |
| [login-with-certificate](login-with-certificate) | Register a cert-manager CA and log in with a certificate it issues. |
| [group-based-access](group-based-access) | Grant a group of identities access to a group of services by role. |
| [isolate-teams-on-one-controller](isolate-teams-on-one-controller) | Share one Ziti network between two namespaces and keep them apart. |
| [full-control](full-control) | Replace a `ZitiApp` with one resource per Ziti object. |

Every README links to the doc that explains the use case in depth. The folder is the runnable part, the doc is the reference.

## House rules

- Kebab-case folder names, one use case each. `example.com` hosts and documentation address ranges only.
- Each README embeds the YAML of its own folder. Change the file and the README together.
- A README stays under 200 words, code excluded.
