// SPDX-License-Identifier: Apache-2.0

// Package v1alpha1 holds the validating admission webhooks for the everyday kinds. They run only the
// offline checks that the reconcilers run too, so admission keeps working while Ziti is down. The
// webhook never reads Ziti; checks that need the network stay in the reconcilers.
package v1alpha1

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	zitiv1 "github.com/aliAljaffer/openziti-operator/api/v1alpha1"
	"github.com/aliAljaffer/openziti-operator/internal/validate"
)

// Setup registers the validating webhooks on the manager webhook server. cmd/main.go calls it only when
// --enable-webhook is set, so a default install serves nothing here.
func Setup(mgr ctrl.Manager) error {
	reader := mgr.GetClient()
	if err := ctrl.NewWebhookManagedBy(mgr, &zitiv1.ZitiConnection{}).
		WithValidator(&ZitiConnectionValidator{}).Complete(); err != nil {
		return fmt.Errorf("ZitiConnection webhook: %w", err)
	}
	if err := ctrl.NewWebhookManagedBy(mgr, &zitiv1.ZitiAccessPolicy{}).
		WithValidator(&ZitiAccessPolicyValidator{Client: reader}).Complete(); err != nil {
		return fmt.Errorf("ZitiAccessPolicy webhook: %w", err)
	}
	if err := ctrl.NewWebhookManagedBy(mgr, &zitiv1.ZitiIdentity{}).
		WithValidator(&ZitiIdentityValidator{Client: reader}).Complete(); err != nil {
		return fmt.Errorf("ZitiIdentity webhook: %w", err)
	}
	if err := ctrl.NewWebhookManagedBy(mgr, &zitiv1.ZitiApp{}).
		WithValidator(&ZitiAppValidator{Client: reader}).Complete(); err != nil {
		return fmt.Errorf("ZitiApp webhook: %w", err)
	}
	return nil
}

// connection reads the ZitiConnection a resource points at. A missing or unreadable connection is not a
// denial: resources are often applied together, and a transient read failure must not block edits. The
// caller gets a warning, and the checks that do not need the connection still run.
func connection(ctx context.Context, k client.Reader, name string) (*zitiv1.ZitiConnection, admission.Warnings) {
	if name == "" {
		name = "default"
	}
	var conn zitiv1.ZitiConnection
	err := k.Get(ctx, types.NamespacedName{Name: name}, &conn)
	switch {
	case err == nil:
		return &conn, nil
	case apierrors.IsNotFound(err):
		return nil, admission.Warnings{fmt.Sprintf("ZitiConnection %q does not exist yet, so the checks that need it were skipped", name)}
	default:
		return nil, admission.Warnings{fmt.Sprintf("could not read ZitiConnection %q, so the checks that need it were skipped: %v", name, err)}
	}
}

// ZitiConnectionValidator checks a ZitiConnection spec.
type ZitiConnectionValidator struct{}

var _ admission.Validator[*zitiv1.ZitiConnection] = &ZitiConnectionValidator{}

func (v *ZitiConnectionValidator) ValidateCreate(_ context.Context, conn *zitiv1.ZitiConnection) (admission.Warnings, error) {
	return nil, validate.Connection(conn)
}

func (v *ZitiConnectionValidator) ValidateUpdate(_ context.Context, _, conn *zitiv1.ZitiConnection) (admission.Warnings, error) {
	return nil, validate.Connection(conn)
}

func (v *ZitiConnectionValidator) ValidateDelete(context.Context, *zitiv1.ZitiConnection) (admission.Warnings, error) {
	return nil, nil
}

// ZitiAccessPolicyValidator checks a ZitiAccessPolicy spec.
type ZitiAccessPolicyValidator struct{ Client client.Reader }

var _ admission.Validator[*zitiv1.ZitiAccessPolicy] = &ZitiAccessPolicyValidator{}

func (v *ZitiAccessPolicyValidator) ValidateCreate(ctx context.Context, ap *zitiv1.ZitiAccessPolicy) (admission.Warnings, error) {
	return v.validate(ctx, ap)
}

func (v *ZitiAccessPolicyValidator) ValidateUpdate(ctx context.Context, _, ap *zitiv1.ZitiAccessPolicy) (admission.Warnings, error) {
	return v.validate(ctx, ap)
}

func (v *ZitiAccessPolicyValidator) ValidateDelete(context.Context, *zitiv1.ZitiAccessPolicy) (admission.Warnings, error) {
	return nil, nil
}

func (v *ZitiAccessPolicyValidator) validate(ctx context.Context, ap *zitiv1.ZitiAccessPolicy) (admission.Warnings, error) {
	conn, warnings := connection(ctx, v.Client, ap.Spec.ConnectionRef)
	return warnings, validate.AccessPolicy(ap, conn)
}

// ZitiIdentityValidator checks a ZitiIdentity spec.
type ZitiIdentityValidator struct{ Client client.Reader }

var _ admission.Validator[*zitiv1.ZitiIdentity] = &ZitiIdentityValidator{}

func (v *ZitiIdentityValidator) ValidateCreate(ctx context.Context, id *zitiv1.ZitiIdentity) (admission.Warnings, error) {
	return v.validate(ctx, id)
}

func (v *ZitiIdentityValidator) ValidateUpdate(ctx context.Context, _, id *zitiv1.ZitiIdentity) (admission.Warnings, error) {
	return v.validate(ctx, id)
}

func (v *ZitiIdentityValidator) ValidateDelete(context.Context, *zitiv1.ZitiIdentity) (admission.Warnings, error) {
	return nil, nil
}

func (v *ZitiIdentityValidator) validate(ctx context.Context, id *zitiv1.ZitiIdentity) (admission.Warnings, error) {
	conn, warnings := connection(ctx, v.Client, id.Spec.ConnectionRef)
	return warnings, validate.Identity(id, conn)
}

// ZitiAppValidator checks a ZitiApp spec.
type ZitiAppValidator struct{ Client client.Reader }

var _ admission.Validator[*zitiv1.ZitiApp] = &ZitiAppValidator{}

func (v *ZitiAppValidator) ValidateCreate(ctx context.Context, app *zitiv1.ZitiApp) (admission.Warnings, error) {
	return v.validate(ctx, app)
}

func (v *ZitiAppValidator) ValidateUpdate(ctx context.Context, _, app *zitiv1.ZitiApp) (admission.Warnings, error) {
	return v.validate(ctx, app)
}

func (v *ZitiAppValidator) ValidateDelete(context.Context, *zitiv1.ZitiApp) (admission.Warnings, error) {
	return nil, nil
}

func (v *ZitiAppValidator) validate(ctx context.Context, app *zitiv1.ZitiApp) (admission.Warnings, error) {
	conn, warnings := connection(ctx, v.Client, app.Spec.ConnectionRef)
	return warnings, validate.App(app, conn)
}
