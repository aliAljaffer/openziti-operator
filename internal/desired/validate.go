// SPDX-License-Identifier: Apache-2.0

package desired

import (
	"fmt"
	"strings"

	zitiv1 "github.com/aliAljaffer/openziti-operator/api/v1alpha1"
	"github.com/aliAljaffer/openziti-operator/internal/ziti"
)

// CheckName rejects an entity name that would break the Ziti body. Quotes and backslashes end the
// JSON string early. Every kind that sets a ziti name checks it.
func CheckName(name string) error {
	if strings.ContainsAny(name, `"\`) {
		return fmt.Errorf("zitiName must not contain quotes or backslashes")
	}
	return nil
}

// CheckShape builds every entity of every group without writing, so a broken spec is reported before any
// change. allowed comes from AllowedRoles. Router ids are only used to format router roles, so a caller
// that has no network can fill PlaceholderRouterIDs and still reach every shape check.
func (b *Builder) CheckShape(groups []Group, allowed []string) error {
	for _, g := range groups {
		gb := b.WithGroup(g)
		if _, err := gb.InterceptConfig(""); err != nil {
			return err
		}
		if _, err := gb.HostConfig(""); err != nil {
			return err
		}
		if _, err := gb.Service("", ""); err != nil {
			return err
		}
		for _, f := range []func(string) (ziti.Entity, error){gb.Bind, gb.SERP} {
			if _, err := f(""); err != nil {
				return err
			}
		}
		if _, err := gb.Dial("", nil); err != nil {
			return err
		}
	}
	_, err := b.ERP(allowed)
	return err
}

// PlaceholderRouterIDs maps router names to themselves so the shape checks can run before Ziti is read.
// The ids only end up in role strings, so their value does not matter.
func PlaceholderRouterIDs(names ...string) map[string]string {
	out := make(map[string]string, len(names))
	for _, n := range names {
		if n != "" {
			out[n] = n
		}
	}
	return out
}

// AppRouterNames collects every router name a ZitiApp spec and its connection can reference.
func AppRouterNames(svc *zitiv1.ZitiApp, conn *zitiv1.ZitiConnection) []string {
	names := make([]string, 0, len(svc.Spec.EntryRouters)+len(conn.Spec.HostingRouters)+len(conn.Spec.DefaultEdgeRouters)+1+len(svc.Spec.HostedByAnyOf))
	names = append(names, conn.Spec.HostingRouters...)
	names = append(names, conn.Spec.DefaultEdgeRouters...)
	names = append(names, svc.Spec.EntryRouters...)
	names = append(names, svc.Spec.HostedBy)
	names = append(names, svc.Spec.HostedByAnyOf...)
	return names
}
