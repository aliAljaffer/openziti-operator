# Authentication policies

A `ZitiAuthPolicy` maps one to one to a Ziti auth policy. It lets a team define how identities
authenticate instead of hand-creating the policy in Ziti.

```yaml
apiVersion: ziti.alialjaffer.com/v1alpha1
kind: ZitiAuthPolicy
metadata:
  name: humans
  namespace: team-a
spec:
  primary:
    cert:
      allowed: true
    updb:
      allowed: true
      minPasswordLength: 12
      maxAttempts: 5
      lockoutDurationMinutes: 15
      requireMixedCase: true
      requireNumberChar: true
      requireSpecialChar: true
    extJwt:
      allowed: false
  secondary:
    requireTotp: true
```

Reference it from an identity:

```yaml
spec:
  authPolicy: humans
```

The identity controller resolves a `ZitiAuthPolicy` in its namespace first, then falls back to a Ziti
auth policy with that name. The `zitiName` defaults to `<namespace>.<name>`, so two namespaces can use
the same Kubernetes name.

`primary.extJwt.allowedSigners` and `secondary.requireExtJwtSigner` are `ZitiJwtSigner` names. The
operator resolves them to signer IDs.

Defaults are conservative: certificate login is allowed, username/password and external JWT login
are off, and TOTP is not required. Set the fields explicitly.

## Disable an identity

```yaml
spec:
  disabled: true
  disabledUntil: "2026-11-01T12:00:00Z"
```

`disabled` rejects the identity's API session requests. Without `disabledUntil` it is indefinite.
With it, Ziti receives the remaining minutes, rounded up. The operator reports a `Disabled`
condition and never deletes the identity.

## Limits

- The operator can enforce an auth policy, but it does not enroll a TOTP secret, reset MFA, or set a
  UPDB password. Those remain client or admin actions.
- The implicit auth policy created by `ZitiJwtSigner` stays for backward compatibility. A
  `ZitiAuthPolicy` is the explicit, reusable form.
