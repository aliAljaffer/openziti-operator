---
name: release-checklist
description: Use when you prepare a release or a public push of this operator. Lists the checks, the files, and the steps that need the repository owner.
---

# Release checklist

## Before you tag

1. `make test` passes. Integration tests pass against a fresh `openziti/ziti-cli` controller.
2. `hack/test-chart.sh` passes (chart values, failure messages, CRD sync). `helm lint charts/chart` is clean.
3. `make docs-check` passes. `hack/gen-wiki.py <dir>` builds with no broken links.
4. `hack/check-hygiene.sh <private-patterns-file>` finds nothing. Never put the patterns file in the repo.
5. Run the kind e2e in the `run-e2e` skill, including the client-pod dial. Run `make test-e2e` too.
6. `docs/compatibility.md` names the controller and Kubernetes versions you tested.
7. `make build-installer IMG=<registry>/ziti-operator:<tag>` builds. Do not commit `dist/` until the release.

## Needs the repository owner

- Repository secret `HYGIENE_PATTERNS` (internal names, one regex per line).
- GitHub private vulnerability reporting (`SECURITY.md` points to it).
- The wiki enabled and one page created by hand, so the wiki repository exists. Then `hack/publish-wiki.sh --push` or the `wiki.yml` workflow updates it.
- A registry, image build and push, and the `v0.1.0` tag.
- Confirm the module path `github.com/aliAljaffer/openziti-operator` and the API group `ziti.alialjaffer.com`.

## Never

- Do not push, tag, or publish without the owner asking.
- Do not include `CURRENT_STATE.md` or `*.private.yaml`. The hygiene check fails on them.
