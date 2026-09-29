---
name: ziti-operator-workflow
description: Use when you work on any code, chart, or docs in this OpenZiti Kubernetes operator repo. Gives the layout, the commands, the generated files, and the commit and safety rules. Read it before the first edit.
---

# Working in this repo

The operator drives the OpenZiti Edge Management API from Kubernetes resources. Kinds: `ZitiConnection` (cluster), `ZitiApp`, `ZitiIdentity`, `ZitiAccessPolicy` (namespaced). API group `alialjaffer.ziti`, version `v1alpha1`.

## Layout

| Path | Holds |
|---|---|
| `api/v1alpha1/*_types.go` | CRD types, markers, CEL rules |
| `internal/controller/` | Reconcilers, `entityset.go`, sweeper, metrics collector |
| `internal/desired/` | Pure builders: spec to Ziti entity bodies. `Matches` compares desired and actual |
| `internal/check/` | Pure graph checks. The same code feeds status conditions and `cmd/audit` |
| `internal/ziti/` | REST client, fake client, enrollment, certificate extend |
| `cmd/main.go`, `cmd/audit/` | Manager and the read-only audit command |
| `charts/chart/` | Helm chart. CRD copies come from `make chart-crds` |
| `docs/` | Guides. `docs/reference/` is generated |
| `hack/` | Chart tests, docs and wiki generators, hygiene check |

## Commands

| Task | Command |
|---|---|
| Regenerate CRDs, RBAC, deepcopy | `make manifests generate` |
| Copy CRDs into the chart | `make chart-crds` |
| Regenerate the reference docs | `make docs` |
| Unit and envtest tests | `make test` (about 40 seconds, run it in the background) |
| Chart checks | `hack/test-chart.sh` |
| Integration tests (real Ziti controller) | see the `run-e2e` skill |

## Rules

- Never edit generated files: `config/crd/bases/*`, `config/rbac/role.yaml`, `zz_generated.*`, `docs/reference/*`, `charts/chart/templates/crd/*`. Change the source and regenerate.
- Never remove `+kubebuilder:scaffold:*` markers. Scaffold new kinds with `kubebuilder create api`.
- The operator owns only entities that carry the tag `ziti-operator-uid`. Never update or delete anything else.
- No internal names, hosts, IDs, or addresses in code, tests, docs, or samples. Run `hack/check-hygiene.sh <private-patterns-file>` before you push.
- Never write to the live Ziti network. Use a local controller.
- Commit messages: one line, `git commit -s`, no co-author line. No em-dashes anywhere. Comments only for non-obvious reasons.
- `PLAN.md`, `ELEMENTARY_PLAN.md`, `PROGRESS.md`, `AGENTS.md`, `CURRENT_STATE.md` stay local. Update `PROGRESS.md` when you finish a step.
- Keep each tool call under 30 seconds. Run long commands in the background.
