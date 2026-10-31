// SPDX-License-Identifier: Apache-2.0

// Package accessreport prints which identities can dial or bind each Ziti service and which routers carry it.
package accessreport

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"slices"
	"strings"
	"text/tabwriter"

	"github.com/aliAljaffer/openziti-operator/internal/audit"
	"github.com/aliAljaffer/openziti-operator/internal/check"
	"github.com/aliAljaffer/openziti-operator/internal/ziti"
)

type Options struct {
	Connection        string
	Kubeconfig        string
	URL               string
	CAFile            string
	RequestsPerSecond float64
	Format            string
}

type Row struct {
	Service string   `json:"service"`
	Dialers []string `json:"dialers"`
	Binders []string `json:"binders"`
	Routers []string `json:"routers"`
	Posture []string `json:"postureCheckRoles"`
}

func Main(args []string) int {
	fs := flag.NewFlagSet("access", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var o Options
	fs.StringVar(&o.Connection, "connection", "", "ZitiConnection to read the URL and credentials from. Needs cluster access.")
	fs.StringVar(&o.Kubeconfig, "kubeconfig", "", "Kubeconfig path. Default: in-cluster config or the default kubeconfig.")
	fs.StringVar(&o.URL, "url", "", "Edge Management API URL. Ignored with --connection.")
	fs.StringVar(&o.CAFile, "ca-file", "", "PEM CA bundle. Ignored with --connection.")
	fs.Float64Var(&o.RequestsPerSecond, "requests-per-second", 10, "Rate limit for calls to the API")
	fs.StringVar(&o.Format, "o", "table", "Output format: table or json")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fs.NArg() > 0 {
		fmt.Fprintln(os.Stderr, "access: unexpected arguments:", fs.Args())
		return 2
	}
	if err := Run(context.Background(), o); err != nil {
		fmt.Fprintln(os.Stderr, "access:", err)
		return 2
	}
	return 0
}

func Run(ctx context.Context, o Options) error {
	if o.Format != "table" && o.Format != "json" {
		return fmt.Errorf("-o must be table or json")
	}
	zc, err := audit.ClientFor(ctx, audit.Options{
		Connection: o.Connection, Kubeconfig: o.Kubeconfig, URL: o.URL, CAFile: o.CAFile,
		RequestsPerSecond: o.RequestsPerSecond,
	})
	if err != nil {
		return err
	}
	g, err := check.Load(ctx, zc)
	if err != nil {
		return err
	}
	posture, err := zc.List(ctx, ziti.PostureChecks, "")
	if err != nil {
		return err
	}
	postureNames := map[string]string{}
	for _, p := range posture {
		postureNames[p.ID()] = p.Name()
	}
	rows := build(g, postureNames)
	if o.Format == "json" {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(rows)
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "SERVICE\tDIALERS\tBINDERS\tROUTERS\tPOSTURE")
	for _, r := range rows {
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", r.Service,
			strings.Join(r.Dialers, ","), strings.Join(r.Binders, ","), strings.Join(r.Routers, ","), strings.Join(r.Posture, ","))
	}
	return w.Flush()
}

func build(g *check.Graph, postureNames map[string]string) []Row {
	rows := make([]Row, 0, len(g.Services))
	for _, svc := range g.Services {
		report := g.Service(svc)
		row := Row{Service: svc.Name()}
		for _, id := range report.Dialers {
			row.Dialers = append(row.Dialers, nameOf(g.Identities, id))
		}
		slices.Sort(row.Dialers)
		row.Dialers = slices.Compact(row.Dialers)

		for _, p := range g.ServicePolicies {
			if p.Name() == "" || str(p, "type") != "Bind" {
				continue
			}
			if !check.Selects(strs(p, "serviceRoles"), str(p, "semantic"), svc.ID(), strList(svc["roleAttributes"])) {
				continue
			}
			for _, id := range check.Selected(g.Identities, p, "identityRoles") {
				row.Binders = append(row.Binders, nameOf(g.Identities, id))
			}
		}
		slices.Sort(row.Binders)
		row.Binders = slices.Compact(row.Binders)

		for _, p := range g.SERPs {
			if !check.Selects(strs(p, "serviceRoles"), str(p, "semantic"), svc.ID(), strList(svc["roleAttributes"])) {
				continue
			}
			for _, id := range check.Selected(g.Routers, p, "edgeRouterRoles") {
				row.Routers = append(row.Routers, nameOf(g.Routers, id))
			}
		}
		slices.Sort(row.Routers)
		row.Routers = slices.Compact(row.Routers)

		for _, p := range g.ServicePolicies {
			if str(p, "type") != "Dial" {
				continue
			}
			if !check.Selects(strs(p, "serviceRoles"), str(p, "semantic"), svc.ID(), strList(svc["roleAttributes"])) {
				continue
			}
			for _, role := range strs(p, "postureCheckRoles") {
				if id, ok := strings.CutPrefix(role, "@"); ok {
					if name, found := postureNames[id]; found {
						role = "@" + name
					}
				}
				row.Posture = append(row.Posture, role)
			}
		}
		slices.Sort(row.Posture)
		row.Posture = slices.Compact(row.Posture)
		rows = append(rows, row)
	}
	slices.SortFunc(rows, func(a, b Row) int { return strings.Compare(a.Service, b.Service) })
	return rows
}

func nameOf(entities []ziti.Entity, id string) string {
	for _, e := range entities {
		if e.ID() == id {
			return e.Name()
		}
	}
	return id
}

func str(e ziti.Entity, key string) string { s, _ := e[key].(string); return s }

func strs(e ziti.Entity, key string) []string { return strList(e[key]) }

func strList(v any) []string {
	l, _ := v.([]any)
	out := make([]string, 0, len(l))
	for _, x := range l {
		if s, ok := x.(string); ok {
			out = append(out, s)
		}
	}
	return out
}
