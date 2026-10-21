---
name: publish-an-app
description: Use when you publish an app through Ziti with a ZitiApp, annotate a Service or Ingress to expose it, route ports to different backends or TLS backends, or observe an existing service. For copying a hand-made service, use new-ziti-app.
---

# Publish an app

Details: `docs/first-app.md`, `docs/ingress.md`, `docs/tls.md`, `docs/existing-resources.md`, `docs/reference/ZitiApp.md`, `examples/publish-an-app`.

A `ZitiApp` creates the configs, the service, the Bind and Dial policies, and the router policy. It never touches entities it did not create.

## Pick one way

| Way | Use when |
|---|---|
| `ZitiApp` with `expose` + `targets` | Backend is an IP, a DNS name, or a Service by `kubernetesService`. |
| `ZitiApp` with `expose.selector` | Labeled Services in the same namespace define addresses, ports, and targets. Do not set `targets` with it. |
| Annotate a Service | Fastest path. The operator creates a `ZitiApp` the Service owns. |
| Annotate an HTTP Ingress | Same, for Ingress hosts. |

## Annotations (`ziti.alialjaffer.com/`)

`expose: "true"` turns it on. Optional: `connection`, `name`, `addresses`, `ports`, `protocols`, `allow-groups`, `allow-identities`, `member-of`, `entry-routers`, `hosted-by`.

Ingress limits: every path must go to one Service and port. TLS Ingresses are not exposed. At least one host, or set `addresses`. Removing the annotation deletes the generated `ZitiApp` (`ingress-<name>`).

## Ports and TLS

- `ZitiApp` carries TCP. It never terminates TLS. The backend serves TLS and its certificate must cover the dialed name.
- `targets[].exposedPorts` (client side) and `targets[].port` (backend side) map ports. One distinct `exposedPorts` set becomes one Ziti service, named `<zitiName>-<ports>` when there is more than one.
- Every exposed port needs exactly one target group. Else `InvalidSpec`.
- Use `protocols: [tcp]` when the backend is TCP only. A UDP intercept without a UDP host is captured and refused.

## Access

`allow.groups` and `allow.identities` set who may dial. `memberOf` sets the service role attributes. Groups follow role scope (`.claude/skills/access-and-scope/SKILL.md`). `entryRouters` and `hostedBy` must come from the connection lists.

## Existing services

`managementPolicy: Observe` with `zitiName` reads a hand-made service and writes nothing. Start there. An app cannot be adopted. Switching `Observe` to `Manage` on an existing name gives `NameConflict`.

## Check

```sh
kubectl -n <ns> get ztapp <name> -o wide
```

`HOSTED`, `DIALABLE`, `ROUTEPATH`, `READY` must be `True`. `HOSTED` can lag several minutes. Dial the service without `curl -k`. Failures: `.claude/skills/troubleshoot-resources/SKILL.md`, `docs/conditions.md`.
