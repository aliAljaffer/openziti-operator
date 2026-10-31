# ADR 0004: Backward-compatible roadmap and deferred breaking changes

- Status: Proposed
- Date: 2026-10-31

## Context

The operator is at `v0.3.x` with `v1alpha1`. Several high-value features can be added without
changing an existing field, default, or deletion behavior. The additive parts are implemented now.
This ADR records the cleanups that would break existing resources or installs, so a later session can
do them in a `v1alpha2` with conversion, or with an explicit migration release.

## Deferred breaking changes

### 1. One hosting router field

Current: `ZitiApp.spec.hostedBy` is a string, and `ZitiApp.spec.hostedByAnyOf` was added for
additional routers.

Future: replace both with `spec.hostingRouters []string` in `v1alpha2`, and convert `hostedBy` to a
one-element list. Keep `hostedByAnyOf` working through conversion.

### 2. Groups own membership

Current: `ZitiIdentity.spec.roleAttributes` remains the source of identity attributes. `ZitiGroup`
adds one reserved attribute (`group.<name>` by default) but does not replace manual attributes.

Future: make `ZitiGroup` the canonical membership source, deprecate group-like attributes in
`roleAttributes`, and migrate attributes that match a group name. This needs a conversion or a
one-time migration command because removing an attribute changes access.

### 3. Explicit auth policy only

Current: `ZitiJwtSigner` still creates an implicit auth policy of the same name for backward
compatibility.

Future: create only the signer, require a `ZitiAuthPolicy` that names it, and migrate existing
signers. This changes the default `createAuthPolicy: true` behavior.

### 4. Imported identities as the normal adoption path

Current: `ZitiIdentity.spec.importFrom` is opt-in. Adopting an enrolled identity without an identity
file still reports `IdentityFileUnavailable`.

Future: make an identity file or an explicit `enrollmentMode: None` mandatory for `Adopt`, and turn
the old behavior into a validation error. This is a behavior break for anyone relying on a
half-adopted identity.

### 5. Safer deletion default

Current: `deletionPolicy` defaults to `Delete`, and it is immutable.

Future: default to `Orphan`, or require an explicit policy for `Manage`, so deleting a GitOps
manifest cannot delete the Ziti entities. This changes the meaning of an omitted field. It needs a
migration note and probably a chart value for one release.

### 6. TLS Ingress default

Current: TLS Ingresses are rejected unless `ziti.alialjaffer.com/via-ingress=true` is set.

Future: infer `via-ingress` for TLS Ingresses, or default to the ingress controller Service. This
changes what an existing annotated Ingress publishes, so keep the explicit annotation for at least
one release.

### 7. Identity type and enrollment defaults

Current: `ZitiIdentity.spec.type` defaults to `Default`, and `enrollmentMode` defaults to `JwtOnly`.
Imported identities are generated as `Observe` with the default enrollment mode.

Future: infer the identity type and require an enrollment mode when importing. This changes the
manifest shape for imported identities.

### 8. v1alpha2 conversion webhook

The operator has one version and no conversion webhook. A future `v1alpha2` needs one for the field
renames above. Do not add it until there is a second served version.

## Compatibility rules for the additive work

- Existing fields keep their names, types, and defaults.
- New fields are optional.
- New kinds are additive. They are registered in the scheme, RBAC, chart CRDs, samples, and the
  orphan sweeper where they own a Ziti entity.
- `ZitiGroup` owns no Ziti entity and stays out of the orphan sweeper.
- Generated exports default to `managementPolicy: Observe`.
- No existing resource changes behavior unless it sets a new field.
