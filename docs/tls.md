# Serve plain and TLS ports from one ZitiApp

A `ZitiApp` carries TCP. It does not start or end TLS. The backend must serve HTTPS itself, and its certificate must cover the name clients dial.

By default every exposed port goes to one backend port. To send some exposed ports to a different backend port or address, set `exposedPorts` on each target. The operator then creates one Ziti service per distinct `exposedPorts` set, so one `ZitiApp` can publish a plain port and a TLS port to different backends.

## `exposedPorts` compared with `port`

The two keys are the two sides of a port mapping, like `docker run -p exposedPorts:port`.

| Key | Side | Meaning |
|---|---|---|
| `exposedPorts` | Client | The ports clients dial. A subset of `expose.ports`. |
| `port` | Backend | Where the app is actually served, the port the router dials. |

- `exposedPorts` is where you expose it. Clients dial these ports.
- `port` is where it is served. One number per target.
- Omit `port` and the router dials the port the client dialed.
- Omit `exposedPorts` and the target is exposed on every port in `expose.ports`.

A target is one mapping. To map two client ports to two backends, use two targets.

## Example 1: one backend for every port

Here 80 and 443 both reach backend port 8080. One target, one service.

```yaml
apiVersion: ziti.alialjaffer.com/v1alpha1
kind: ZitiApp
metadata:
  name: web
  namespace: apps
spec:
  zitiName: web.example.com
  expose:
    addresses:
      - web.example.com
    ports:
      - 80
      - 443
    protocols:
      - tcp
  hostedBy: my-router
  targets:
    - address: 10.0.0.10
      port: 8080
  allow:
    groups:
      - admins
```

| Client dials | Backend | Ziti service |
|---|---|---|
| `http://web.example.com` (port 80) | 8080 | `web.example.com` |
| `https://web.example.com` (port 443) | 8080 | `web.example.com` |

## Example 2: a different backend per port

Here 80 reaches backend 8080, and 443 reaches backend 8443. Each target declares the client port it is exposed on. Two groups become two services.

```yaml
apiVersion: ziti.alialjaffer.com/v1alpha1
kind: ZitiApp
metadata:
  name: web
  namespace: apps
spec:
  zitiName: web.example.com
  expose:
    addresses:
      - web.example.com
    ports:
      - 80
      - 443
    protocols:
      - tcp
  hostedBy: my-router
  targets:
    - address: 10.0.0.10
      port: 8080
      exposedPorts:
      - 80
    - address: 10.0.0.10
      port: 8443
      exposedPorts:
      - 443
  allow:
    groups:
      - admins
```

| Client dials | Backend | Ziti service |
|---|---|---|
| `http://web.example.com` (port 80) | 8080 | `web.example.com-80` |
| `https://web.example.com` (port 443) | 8443 | `web.example.com-443` |

## How targets group

- `expose.ports` lists every port clients may dial.
- Each target lists the client ports it is exposed on in `exposedPorts`.
- A target without `exposedPorts` is exposed on every port in `expose.ports`.
- Targets with the same `exposedPorts` set become one Ziti service with one terminator each.
- Every exposed port must be served by exactly one group. The operator reports `InvalidSpec` on an overlap or a port no target serves.

With a single group the service keeps the plain `zitiName`. With more than one group the operator adds the port set to the name, as in `web.example.com-443`. The status lists every service under `status.services`, and the first service is repeated in `status.zitiName`, `status.ids`, and `status.terminators`.

Keep the `exposedPorts` sets disjoint. Two groups must not share an address and port.

## Certificate

The client checks the backend certificate against the name it dialed. Use a certificate for `web.example.com`. Issue it from a CA that the client trusts, for example with a cert-manager issuer and an ACME DNS-01 challenge. HTTP-01 does not work, because the name resolves to a tunnel address on the client.

TLS passes through the tunnel byte for byte. The operator does not terminate or re-encrypt it.

## Check

```sh
kubectl -n apps get ztapp web -o jsonpath='{.status.services[*].name}'
curl -v https://web.example.com
```

Every service must show `HOSTED`, `DIALABLE`, `ROUTEPATH`, and `READY` as `True`. The `HOSTED` condition can take several minutes to change after the router registers the service. Dial the port to see the real state. If it fails, read [Conditions and reasons](conditions.md).

Do not use `curl -k` as proof. It skips the certificate check.
