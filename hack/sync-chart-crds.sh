#!/usr/bin/env bash
# Copies config/crd/bases into the chart templates and wraps each CRD for Helm.
# Run after `make manifests`. hack/test-chart.sh fails when the copies differ.
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
