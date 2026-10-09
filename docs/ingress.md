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

The ZitiApp intercepts the Ingress hosts on port 80 and forwards to the backend Service. It uses the same annotations as an annotated Service: `connection`, `name`, `addresses`, `ports`, `protocols`, `allow-groups`, `allow-identities`, `member-of`, `entry-routers`, and `hosted-by`.

## Limits

- Every path must target the same Service and Service port. Ziti does not preserve Kubernetes path routing. Mixed backends would silently expose the wrong service, so the operator rejects them.
- TLS Ingresses are not exposed automatically. An Ingress controller may terminate TLS before forwarding to the backend; bypassing it changes that behavior. Use a `ZitiApp` to choose the TLS backend explicitly.
- The Ingress must have at least one host, or set `ziti.alialjaffer.com/addresses`.
- The operator owns the generated ZitiApp through an owner reference. Removing the annotation or deleting the Ingress deletes that app.
