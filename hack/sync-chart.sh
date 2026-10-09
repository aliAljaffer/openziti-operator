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
# (templates/rbac/secret-roles.yaml). The ziti-operator.limitSecrets helper covers both. With
# rbac.serviceNamespaces set, services move to per-namespace Roles the same way
# (templates/rbac/service-roles.yaml), and ziti-operator.limitServices covers that.
#
# controller-gen merges rules that share an apiGroup and a verb set, so secrets and services land in the same rule as
# persistentvolumeclaims. Guard the resource line, not the rule, or the ClusterRole would keep Secret access in the
# mode that exists to remove it. A rule made only of guarded resources is dropped whole, unless they want different
# guards, in which case it is guarded per line like a mixed one: one resource's guard around the whole rule would
# drop another resource's access in a mode meant to keep it.
#
# services is the one resource that is read from every namespace no matter where the router runs, so narrowing it
# drops the write verbs and adds a separate read rule back.
python3 - <<'PY'
import re

LIMIT_SECRETS = '{{- if not (include "ziti-operator.limitSecrets" .) }}'
LIMIT_SERVICES = '{{- if not (include "ziti-operator.limitServices" .) }}'
GUARDS = {"secrets": LIMIT_SECRETS, "configmaps": LIMIT_SECRETS, "services": LIMIT_SERVICES}

READ_BACK = """
{{- if (include "ziti-operator.limitServices" .) }}
- apiGroups:
  - ""
  resources:
  - services
  verbs:
  - get
  - list
  - watch
{{- end }}
"""

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
read_back = ""
for b in blocks:
    found = resources(b)
    names = found[2] if found else []
    if not found or not any(n in GUARDS for n in names):
        out.append("\n".join(b))
    elif all(n in GUARDS for n in names) and len({GUARDS[n] for n in names}) == 1:
        guard = GUARDS[names[0]]
        if "services" in names:
            read_back = READ_BACK
        out.append(guard + "\n" + "\n".join(b) + "\n{{- end }}")
    else:
        start, end, _ = found
        lines = []
        for n in names:
            if n in GUARDS:
                lines += [GUARDS[n], "  - " + n, "{{- end }}"]
                if n == "services":
                    read_back = READ_BACK
            else:
                lines.append("  - " + n)
        out.append("\n".join(b[:start + 1] + lines + b[end:]))

if read_back:
    out.append(read_back.rstrip("\n"))

path = "charts/chart/templates/rbac/manager-role.yaml"
tpl = open(path).read()
head = tpl.split("\nrules:\n", 1)[0]
open(path, "w").write(head + "\nrules:\n" + "\n".join(out) + "\n")
PY
