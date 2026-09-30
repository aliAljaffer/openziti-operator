// SPDX-License-Identifier: Apache-2.0

// Command audit reports problems in a Ziti network. It only reads.
//
//	ZITI_USERNAME=admin ZITI_PASSWORD=... audit --url https://ctrl:443/edge/management/v1 \
//	  [--ca-file ca.pem] [-o table|json]
package main

import (
	"cmp"
	"context"
	"crypto/x509"
	"encoding/json"
	"flag"
	"fmt"
	"net/url"
	"os"
	"slices"
	"text/tabwriter"
	"time"

	"github.com/openziti/edge-api/rest_util"

	"github.com/aliAljaffer/openziti-operator/internal/check"
	"github.com/aliAljaffer/openziti-operator/internal/ziti"
)

func main() {
	mgmt := flag.String("url", "", "Edge Management API URL, for example https://host:443/edge/management/v1")
	caFile := flag.String("ca-file", "", "PEM CA bundle. Default: the controller's well-known CA.")
	format := flag.String("o", "table", "Output format: table or json")
	rps := flag.Float64("requests-per-second", 10, "Rate limit for calls to the API")
	flag.Parse()
	if err := run(*mgmt, *caFile, *format, *rps); err != nil {
		fmt.Fprintln(os.Stderr, "audit:", err)
		os.Exit(2)
	}
}

func run(mgmt, caFile, format string, rps float64) error {
	user, pass := os.Getenv("ZITI_USERNAME"), os.Getenv("ZITI_PASSWORD")
	if mgmt == "" || user == "" || pass == "" {
		return fmt.Errorf("--url, ZITI_USERNAME and ZITI_PASSWORD are required")
	}
	if format != "table" && format != "json" {
		return fmt.Errorf("-o must be table or json")
	}
	u, err := url.Parse(mgmt)
	if err != nil {
		return err
	}
	var pool *x509.CertPool
	if caFile != "" {
		pem, err := os.ReadFile(caFile)
		if err != nil {
			return err
		}
		pool = x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return fmt.Errorf("%s has no PEM certificates", caFile)
		}
	} else if pool, err = rest_util.GetControllerWellKnownCaPool("https://" + u.Host); err != nil {
		return err
	}
	auth := rest_util.NewAuthenticatorUpdb(user, pass)
	auth.RootCas = pool
	zc, err := ziti.NewREST(mgmt, auth, rps)
	if err != nil {
		return err
	}

	ctx := context.Background()
	g, err := check.Load(ctx, zc)
	if err != nil {
		return err
	}
	findings := g.Audit(time.Now())
	slices.SortFunc(findings, func(a, b check.Finding) int {
		return cmp.Or(cmp.Compare(a.Code, b.Code), cmp.Compare(a.Entity, b.Entity))
	})

	if format == "json" {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if findings == nil {
			findings = []check.Finding{}
		}
		return enc.Encode(findings)
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "CODE\tENTITY\tMESSAGE")
	for _, f := range findings {
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\n", f.Code, f.Entity, f.Message)
	}
	return w.Flush()
}
