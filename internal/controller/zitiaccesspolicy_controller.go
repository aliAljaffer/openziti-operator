/*
Copyright 2026 The openziti-operator Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"errors"
	"strings"

	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	zitiv1alpha1 "github.com/aliAljaffer/openziti-operator/api/v1alpha1"
	"github.com/aliAljaffer/openziti-operator/internal/check"
	"github.com/aliAljaffer/openziti-operator/internal/desired"
	"github.com/aliAljaffer/openziti-operator/internal/metrics"
	"github.com/aliAljaffer/openziti-operator/internal/ziti"
)

var accessKinds = []ziti.Kind{ziti.ServicePolicies, ziti.EdgeRouterPolicies}

type ZitiAccessPolicyReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Clients  ClientProvider
	Recorder record.EventRecorder
}

// +kubebuilder:rbac:groups=ziti.alialjaffer.com,resources=zitiaccesspolicies,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=ziti.alialjaffer.com,resources=zitiaccesspolicies/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=ziti.alialjaffer.com,resources=zitiaccesspolicies/finalizers,verbs=update

func (r *ZitiAccessPolicyReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	metrics.Reconciliations.WithLabelValues("zitiaccesspolicy").Inc()
	var ap zitiv1alpha1.ZitiAccessPolicy
	if err := r.Get(ctx, req.NamespacedName, &ap); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if !ap.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, r.finalize(ctx, &ap)
	}
	if controllerutil.AddFinalizer(&ap, Finalizer) {
		if err := r.Update(ctx, &ap); err != nil {
			return ctrl.Result{}, err
		}
	}

	before := ap.Status.DeepCopy()
	conn, zc, err := connect(ctx, r.Client, r.Clients, ap.Spec.ConnectionRef)
	if err == nil {
		err = namespaceAllowed(ctx, r.Client, conn, ap.Namespace)
	}
	if err == nil {
		err = r.sync(ctx, &ap, conn, zc)
	}

	var se *specError
	var next ctrl.Result
	switch {
	case err == nil:
		next.RequeueAfter = jitter(serviceResync)
	case errors.As(err, &se):
		markFailed(&ap.Status.Conditions, ap.Generation, se.reason, se.msg)
		next.RequeueAfter = jitter(serviceResync)
		err = nil
	case ziti.IsSpecError(err):
		markFailed(&ap.Status.Conditions, ap.Generation, "ZitiRejected", err.Error())
		next.RequeueAfter = jitter(serviceResync)
		err = nil
	default:
		markFailed(&ap.Status.Conditions, ap.Generation, "Error", err.Error())
	}
	ap.Status.ObservedGeneration = ap.Generation

	if !equality.Semantic.DeepEqual(before, &ap.Status) {
		if uerr := r.Status().Update(ctx, &ap); uerr != nil {
			return ctrl.Result{}, uerr
		}
	}
	return next, err
}

func (r *ZitiAccessPolicyReconciler) finalize(ctx context.Context, ap *zitiv1alpha1.ZitiAccessPolicy) error {
	if !controllerutil.ContainsFinalizer(ap, Finalizer) {
		return nil
	}
	_, zc, err := connect(ctx, r.Client, r.Clients, ap.Spec.ConnectionRef)
	if err != nil {
		r.Recorder.Eventf(ap, "Warning", "DeleteBlocked", "cannot reach Ziti: %v", err)
		return err
	}
	set, err := newEntitySet(ctx, zc, r.Recorder, ap, ap.UID, accessKinds)
	if err != nil {
		return err
	}
	if ap.Spec.DeletionPolicy == zitiv1alpha1.DeletionPolicyOrphan {
		err = set.release(ctx)
	} else {
		err = set.prune(ctx)
	}
	if err != nil {
		return err
	}
	controllerutil.RemoveFinalizer(ap, Finalizer)
	return r.Update(ctx, ap)
}

func (r *ZitiAccessPolicyReconciler) sync(ctx context.Context, ap *zitiv1alpha1.ZitiAccessPolicy, conn *zitiv1alpha1.ZitiConnection, zc ziti.Client) error {
	name := desired.AccessName(ap)
	if strings.ContainsAny(name, `"\`) {
		return &specError{"InvalidSpec", "zitiName must not contain quotes or backslashes"}
	}
	routers, err := zc.List(ctx, ziti.EdgeRouters, "")
	if err != nil {
		return err
	}
	routerIDs := map[string]string{}
	for _, rt := range routers {
		routerIDs[rt.Name()] = rt.ID()
	}
	dial, err := desired.AccessDial(ap, conn)
	if err != nil {
		return &specError{"InvalidSpec", err.Error()}
	}
	erp, err := desired.AccessERP(ap, conn, routerIDs)
	if err != nil {
		return &specError{"InvalidSpec", err.Error()}
	}

	set, err := newEntitySet(ctx, zc, r.Recorder, ap, ap.UID, accessKinds)
	if err != nil {
		return err
	}
	dialID, err := set.ensure(ctx, ziti.ServicePolicies, dial)
	if err != nil {
		return err
	}
	var erpID string
	if erp != nil {
		if erpID, err = set.ensure(ctx, ziti.EdgeRouterPolicies, erp); err != nil {
			return err
		}
	}
	if err := set.prune(ctx); err != nil {
		return err
	}

	identities, err := zc.List(ctx, ziti.Identities, "")
	if err != nil {
		return err
	}
	services, err := zc.List(ctx, ziti.Services, "")
	if err != nil {
		return err
	}
	ap.Status.ZitiName, ap.Status.DialPolicyID, ap.Status.EdgeRouterPolicyID = name, dialID, erpID
	dial = withID(dial, dialID)
	ap.Status.Identities = int32(len(check.Selected(identities, dial, "identityRoles")))
	ap.Status.Services = int32(len(check.Selected(services, dial, "serviceRoles")))

	setCond(&ap.Status.Conditions, ap.Generation, CondSynced, true, "Synced", "", "")
	var missing []string
	if ap.Status.Identities == 0 {
		missing = append(missing, "no identity matches identityRoles")
	}
	if ap.Status.Services == 0 {
		missing = append(missing, "no service matches serviceRoles")
	}
	setCond(&ap.Status.Conditions, ap.Generation, CondReady, len(missing) == 0, "Active", "NoMatch", strings.Join(missing, "; "))
	return nil
}

func (r *ZitiAccessPolicyReconciler) SetupWithManager(mgr ctrl.Manager) error {
	b := ctrl.NewControllerManagedBy(mgr).
		For(&zitiv1alpha1.ZitiAccessPolicy{}).
		Named("zitiaccesspolicy")
	return watchDeps(b, mgr.GetClient(), func() client.ObjectList { return &zitiv1alpha1.ZitiAccessPolicyList{} },
		func(o *zitiv1alpha1.ZitiAccessPolicy) string { return o.Spec.ConnectionRef }, true).
		WithOptions(controller.Options{MaxConcurrentReconciles: maxConcurrentSvcs}).
		Complete(r)
}
