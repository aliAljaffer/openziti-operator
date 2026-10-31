// SPDX-License-Identifier: Apache-2.0

// Package validate holds the offline spec checks that the reconcilers and the admission webhook share.
// Nothing here talks to the Ziti controller, so admission keeps working while Ziti is down. Checks that
// need the network stay in the reconcilers and keep their own condition reasons.
package validate

import (
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	zitiv1 "github.com/aliAljaffer/openziti-operator/api/v1alpha1"
	"github.com/aliAljaffer/openziti-operator/internal/desired"
)

// App checks a ZitiApp spec. conn may be nil when the referenced ZitiConnection is not installed yet;
// then only the checks that do not need it run.
func App(svc *zitiv1.ZitiApp, conn *zitiv1.ZitiConnection) error {
	b := &desired.Builder{Svc: svc, Conn: conn}
	if err := desired.CheckName(b.ZitiName()); err != nil {
		return err
	}
	for _, t := range svc.Spec.Targets {
		if t.Namespace != "" && t.KubernetesService == "" {
			return fmt.Errorf("targets[].namespace needs kubernetesService")
		}
	}
	groups, err := b.Groups()
	if err != nil {
		return err
	}
	if conn == nil {
		return nil
	}
	b.RouterIDs = desired.PlaceholderRouterIDs(desired.AppRouterNames(svc, conn)...)
	allowed, _, err := b.AllowedRoles(nil)
	if err != nil {
		return err
	}
	return b.CheckShape(groups, allowed)
}

// AccessPolicy checks a ZitiAccessPolicy spec. conn may be nil when the referenced ZitiConnection is not
// installed yet; then only the name is checked.
func AccessPolicy(ap *zitiv1.ZitiAccessPolicy, conn *zitiv1.ZitiConnection) error {
	if err := desired.CheckName(desired.AccessName(ap)); err != nil {
		return err
	}
	if conn == nil {
		return nil
	}
	if _, err := desired.AccessDial(ap, conn); err != nil {
		return err
	}
	ids := desired.PlaceholderRouterIDs(ap.Spec.EdgeRouters...)
	_, err := desired.AccessERP(ap, conn, ids)
	return err
}

// Identity checks a ZitiIdentity spec. conn may be nil when the referenced ZitiConnection is not
// installed yet; then only an explicit zitiName is checked.
func Identity(id *zitiv1.ZitiIdentity, conn *zitiv1.ZitiConnection) error {
	name := id.Spec.ZitiName
	if conn != nil {
		name = desired.IdentityName(id, conn)
	}
	if err := desired.CheckName(name); err != nil {
		return err
	}
	if conn == nil {
		return nil
	}
	_, err := desired.Identity(id, conn, "")
	return err
}

// Connection checks a ZitiConnection spec. It never reads another resource.
func Connection(conn *zitiv1.ZitiConnection) error {
	if conn.Spec.AllowedNamespaces == nil {
		return nil
	}
	if _, err := metav1.LabelSelectorAsSelector(conn.Spec.AllowedNamespaces); err != nil {
		return fmt.Errorf("allowedNamespaces: %w", err)
	}
	return nil
}
