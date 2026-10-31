// SPDX-License-Identifier: Apache-2.0

// Package audit reads a Ziti network and reports problems. It never writes.
package audit

import (
	"cmp"
	"context"
	"crypto/x509"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/url"
	"os"
	"slices"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/openziti/edge-api/rest_util"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"

	zitiv1 "github.com/aliAljaffer/openziti-operator/api/v1alpha1"
	"github.com/aliAljaffer/openziti-operator/internal/check"
	"github.com/aliAljaffer/openziti-operator/internal/controller"
	"github.com/aliAljaffer/openziti-operator/internal/ziti"
)

// Options selects what to audit.
type Options struct {
	// Connection names a ZitiConnection in the cluster. The URL, CA bundle, and credentials come from it.
	Connection string
	// Kubeconfig is the kubeconfig path. Empty uses the in-cluster config or the default kubeconfig.
	Kubeconfig string
	// URL is the Edge Management API URL. Used when Connection is empty.
	URL string
	// CAFile is a PEM CA bundle. Empty uses the controller's well-known CA.
	CAFile string
	// Format is table or json.
	Format string
	// RequestsPerSecond limits calls to the Ziti API.
	RequestsPerSecond float64
	// FailOn exits 1 when a finding matches. Use any or a comma separated list of finding codes.
	FailOn string
}

// Main parses the audit flags and returns the process exit code.
func Main(args []string) int {
	fs := flag.NewFlagSet("audit", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var opts Options
	fs.StringVar(&opts.Connection, "connection", "", "ZitiConnection to read the URL and credentials from. Needs cluster access.")
	fs.StringVar(&opts.Kubeconfig, "kubeconfig", "", "Kubeconfig path. Default: in-cluster config or the default kubeconfig.")
	fs.StringVar(&opts.URL, "url", "", "Edge Management API URL, for example https://host:443/edge/management/v1. Ignored with --connection.")
	fs.StringVar(&opts.CAFile, "ca-file", "", "PEM CA bundle. Default: the controller's well-known CA. Ignored with --connection.")
	fs.StringVar(&opts.Format, "o", "table", "Output format: table or json")
	fs.Float64Var(&opts.RequestsPerSecond, "requests-per-second", 10, "Rate limit for calls to the API")
	fs.StringVar(&opts.FailOn, "fail-on", "", "Exit 1 when a finding matches: any or a comma separated list of codes.")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fs.NArg() > 0 {
		fmt.Fprintln(os.Stderr, "audit: unexpected arguments:", fs.Args())
		return 2
	}
	if err := Run(context.Background(), opts); err != nil {
		fmt.Fprintln(os.Stderr, "audit:", err)
		if _, ok := errors.AsType[*FindingsError](err); ok {
			return 1
		}
		return 2
	}
	return 0
}

// Run audits the network selected by opts.
func Run(ctx context.Context, opts Options) error {
	if opts.Format != "table" && opts.Format != "json" {
		return fmt.Errorf("-o must be table or json")
	}

	zc, err := ClientFor(ctx, opts)
	if err != nil {
		return err
	}

	g, err := check.Load(ctx, zc)
	if err != nil {
		return err
	}
	findings := g.Audit(time.Now())
	slices.SortFunc(findings, func(a, b check.Finding) int {
		return cmp.Or(cmp.Compare(a.Code, b.Code), cmp.Compare(a.Entity, b.Entity))
	})

	if opts.Format == "json" {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if findings == nil {
			findings = []check.Finding{}
		}
		if err := enc.Encode(findings); err != nil {
			return err
		}
	} else {
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		_, _ = fmt.Fprintln(w, "CODE\tENTITY\tMESSAGE")
		for _, f := range findings {
			_, _ = fmt.Fprintf(w, "%s\t%s\t%s\n", f.Code, f.Entity, f.Message)
		}
		if err := w.Flush(); err != nil {
			return err
		}
	}

	if opts.FailOn != "" {
		if matched := filterFindings(findings, opts.FailOn); len(matched) > 0 {
			return &FindingsError{Count: len(matched), Filter: opts.FailOn}
		}
	}
	return nil
}

// FindingsError says that the audit should fail because findings matched --fail-on.
type FindingsError struct {
	Count  int
	Filter string
}

func (e *FindingsError) Error() string {
	return fmt.Sprintf("%d finding(s) matched --fail-on %s", e.Count, e.Filter)
}

func filterFindings(findings []check.Finding, filter string) []check.Finding {
	filter = strings.TrimSpace(filter)
	if strings.EqualFold(filter, "any") {
		return findings
	}
	codes := map[string]bool{}
	for code := range strings.SplitSeq(filter, ",") {
		if code = strings.TrimSpace(code); code != "" {
			codes[strings.ToUpper(code)] = true
		}
	}
	var matched []check.Finding
	for _, finding := range findings {
		if codes[strings.ToUpper(finding.Code)] {
			matched = append(matched, finding)
		}
	}
	return matched
}

func ClientFor(ctx context.Context, opts Options) (ziti.Client, error) {
	if opts.Connection != "" {
		return connectionClient(ctx, opts)
	}

	user, pass := os.Getenv("ZITI_USERNAME"), os.Getenv("ZITI_PASSWORD")
	if opts.URL == "" || user == "" || pass == "" {
		return nil, fmt.Errorf("--url, ZITI_USERNAME and ZITI_PASSWORD are required, or use --connection")
	}
	u, err := url.Parse(opts.URL)
	if err != nil {
		return nil, err
	}
	var pool *x509.CertPool
	if opts.CAFile != "" {
		pem, err := os.ReadFile(opts.CAFile)
		if err != nil {
			return nil, err
		}
		pool = x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("%s has no PEM certificates", opts.CAFile)
		}
	} else if pool, err = rest_util.GetControllerWellKnownCaPool("https://" + u.Host); err != nil {
		return nil, err
	}
	auth := rest_util.NewAuthenticatorUpdb(user, pass)
	auth.RootCas = pool
	return ziti.NewREST(opts.URL, auth, opts.RequestsPerSecond)
}

func connectionClient(ctx context.Context, opts Options) (ziti.Client, error) {
	cfg, err := kubeConfig(opts.Kubeconfig)
	if err != nil {
		return nil, err
	}
	scheme := runtime.NewScheme()
	if err := zitiv1.AddToScheme(scheme); err != nil {
		return nil, err
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		return nil, err
	}
	k, err := client.New(cfg, client.Options{Scheme: scheme})
	if err != nil {
		return nil, err
	}
	return connectionClientFor(ctx, k, opts)
}

func connectionClientFor(ctx context.Context, k client.Reader, opts Options) (ziti.Client, error) {
	var conn zitiv1.ZitiConnection
	if err := k.Get(ctx, types.NamespacedName{Name: opts.Connection}, &conn); err != nil {
		return nil, fmt.Errorf("ZitiConnection %q: %w", opts.Connection, err)
	}
	p := &controller.SecretClientProvider{Reader: k, RequestsPerSecond: opts.RequestsPerSecond}
	return p.For(ctx, &conn)
}

func kubeConfig(path string) (*rest.Config, error) {
	if path != "" {
		return clientcmd.BuildConfigFromFlags("", path)
	}
	if cfg, err := rest.InClusterConfig(); err == nil {
		return cfg, nil
	}
	return clientcmd.BuildConfigFromFlags("", "")
}
