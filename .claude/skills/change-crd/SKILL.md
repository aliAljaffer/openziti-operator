---
name: change-crd
description: Use when you add a kind, add or change a spec or status field, change validation, printer columns, or defaults in this operator. Lists every file to touch, the tests to add, and the traps found so far.
---

# Change an API type

## Steps

1. Edit `api/v1alpha1/<kind>_types.go`. Give every field a doc comment. `kubectl explain` and the reference docs use it.
2. New kind: run `kubebuilder create api --group alialjaffer --version v1alpha1 --kind <Kind> --resource --controller --namespaced`. Delete the stub test it makes.
3. Add markers: `+kubebuilder:default`, `Enum`, `Minimum`, `MaxLength`, printer columns, `resource:shortName=...,categories=ziti`.
4. Put mapping logic in `internal/desired/` as a pure function with a table test. Keep the reconciler thin.
5. Reconcile with `entitySet` (create, update, prune, release) for entities the CR owns.
6. Set conditions with `setCond` and `markFailed`. Use a `specError{reason, message}` for a spec problem. It sets the status and retries slowly.
7. Run `make manifests generate chart-crds docs`. Commit the generated files.
8. Add a sample to `config/samples/` and validate it: `kubectl apply --dry-run=server -k config/samples`.

## Tests to add

- Reconciler test with `ziti.Fake` and the fake Kubernetes client: create, second reconcile writes nothing, update, delete, name conflict, `Orphan`.
- Envtest specs in `internal/controller/crd_validation_test.go`: every CEL rule and default.
- Integration test (tag `integration`) that runs the reconciler against a real controller and asserts zero writes on the second reconcile. The fake hides real response shapes.

## Traps

- CEL cost budget: a rule on an int-or-string item is too costly without a string length cap, and `controller-gen` cannot set `maxLength` on it. Validate ranges in Go and report `InvalidSpec`.
- An empty desired list never equals a missing list in `Matches`. `desired.Matches` treats them as equal.
- Ziti returns some fields in another shape than you send (identity `type` is an object, `certPem` gets an extra newline). Compare a trimmed copy. Otherwise the operator writes on every resync.
- `PUT` replaces the entity. A `PATCH` on `tags` replaces the whole tag map. Merge tags by hand.
- Optional struct fields need `omitzero` in the JSON tag, or the API server sees an empty object and fails required rules.
- `deletionPolicy`, `zitiName`, and `enrollmentMode` are immutable by CEL. Keep new identity-defining fields immutable too.
- Keep secrets out of status, events, logs, and metrics. Identity reads strip `enrollment.*.jwt` and `.token` in `internal/ziti`.
