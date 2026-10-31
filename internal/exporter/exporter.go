// SPDX-License-Identifier: Apache-2.0

// Package exporter reads a Ziti network and writes Kubernetes manifests that match this operator's CRDs.
// It never writes to Ziti and every generated resource defaults to managementPolicy Observe.
package exporter

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"

	zitiv1 "github.com/aliAljaffer/openziti-operator/api/v1alpha1"
	"github.com/aliAljaffer/openziti-operator/internal/audit"
	"github.com/aliAljaffer/openziti-operator/internal/ziti"
)

type Options struct {
	Connection         string
	Kubeconfig         string
	URL                string
	CAFile             string
	RequestsPerSecond  float64
	Namespace          string
	OutputDir          string
	ManagementPolicy   string
	RoleScope          string
	IncludeTerminators bool
}

func Main(args []string) int {
	fs := flag.NewFlagSet("export", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var o Options
	fs.StringVar(&o.Connection, "connection", "", "ZitiConnection to read the URL and credentials from. Needs cluster access.")
	fs.StringVar(&o.Kubeconfig, "kubeconfig", "", "Kubeconfig path. Default: in-cluster config or the default kubeconfig.")
	fs.StringVar(&o.URL, "url", "", "Edge Management API URL. Ignored with --connection.")
	fs.StringVar(&o.CAFile, "ca-file", "", "PEM CA bundle. Ignored with --connection.")
	fs.Float64Var(&o.RequestsPerSecond, "requests-per-second", 10, "Rate limit for calls to the API")
	fs.StringVar(&o.Namespace, "namespace", "default", "Namespace for the generated resources")
	fs.StringVar(&o.OutputDir, "output-dir", "ziti-export", "Directory that receives the manifests")
	fs.StringVar(&o.ManagementPolicy, "management-policy", "Observe", "Observe or Adopt for generated resources")
	fs.StringVar(&o.RoleScope, "role-scope", "Global", "Role scope of the target ZitiConnection: Global or Namespaced")
	fs.BoolVar(&o.IncludeTerminators, "include-terminators", false, "Also export runtime terminators")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fs.NArg() > 0 {
		fmt.Fprintln(os.Stderr, "export: unexpected arguments:", fs.Args())
		return 2
	}
	if err := Run(context.Background(), o); err != nil {
		fmt.Fprintln(os.Stderr, "export:", err)
		return 2
	}
	return 0
}

type doc struct {
	kind    string
	objects []any
	name    string
}

func Run(ctx context.Context, o Options) error {
	if o.ManagementPolicy != string(zitiv1.ManagementObserve) && o.ManagementPolicy != string(zitiv1.ManagementAdopt) {
		return fmt.Errorf("--management-policy must be Observe or Adopt")
	}
	if o.RoleScope != string(zitiv1.RoleScopeGlobal) && o.RoleScope != string(zitiv1.RoleScopeNamespaced) {
		return fmt.Errorf("--role-scope must be Global or Namespaced")
	}
	zc, err := audit.ClientFor(ctx, audit.Options{
		Connection: o.Connection, Kubeconfig: o.Kubeconfig, URL: o.URL, CAFile: o.CAFile,
		RequestsPerSecond: o.RequestsPerSecond,
	})
	if err != nil {
		return err
	}
	lists, err := readAll(ctx, zc, o.IncludeTerminators)
	if err != nil {
		return err
	}
	names := nameIndex(lists)
	configTypes := map[string]string{}
	for _, e := range lists[ziti.ConfigTypes] {
		configTypes[e.ID()] = e.Name()
	}
	report := &strings.Builder{}
	fmt.Fprintf(report, "# Ziti export report\n\n")
	fmt.Fprintf(report, "- Namespace: `%s`\n- Management policy: `%s`\n- Role scope: `%s`\n\n", o.Namespace, o.ManagementPolicy, o.RoleScope)

	docs := exportDocs(lists, names, configTypes, o, report)
	if err := os.MkdirAll(o.OutputDir, 0o755); err != nil {
		return err
	}
	var kustomize []string
	for _, d := range docs {
		if len(d.objects) == 0 {
			continue
		}
		file := d.name + ".yaml"
		if err := writeDocs(filepath.Join(o.OutputDir, file), d.objects); err != nil {
			return err
		}
		kustomize = append(kustomize, file)
	}
	if len(kustomize) > 0 {
		slices.Sort(kustomize)
		var k strings.Builder
		k.WriteString("apiVersion: kustomize.config.k8s.io/v1beta1\nkind: Kustomization\nresources:\n")
		for _, f := range kustomize {
			k.WriteString("  - " + f + "\n")
		}
		if err := os.WriteFile(filepath.Join(o.OutputDir, "kustomization.yaml"), []byte(k.String()), 0o644); err != nil {
			return err
		}
	}
	if err := os.WriteFile(filepath.Join(o.OutputDir, "import-report.md"), []byte(report.String()), 0o644); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(os.Stdout, "wrote %d manifest files to %s\n", len(kustomize), o.OutputDir)
	return nil
}

func readAll(ctx context.Context, zc ziti.Client, terminators bool) (map[ziti.Kind][]ziti.Entity, error) {
	kinds := []ziti.Kind{
		ziti.Configs, ziti.ConfigTypes, ziti.Services, ziti.ServicePolicies,
		ziti.EdgeRouterPolicies, ziti.ServiceEdgeRouterPolicies, ziti.Identities,
		ziti.EdgeRouters, ziti.CertificateAuthorities, ziti.ExternalJWTSigners,
		ziti.AuthPolicies, ziti.PostureChecks,
	}
	if terminators {
		kinds = append(kinds, ziti.Terminators)
	}
	out := map[ziti.Kind][]ziti.Entity{}
	for _, kind := range kinds {
		list, err := zc.List(ctx, kind, "")
		if err != nil {
			return nil, fmt.Errorf("list %s: %w", kind, err)
		}
		out[kind] = list
	}
	return out, nil
}

func nameIndex(lists map[ziti.Kind][]ziti.Entity) map[ziti.Kind]map[string]string {
	out := map[ziti.Kind]map[string]string{}
	for kind, list := range lists {
		out[kind] = map[string]string{}
		for _, e := range list {
			out[kind][e.ID()] = e.Name()
		}
	}
	return out
}

//nolint:gocyclo // one block per exported kind
func exportDocs(lists map[ziti.Kind][]ziti.Entity, names map[ziti.Kind]map[string]string, configTypes map[string]string, o Options, report *strings.Builder) []doc {
	var docs []doc
	used := map[string]map[string]bool{}
	nameFor := func(kind, zitiName string) string {
		if used[kind] == nil {
			used[kind] = map[string]bool{}
		}
		base := sanitize(zitiName)
		if base == "" {
			base = "ziti"
		}
		name := base
		if used[kind][name] {
			name = base + "-" + shortHash(kind+"/"+zitiName)
		}
		used[kind][name] = true
		return name
	}
	add := func(kind string, objs ...any) {
		for i := range docs {
			if docs[i].kind == kind {
				docs[i].objects = append(docs[i].objects, objs...)
				return
			}
		}
		docs = append(docs, doc{kind: kind, name: fileForKind(kind), objects: objs})
	}

	var configs []any
	for _, e := range lists[ziti.Configs] {
		typeName := configTypes[getString(e, "configTypeId")]
		if typeName == "" {
			fmt.Fprintf(report, "- skipped config `%s`: config type not found\n", e.Name())
			continue
		}
		data := map[string]any{}
		if raw, ok := e["data"].(map[string]any); ok {
			data = raw
		}
		raw, _ := json.Marshal(data)
		configs = append(configs, &zitiv1.ZitiConfig{
			TypeMeta: typeMeta("ZitiConfig"),
			Name:     nameFor("ZitiConfig", e.Name()), Namespace: o.Namespace,
			Spec: zitiv1.ZitiConfigSpec{
				EntitySpec: observeSpec(e.Name(), o.ManagementPolicy),
				Type:       typeName,
				Data:       apiextensionsv1.JSON{Raw: raw},
			},
		})
	}
	if len(configs) > 0 {
		add("ZitiConfig", configs...)
	}

	for _, e := range lists[ziti.Services] {
		cfgNames := idsToNames(getStrings(e, "configs"), names[ziti.Configs], report, "service "+e.Name())
		enc := getBool(e, "encryptionRequired")
		add("ZitiService", &zitiv1.ZitiService{
			TypeMeta: typeMeta("ZitiService"),
			Name:     nameFor("ZitiService", e.Name()), Namespace: o.Namespace,
			Spec: zitiv1.ZitiServiceSpec{
				EntitySpec:         observeSpec(e.Name(), o.ManagementPolicy),
				RoleAttributes:     getStrings(e, "roleAttributes"),
				Configs:            cfgNames,
				TerminatorStrategy: getString(e, "terminatorStrategy"),
				EncryptionRequired: &enc,
				MaxIdleTimeMillis:  getInt(e, "maxIdleTimeMillis"),
			},
		})
	}

	for _, e := range lists[ziti.ServicePolicies] {
		add("ZitiServicePolicy", &zitiv1.ZitiServicePolicy{
			TypeMeta: typeMeta("ZitiServicePolicy"),
			Name:     nameFor("ZitiServicePolicy", e.Name()), Namespace: o.Namespace,
			Spec: zitiv1.ZitiServicePolicySpec{
				EntitySpec:        observeSpec(e.Name(), o.ManagementPolicy),
				Type:              getString(e, "type"),
				Semantic:          getString(e, "semantic"),
				IdentityRoles:     idRoles(getStrings(e, "identityRoles"), names[ziti.Identities], report, "policy "+e.Name()),
				ServiceRoles:      idRoles(getStrings(e, "serviceRoles"), names[ziti.Services], report, "policy "+e.Name()),
				PostureCheckRoles: idRoles(getStrings(e, "postureCheckRoles"), names[ziti.PostureChecks], report, "policy "+e.Name()),
			},
		})
	}

	for _, e := range lists[ziti.EdgeRouterPolicies] {
		add("ZitiEdgeRouterPolicy", &zitiv1.ZitiEdgeRouterPolicy{
			TypeMeta: typeMeta("ZitiEdgeRouterPolicy"),
			Name:     nameFor("ZitiEdgeRouterPolicy", e.Name()), Namespace: o.Namespace,
			Spec: zitiv1.ZitiEdgeRouterPolicySpec{
				EntitySpec:      observeSpec(e.Name(), o.ManagementPolicy),
				Semantic:        getString(e, "semantic"),
				IdentityRoles:   idRoles(getStrings(e, "identityRoles"), names[ziti.Identities], report, "policy "+e.Name()),
				EdgeRouterRoles: idRoles(getStrings(e, "edgeRouterRoles"), names[ziti.EdgeRouters], report, "policy "+e.Name()),
			},
		})
	}

	for _, e := range lists[ziti.ServiceEdgeRouterPolicies] {
		add("ZitiServiceEdgeRouterPolicy", &zitiv1.ZitiServiceEdgeRouterPolicy{
			TypeMeta: typeMeta("ZitiServiceEdgeRouterPolicy"),
			Name:     nameFor("ZitiServiceEdgeRouterPolicy", e.Name()), Namespace: o.Namespace,
			Spec: zitiv1.ZitiServiceEdgeRouterPolicySpec{
				EntitySpec:      observeSpec(e.Name(), o.ManagementPolicy),
				Semantic:        getString(e, "semantic"),
				ServiceRoles:    idRoles(getStrings(e, "serviceRoles"), names[ziti.Services], report, "policy "+e.Name()),
				EdgeRouterRoles: idRoles(getStrings(e, "edgeRouterRoles"), names[ziti.EdgeRouters], report, "policy "+e.Name()),
			},
		})
	}

	for _, e := range lists[ziti.Identities] {
		disabled := getBool(e, "disabled")
		authPolicy := names[ziti.AuthPolicies][getString(e, "authPolicyId")]
		if authPolicy == "" {
			authPolicy = getRefName(e["authPolicy"])
		}
		externalID := getString(e, "externalId")
		serviceAccount := ""
		prefix := "system:serviceaccount:" + o.Namespace + ":"
		if name, ok := strings.CutPrefix(externalID, prefix); ok && name != "" {
			serviceAccount = name
			externalID = ""
		}
		var disabledUntil *metav1.Time
		if raw := getString(e, "disabledUntil"); raw != "" {
			if t, err := time.Parse(time.RFC3339, raw); err == nil {
				disabledUntil = &metav1.Time{Time: t}
			}
		}
		identity := &zitiv1.ZitiIdentity{
			TypeMeta: typeMeta("ZitiIdentity"),
			Name:     nameFor("ZitiIdentity", e.Name()), Namespace: o.Namespace,
			Spec: zitiv1.ZitiIdentitySpec{
				ZitiName:         e.Name(),
				RoleAttributes:   getStrings(e, "roleAttributes"),
				AuthPolicy:       authPolicy,
				ExternalID:       externalID,
				Type:             identityType(e),
				Disabled:         disabled,
				DisabledUntil:    disabledUntil,
				ServiceAccount:   serviceAccount,
				ManagementPolicy: zitiv1.ManagementObserve,
			},
		}
		add("ZitiIdentity", identity)
		if disabled {
			fmt.Fprintf(report, "- identity `%s` is disabled and is exported as `disabled: true`\n", e.Name())
		}
		fmt.Fprintf(report, "- identity `%s` is exported Observe only: the controller does not hold its private key. Use `importFrom` with an existing identity file before switching to Adopt.\n", e.Name())
	}

	for _, e := range lists[ziti.EdgeRouters] {
		add("ZitiRouter", &zitiv1.ZitiRouter{
			TypeMeta: typeMeta("ZitiRouter"),
			Name:     nameFor("ZitiRouter", e.Name()),
			Spec: zitiv1.ZitiRouterSpec{
				ZitiName:         e.Name(),
				RoleAttributes:   getStrings(e, "roleAttributes"),
				TunnelerEnabled:  getBool(e, "isTunnelerEnabled"),
				Cost:             int32(getInt(e, "cost")),
				NoTraversal:      getBool(e, "noTraversal"),
				Disabled:         getBool(e, "disabled"),
				ManagementPolicy: zitiv1.ManagementObserve,
			},
		})
	}

	for _, e := range lists[ziti.CertificateAuthorities] {
		name := nameFor("ZitiCA", e.Name())
		secretName := name + "-ca"
		cert := getString(e, "certPem")
		if cert == "" {
			fmt.Fprintf(report, "- skipped CA `%s`: the API response has no certificate\n", e.Name())
			continue
		}
		claim := getMap(e, "externalIdClaim")
		auto := getBool(e, "isAutoCaEnrollmentEnabled")
		spec := zitiv1.ZitiCASpec{
			ZitiName:         e.Name(),
			ManagementPolicy: zitiv1.ManagementObserve,
			Certificate: zitiv1.CertificateSource{
				SecretRef: zitiv1.SecretRef{Namespace: o.Namespace, Name: secretName},
				CertKey:   "tls.crt",
			},
			AuthEnabled: new(getBool(e, "isAuthEnabled")),
			ExternalIDClaim: zitiv1.ExternalIDClaim{
				Location:        getString(claim, "location"),
				Matcher:         getString(claim, "matcher"),
				MatcherCriteria: getString(claim, "matcherCriteria"),
				Parser:          getString(claim, "parser"),
				ParserCriteria:  getString(claim, "parserCriteria"),
				Index:           int32(getInt(claim, "index")),
			},
		}
		if auto {
			spec.AutoEnrollment = &zitiv1.AutoEnrollment{
				IdentityRoles:      getStrings(e, "identityRoles"),
				IdentityNameFormat: getString(e, "identityNameFormat"),
			}
		}
		add("ZitiCA", &zitiv1.ZitiCA{
			TypeMeta: typeMeta("ZitiCA"),
			Name:     name,
			Spec:     spec,
		})
		if cert != "" {
			add("Secret", &corev1.Secret{
				APIVersion: "v1", Kind: "Secret",
				Name: secretName, Namespace: o.Namespace,
				Type: corev1.SecretTypeOpaque,
				Data: map[string][]byte{"tls.crt": []byte(cert)},
			})
		}
	}

	for _, e := range lists[ziti.ExternalJWTSigners] {
		keys := zitiv1.SignerKeys{}
		if ep := getString(e, "jwksEndpoint"); ep != "" {
			keys.JwksEndpoint = &zitiv1.JwksEndpoint{URL: ep, Issuer: getString(e, "issuer")}
		} else {
			keys.CertPEM = getString(e, "certPem")
			keys.Kid = getString(e, "kid")
		}
		if keys.JwksEndpoint == nil && keys.CertPEM == "" {
			fmt.Fprintf(report, "- skipped external JWT signer `%s`: no key material in the API response\n", e.Name())
			continue
		}
		add("ZitiJwtSigner", &zitiv1.ZitiJwtSigner{
			TypeMeta: typeMeta("ZitiJwtSigner"),
			Name:     nameFor("ZitiJwtSigner", e.Name()),
			Spec: zitiv1.ZitiJwtSignerSpec{
				ZitiName:         e.Name(),
				ManagementPolicy: zitiv1.ManagementObserve,
				Audience:         getString(e, "audience"),
				Issuer:           getString(e, "issuer"),
				ClaimsProperty:   getString(e, "claimsProperty"),
				UseExternalID:    new(getBool(e, "useExternalId")),
				Keys:             keys,
				CreateAuthPolicy: new(false),
			},
		})
	}

	for _, e := range lists[ziti.AuthPolicies] {
		add("ZitiAuthPolicy", &zitiv1.ZitiAuthPolicy{
			TypeMeta: typeMeta("ZitiAuthPolicy"),
			Name:     nameFor("ZitiAuthPolicy", e.Name()), Namespace: o.Namespace,
			Spec: authPolicySpec(e, names, report),
		})
	}

	for _, e := range lists[ziti.PostureChecks] {
		data := map[string]any{}
		for k, v := range e {
			switch k {
			case "id", "name", "typeId", "roleAttributes", "tags", "createdAt", "updatedAt", "links":
				continue
			}
			data[k] = v
		}
		raw, _ := json.Marshal(data)
		add("ZitiPostureCheck", &zitiv1.ZitiPostureCheck{
			TypeMeta: typeMeta("ZitiPostureCheck"),
			Name:     nameFor("ZitiPostureCheck", e.Name()), Namespace: o.Namespace,
			Spec: zitiv1.ZitiPostureCheckSpec{
				EntitySpec:     observeSpec(e.Name(), o.ManagementPolicy),
				Type:           getString(e, "typeId"),
				RoleAttributes: getStrings(e, "roleAttributes"),
				Data:           apiextensionsv1.JSON{Raw: raw},
			},
		})
	}

	if o.IncludeTerminators {
		for i, e := range lists[ziti.Terminators] {
			service := names[ziti.Services][getString(e, "serviceId")]
			router := names[ziti.EdgeRouters][getString(e, "routerId")]
			address := getString(e, "address")
			if service == "" || router == "" || address == "" {
				fmt.Fprintf(report, "- skipped terminator %d: service, router, or address is missing\n", i)
				continue
			}
			add("ZitiTerminator", &zitiv1.ZitiTerminator{
				TypeMeta: typeMeta("ZitiTerminator"),
				Name:     fmt.Sprintf("terminator-%s-%d", sanitize(service), i), Namespace: o.Namespace,
				Spec: zitiv1.ZitiTerminatorSpec{
					Service:    service,
					Router:     router,
					Address:    address,
					Binding:    getString(e, "binding"),
					Cost:       int32(getInt(e, "cost")),
					Precedence: getString(e, "precedence"),
				},
			})
		}
	}

	fmt.Fprintf(report, "\n## Counts\n\n")
	for _, d := range docs {
		fmt.Fprintf(report, "- %s: %d\n", d.kind, len(d.objects))
	}
	fmt.Fprintf(report, "\n## Not exported\n\n")
	fmt.Fprintf(report, "- `ZitiConnection`: credentials and CA bundle are outside the Edge Management API.\n")
	fmt.Fprintf(report, "- `ZitiApp`: a service bundle is ambiguous; the one-to-one resources above are the faithful form.\n")
	fmt.Fprintf(report, "- `ZitiGroup`: Ziti has no group object; role attributes are preserved on identities and services.\n")
	fmt.Fprintf(report, "- `ZitiSidecar`, `ZitiPortForward`: they own no Ziti entity.\n")
	fmt.Fprintf(report, "- sessions, API sessions, posture data, enrollments: runtime state.\n")
	if !o.IncludeTerminators {
		fmt.Fprintf(report, "- terminators: runtime state; use `--include-terminators` to snapshot them.\n")
	}
	return docs
}

func typeMeta(kind string) metav1.TypeMeta {
	return metav1.TypeMeta{APIVersion: zitiv1.GroupVersion.String(), Kind: kind}
}

func observeSpec(zitiName, policy string) zitiv1.EntitySpec {
	return zitiv1.EntitySpec{ZitiName: zitiName, ManagementPolicy: zitiv1.ManagementPolicy(policy)}
}

func fileForKind(kind string) string {
	switch kind {
	case "ZitiConfig":
		return "ziti-configs"
	case "ZitiService":
		return "ziti-services"
	case "ZitiServicePolicy":
		return "ziti-service-policies"
	case "ZitiEdgeRouterPolicy":
		return "ziti-edge-router-policies"
	case "ZitiServiceEdgeRouterPolicy":
		return "ziti-service-edge-router-policies"
	case "ZitiIdentity":
		return "ziti-identities"
	case "ZitiRouter":
		return "ziti-routers"
	case "ZitiCA":
		return "ziti-cas"
	case "Secret":
		return "ziti-ca-secrets"
	case "ZitiJwtSigner":
		return "ziti-jwt-signers"
	case "ZitiAuthPolicy":
		return "ziti-auth-policies"
	case "ZitiPostureCheck":
		return "ziti-posture-checks"
	case "ZitiTerminator":
		return "ziti-terminators"
	}
	return strings.ToLower(kind)
}

func writeDocs(path string, objects []any) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	for i, obj := range objects {
		raw, err := json.Marshal(obj)
		if err != nil {
			return err
		}
		y, err := yaml.JSONToYAML(raw)
		if err != nil {
			return err
		}
		if i > 0 {
			if _, err := f.WriteString("---\n"); err != nil {
				return err
			}
		}
		if _, err := f.Write(y); err != nil {
			return err
		}
	}
	return nil
}

