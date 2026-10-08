#!/usr/bin/env bash
# Copies the generated CRDs and the manager RBAC rules into the Helm chart. Run after `make manifests`.
# hack/test-chart.sh fails when the copies differ.
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"

out=charts/chart/templates/crd
mkdir -p "$out"
for crd in config/crd/bases/*.yaml; do
  name=$(yq .metadata.name "$crd")
  {
    echo '{{- if .Values.crd.enabled }}'
    awk '
      NR == 1 && $0 == "---" { next }
      { print }
      $0 == "  annotations:" {
        print "    {{- if .Values.crd.keep }}"
        print "    \"helm.sh/resource-policy\": keep"
        print "    {{- end }}"
      }
    ' "$crd"
    echo '{{- end }}'
  } >"$out/$name.yaml"
done

# The manager role keeps the rules of config/rbac/role.yaml. With rbac.secretNamespaces set, or with
# rbac.clusterWideSecrets turned off, secrets and configmaps move to per-namespace Roles
# (templates/rbac/secret-roles.yaml). The ziti-operator.limitSecrets helper covers both.
#
# controller-gen merges rules that share an apiGroup and a verb set, so secrets can land in the same rule as
# services or persistentvolumeclaims. Guard the resource line, not the rule, or the ClusterRole would keep Secret
# access in the mode that exists to remove it. A rule made only of guarded resources is dropped whole.
python3 - <<'PY'
import re

GUARDED = ("secrets", "configmaps")
GUARD = '{{- if not (include "ziti-operator.limitSecrets" .) }}'

src = open("config/rbac/role.yaml").read()
rules = src.split("\nrules:\n", 1)[1].strip("\n").split("\n")
blocks, cur = [], []
for line in rules:
    if line.startswith("- ") and cur:
        blocks.append(cur)
        cur = []
    cur.append(line)
blocks.append(cur)

def resources(b):
    """Returns (start, end, names) of the resources list of one rule block."""
    start = next((i for i, l in enumerate(b) if l == "  resources:"), None)
    if start is None:
        return None
    end = start + 1
    while end < len(b) and b[end].startswith("  - "):
        end += 1
    return start, end, [l[4:] for l in b[start + 1:end]]

out = []
for b in blocks:
    found = resources(b)
    names = found[2] if found else []
    if not found or not any(n in GUARDED for n in names):
        out.append("\n".join(b))
    elif all(n in GUARDED for n in names):
        out.append(GUARD + "\n" + "\n".join(b) + "\n{{- end }}")
    else:
        start, end, _ = found
        lines = []
        for n in names:
            if n in GUARDED:
                lines += [GUARD, "  - " + n, "{{- end }}"]
            else:
                lines.append("  - " + n)
        out.append("\n".join(b[:start + 1] + lines + b[end:]))

path = "charts/chart/templates/rbac/manager-role.yaml"
tpl = open(path).read()
head = tpl.split("\nrules:\n", 1)[0]
open(path, "w").write(head + "\nrules:\n" + "\n".join(out) + "\n")
PY
