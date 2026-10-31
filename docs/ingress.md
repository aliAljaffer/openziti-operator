# Expose an Ingress

Annotate an HTTP Ingress with `ziti.alialjaffer.com/expose: "true"` and the operator creates a `ZitiApp` named `ingress-<ingress-name>` in the same namespace.

```yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: billing
  namespace: team-a
  annotations:
    ziti.alialjaffer.com/expose: "true"
    ziti.alialjaffer.com/allow-groups: finance
spec:
  rules:
    - host: billing.example.com
      http:
        paths:
          - path: /
            pathType: Prefix
            backend:
              service:
                name: billing
                port:
                  number: 80
```

The ZitiApp intercepts the Ingress hosts on port 80 and forwards to the backend Service. It uses the same annotations as an annotated Service: `connection`, `name`, `addresses`, `ports`, `protocols`, `allow-groups`, `allow-identities`, `posture-check-roles`, `member-of`, `entry-routers`, and `hosted-by`.

For a TLS Ingress, publish through the ingress controller instead of the backend Service:

```yaml
metadata:
  annotations:
    ziti.alialjaffer.com/expose: "true"
    ziti.alialjaffer.com/via-ingress: "true"
    ziti.alialjaffer.com/ingress-backend: ingress-nginx/ingress-nginx-controller:443
```

The target Service needs `ziti.alialjaffer.com/allow-expose=true` when it is in another namespace.

## Limits

- Every path must target the same Service and Service port unless `ziti.alialjaffer.com/via-ingress=true`
  is set. Ziti does not preserve Kubernetes path routing. Mixed backends would silently expose the
  wrong service, so the operator rejects them.
- TLS Ingresses are not exposed automatically. Set `ziti.alialjaffer.com/via-ingress=true` and
  `ziti.alialjaffer.com/ingress-backend=<namespace>/<service>:<port>` to publish through the ingress
  controller, which keeps TLS termination and path routing. The target Service needs
  `ziti.alialjaffer.com/allow-expose=true` when it is in another namespace. Use a `ZitiApp` when the
  backend serves TLS itself.
- The Ingress must have at least one host, or set `ziti.alialjaffer.com/addresses`.
- The operator owns the generated ZitiApp through an owner reference. Removing the annotation or deleting the Ingress deletes that app.