func sanitize(s string) string {
	s = strings.ToLower(s)
	var b strings.Builder
	lastDash := false
	for _, r := range s {
		ok := r >= 'a' && r <= 'z' || r >= '0' && r <= '9'
		if ok {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	out := strings.Trim(b.String(), "-")
	if len(out) > 50 {
		out = out[:50] + "-" + shortHash(s)
	}
	return out
}

func shortHash(s string) string {
	sum := sha1.Sum([]byte(s))
	return hex.EncodeToString(sum[:4])
}

func getString(m map[string]any, key string) string { s, _ := m[key].(string); return s }

func getStrings(m map[string]any, key string) []string {
	l, _ := m[key].([]any)
	out := make([]string, 0, len(l))
	for _, v := range l {
		if s, ok := v.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func getBool(m map[string]any, key string) bool { b, _ := m[key].(bool); return b }

func getInt(m map[string]any, key string) int64 {
	switch v := m[key].(type) {
	case float64:
		return int64(v)
	case int64:
		return v
	case int:
		return int64(v)
	case json.Number:
		n, _ := v.Int64()
		return n
	}
	return 0
}

func getMap(m map[string]any, key string) map[string]any {
	v, _ := m[key].(map[string]any)
	return v
}

func getRefName(v any) string {
	m, _ := v.(map[string]any)
	name, _ := m["name"].(string)
	return name
}

func idsToNames(ids []string, names map[string]string, report *strings.Builder, where string) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if name, ok := names[id]; ok {
			out = append(out, name)
			continue
		}
		fmt.Fprintf(report, "- unresolved config id `%s` on %s\n", id, where)
	}
	return out
}

func idRoles(roles []string, names map[string]string, report *strings.Builder, where string) []string {
	out := make([]string, 0, len(roles))
	for _, r := range roles {
		if name, ok := strings.CutPrefix(r, "@"); ok {
			if got, found := names[name]; found {
				out = append(out, "@"+got)
			} else {
				out = append(out, r)
				fmt.Fprintf(report, "- unresolved role `%s` on %s\n", r, where)
			}
			continue
		}
		out = append(out, r)
	}
	return out
}

func identityType(e ziti.Entity) string {
	switch v := e["type"].(type) {
	case string:
		if v != "" {
			return v
		}
	case map[string]any:
		if n, _ := v["name"].(string); n != "" {
			return n
		}
	}
	return "Default"
}

func authPolicySpec(e ziti.Entity, names map[ziti.Kind]map[string]string, report *strings.Builder) zitiv1.ZitiAuthPolicySpec {
	primary := getMap(e, "primary")
	secondary := getMap(e, "secondary")
	cert := getMap(primary, "cert")
	updb := getMap(primary, "updb")
	ext := getMap(primary, "extJwt")
	signers := idRoles(getStrings(ext, "allowedSigners"), names[ziti.ExternalJWTSigners], report, "auth policy "+e.Name())
	return zitiv1.ZitiAuthPolicySpec{
		EntitySpec: observeSpec(e.Name(), string(zitiv1.ManagementObserve)),
		Primary: zitiv1.AuthPolicyPrimary{
			Cert: zitiv1.AuthPolicyCert{
				Allowed:           new(getBool(cert, "allowed")),
				AllowExpiredCerts: getBool(cert, "allowExpiredCerts"),
			},
			Updb: zitiv1.AuthPolicyUpdb{
				Allowed:                new(getBool(updb, "allowed")),
				MinPasswordLength:      getInt(updb, "minPasswordLength"),
				MaxAttempts:            getInt(updb, "maxAttempts"),
				LockoutDurationMinutes: getInt(updb, "lockoutDurationMinutes"),
				RequireMixedCase:       getBool(updb, "requireMixedCase"),
				RequireNumberChar:      getBool(updb, "requireNumberChar"),
				RequireSpecialChar:     getBool(updb, "requireSpecialChar"),
			},
			ExtJWT: zitiv1.AuthPolicyExtJWT{
				Allowed:        new(getBool(ext, "allowed")),
				AllowedSigners: signers,
			},
		},
		Secondary: zitiv1.AuthPolicySecondary{
			RequireTotp:         getBool(secondary, "requireTotp"),
			RequireExtJwtSigner: names[ziti.ExternalJWTSigners][getString(secondary, "requireExtJwtSigner")],
		},
	}
}

//go:fix inline
