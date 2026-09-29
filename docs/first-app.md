# First app

## Publish an app

1. Apply this file.

   ```yaml
   apiVersion: alialjaffer.ziti/v1alpha1
   kind: ZitiApp
   metadata: {name: billing, namespace: team-a}
   spec:
     expose:
       addresses: [billing.example.com]
       ports: [443, "8000-8005"]
     targets:
       - address: 10.0.0.5
       - kubernetesService: billing
         port: 8080
         cost: 10
     allow:
       groups: [team-a]
       identities: [alice]
     entryRouters: [edge-1]
   ```

2. Check the result.

   ```sh
   kubectl -n team-a get ztapp billing -o wide
   ```

Field help is built in: `kubectl explain ztapp.spec.targets`. The full field list is in the [ZitiApp reference](reference/ZitiApp.md).

## Keys

| Key | Meaning |
|---|---|
| `zitiName` | Service name in Ziti. Default `<namespace>.<name>`. Cannot change later. |
| `expose.addresses` | What clients dial: hostnames, IPs, or CIDRs. |
| `expose.ports` | Numbers or `"low-high"` ranges. |
| `expose.protocols` | `tcp` (default) and `udp`. |
| `targets` | Where the app runs. Each target is one Ziti terminator. |
| `targets[].address` or `kubernetesService` | Set exactly one. |
| `targets[].port` | Port on the target. Without it, the port the client dialed is used. |
| `targets[].cost` | Lower cost is preferred. Equal costs share the load. |
| `allow.groups` | Identities with one of these role attributes may connect. |
| `allow.identities` | Ziti identity names. They may exist outside Kubernetes. |
| `entryRouters` | Routers clients connect through. Creates an edge router policy for the allowed identities. |
| `memberOf` | Groups the app belongs to. Existing policies that grant a group apply to it. |
| `hostedBy` | Router that runs the targets. Must be in `hostingRouters`. |
| `deletionPolicy` | `Delete` (default) or `Orphan`. Cannot change later. |

## Conditions

| Condition | True when |
|---|---|
| `Synced` | Every Ziti entity exists and matches. |
| `Hosted` | At least one terminator exists. |
| `Dialable` | A Dial policy selects at least one identity. |
| `RoutePath` | A client identity has an online router that the app may use. |
| `AccessResolved` | Every name in `allow.identities` exists in Ziti. |
| `Ready` | `Hosted`, `Dialable`, and `RoutePath` are all true. |

`Dialable` and `RoutePath` can be false before the first client exists.

## Expose a Kubernetes Service

Add annotations to the Service. The operator creates a `ZitiApp` that the Service owns.

| Annotation | Meaning |
|---|---|
| `alialjaffer.ziti/expose` | `"true"` to publish. Required. |
| `alialjaffer.ziti/allow-groups`, `allow-identities` | Who may connect. Comma separated. |
| `alialjaffer.ziti/addresses` | Default `<service>.<namespace>.svc`. |
| `alialjaffer.ziti/ports` | Default: the Service ports. Ranges allowed. |
| `alialjaffer.ziti/protocols` | Default: from the Service ports. |
| `alialjaffer.ziti/name`, `connection`, `member-of`, `entry-routers`, `hosted-by` | Same as the `ZitiApp` keys. |

Deleting the Service deletes the app and every Ziti entity it created.
