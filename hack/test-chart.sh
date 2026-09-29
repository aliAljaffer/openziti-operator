#!/usr/bin/env bash
# Renders the chart with several values and checks the result. Needs helm and yq.
set -u
cd "$(git rev-parse --show-toplevel)"
chart=charts/chart
fail=0
ok()   { echo "ok   $1"; }
bad()  { echo "FAIL $1"; fail=1; }
check() { if eval "$2"; then ok "$1"; else bad "$1"; fi; }
render() { helm template t "$chart" -n ns "$@" 2>&1; }

conn=(--set connection.create=true --set connection.managementUrl=https://ziti.example.com/edge/management/v1
      --set 'connection.hostingRouters={router-a}')

out=$(render)
check "default: no ZitiConnection" '[ -z "$(yq "select(.kind==\"ZitiConnection\")" <<<"$out")" ]'
check "default: no trust-manager Bundle" '[ -z "$(yq "select(.kind==\"Bundle\")" <<<"$out")" ]'
check "default: two replicas" 'grep -q "replicas: 2" <<<"$out"'
check "default: cluster-wide secrets rule" 'yq "select(.kind==\"ClusterRole\" and (.metadata.name|test(\"manager-role\"))) | .rules[].resources[]" <<<"$out" | grep -qx secrets'
check "default: no --secret-namespaces flag" '! grep -q -- "--secret-namespaces" <<<"$out"'

out=$(render "${conn[@]}" --set connection.auth.updb.secretName=cred --set connection.roleScope=Global --set 'connection.defaultEdgeRouters={edge-1}')
zc=$(yq 'select(.kind=="ZitiConnection")' <<<"$out")
check "updb: ZitiConnection rendered" '[ "$(yq .spec.auth.updb.secretRef.name <<<"$zc")" = cred ] && [ "$(yq .spec.auth.updb.secretRef.namespace <<<"$zc")" = ns ]'
check "updb: no cert block" '[ "$(yq ".spec.auth | has(\"cert\")" <<<"$zc")" = false ]'
check "updb: values reach the spec" '[ "$(yq .spec.roleScope <<<"$zc")" = Global ] && [ "$(yq ".spec.hostingRouters[0]" <<<"$zc")" = router-a ] && [ "$(yq ".spec.defaultEdgeRouters[0]" <<<"$zc")" = edge-1 ]'
check "updb: CA ConfigMap is in the release namespace" '[ "$(yq .spec.caBundle.configMapRef.namespace <<<"$zc")" = ns ]'

out=$(render "${conn[@]}" --set connection.auth.cert.secretName=opcert --set 'connection.allowedNamespaces.matchLabels.ziti=enabled')
zc=$(yq 'select(.kind=="ZitiConnection")' <<<"$out")
check "cert: cert auth and allowedNamespaces" '[ "$(yq .spec.auth.cert.secretRef.name <<<"$zc")" = opcert ] && [ "$(yq .spec.allowedNamespaces.matchLabels.ziti <<<"$zc")" = enabled ]'

check "fail: managementUrl missing" 'render --set connection.create=true --set "connection.hostingRouters={r}" --set connection.auth.updb.secretName=c | grep -q "managementUrl is required"'
check "fail: no hosting router" 'render --set connection.create=true --set connection.managementUrl=https://x --set connection.auth.updb.secretName=c | grep -q "hostingRouters needs"'
check "fail: both auth forms" 'render "${conn[@]}" --set connection.auth.updb.secretName=a --set connection.auth.cert.secretName=b | grep -q "only one of"'
check "fail: no auth" 'render "${conn[@]}" | grep -q "set connection.auth.updb.secretName or"'

out=$(render --set trustManager.enabled=true --set 'trustManager.sources[0].secret.name=root' --set 'trustManager.sources[0].secret.key=ca.crt')
b=$(yq 'select(.kind=="Bundle")' <<<"$out")
check "trust-manager: Bundle targets the release namespace" '[ "$(yq .spec.target.namespaceSelector.matchLabels.\"kubernetes.io/metadata.name\" <<<"$b")" = ns ] && [ "$(yq .spec.target.configMap.key <<<"$b")" = ca.crt ]'
check "fail: trust-manager without sources" 'render --set trustManager.enabled=true | grep -q "needs at least one source"'

out=$(render --set 'rbac.secretNamespaces={team-a,team-b}')
check "limited secrets: Roles in release namespace and both listed" '[ "$(yq "select(.kind==\"Role\" and (.metadata.name|test(\"secret-access\"))) | .metadata.namespace" <<<"$out" | sort | tr "\n" " ")" = "---- ns team-a team-b " ] || [ "$(yq "select(.kind==\"Role\" and (.metadata.name|test(\"secret-access\"))) | .metadata.namespace" <<<"$out" | grep -v "^---" | sort | tr "\n" " ")" = "ns team-a team-b " ]'
check "limited secrets: ClusterRole has no secrets or configmaps" '! yq "select(.kind==\"ClusterRole\" and (.metadata.name|test(\"manager-role\"))) | .rules[].resources[]" <<<"$out" | grep -qxE "secrets|configmaps"'
check "limited secrets: flag lists the release namespace first" 'grep -q -- "--secret-namespaces=ns,team-a,team-b" <<<"$out"'

for crd in config/crd/bases/*.yaml; do
  name=$(yq .metadata.name "$crd")
  want=$(yq -o=json -I=0 '.spec' "$crd" | sed 's/[[:space:]]//g')
  have=$(helm template t "$chart" -n ns --show-only "templates/crd/$name.yaml" 2>&1 | yq -o=json -I=0 '.spec' | sed 's/[[:space:]]//g')
  check "CRD in the chart matches config/crd/bases: $name" '[ "$want" = "$have" ]'
done
rules() { yq -o=json -I=0 '[.rules[] | {"g": ((.apiGroups // []) | sort), "r": ((.resources // []) | sort), "u": ((.nonResourceURLs // []) | sort), "v": (.verbs | sort)}] | sort_by(. | tostring)'; }
want=$(yq 'select(.kind=="ClusterRole")' config/rbac/role.yaml | rules)
have=$(helm template t "$chart" -n ns --show-only templates/rbac/manager-role.yaml | rules)
check "manager RBAC in the chart matches config/rbac/role.yaml" '[ -n "$want" ] && [ "$want" = "$have" ]'
exit $fail
