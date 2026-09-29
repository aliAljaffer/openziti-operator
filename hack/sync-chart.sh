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

# The manager role keeps the rules of config/rbac/role.yaml. With rbac.secretNamespaces set, the Secret rule and the
# ConfigMap entry move to per-namespace Roles (templates/rbac/secret-roles.yaml).
python3 - <<'PY'
import re
src = open("config/rbac/role.yaml").read()
rules = src.split("\nrules:\n", 1)[1].strip("\n").split("\n")
blocks, cur = [], []
for line in rules:
    if line.startswith("- ") and cur:
        blocks.append(cur)
        cur = []
    cur.append(line)
blocks.append(cur)

out = []
for b in blocks:
    text = "\n".join(b)
    only_secrets = re.search(r"^  resources:\n  - secrets\n  verbs:", text, re.M)
    if only_secrets:
        out.append("{{- if not .Values.rbac.secretNamespaces }}\n" + text + "\n{{- end }}")
        continue
    text = text.replace("  - configmaps\n", "  {{- if not .Values.rbac.secretNamespaces }}\n  - configmaps\n  {{- end }}\n")
    out.append(text)

path = "charts/chart/templates/rbac/manager-role.yaml"
tpl = open(path).read()
head = tpl.split("\nrules:\n", 1)[0]
open(path, "w").write(head + "\nrules:\n" + "\n".join(out) + "\n")
PY
