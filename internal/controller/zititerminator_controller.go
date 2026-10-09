// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	zitiv1alpha1 "github.com/aliAljaffer/openziti-operator/api/v1alpha1"
	"github.com/aliAljaffer/openziti-operator/internal/desired"
	"github.com/aliAljaffer/openziti-operator/internal/metrics"
	"github.com/aliAljaffer/openziti-operator/internal/ziti"
)

var terminatorKinds = []ziti.Kind{ziti.Terminators}

// ZitiTerminatorReconciler keeps one static terminator per resource. A terminator has no name, so the ownership tags find it.
type ZitiTerminatorReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Clients  ClientProvider
	Recorder record.EventRecorder
}

// +kubebuilder:rbac:groups=ziti.alialjaffer.com,resources=zititerminators,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=ziti.alialjaffer.com,resources=zititerminators/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=ziti.alialjaffer.com,resources=zititerminators/finalizers,verbs=update

func (r *ZitiTerminatorReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	metrics.Reconciliations.WithLabelValues("zititerminator").Inc()
	var t zitiv1alpha1.ZitiTerminator
	if err := r.Get(ctx, req.NamespacedName, &t); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if !t.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, r.finalize(ctx, &t)
	}
	if controllerutil.AddFinalizer(&t, Finalizer) {
		if err := r.Update(ctx, &t); err != nil {
			return ctrl.Result{}, err
		}
	}

	before := t.Status.DeepCopy()
	conn, zc, err := connect(ctx, r.Client, r.Clients, t.Spec.ConnectionRef)
	if err == nil {
		err = namespaceAllowed(ctx, r.Client, conn, t.Namespace)
	}
	if err == nil {
		err = r.sync(ctx, &t, conn, zc)
	}

	var se *specError
	var me *desired.MissingError
	var next ctrl.Result
	switch {
	case err == nil:
		next.RequeueAfter = jitter(serviceResync)
	case errors.As(err, &me):
		markFailed(&t.Status.Conditions, t.Generation, "TargetNotFound", me.Error())
		next.RequeueAfter = dependencyRetry
		err = nil
	case errors.As(err, &se):
		markFailed(&t.Status.Conditions, t.Generation, se.reason, se.msg)
		next.RequeueAfter = jitter(serviceResync)
		err = nil
	case ziti.IsSpecError(err):
		markFailed(&t.Status.Conditions, t.Generation, "ZitiRejected", err.Error())
		next.RequeueAfter = jitter(serviceResync)
		err = nil
	default:
		markFailed(&t.Status.Conditions, t.Generation, "Error", err.Error())
	}
	t.Status.ObservedGeneration = t.Generation

	if !equality.Semantic.DeepEqual(before, &t.Status) {
		if uerr := r.Status().Update(ctx, &t); uerr != nil {
			return ctrl.Result{}, uerr
		}
	}
	return next, err
}

func (r *ZitiTerminatorReconciler) finalize(ctx context.Context, t *zitiv1alpha1.ZitiTerminator) error {
	if !controllerutil.ContainsFinalizer(t, Finalizer) {
		return nil
	}
	_, zc, err := connect(ctx, r.Client, r.Clients, t.Spec.ConnectionRef)
	if err != nil {
		r.Recorder.Eventf(t, "Warning", "DeleteBlocked", "cannot reach Ziti: %v", err)
		return err
	}
	set, err := newEntitySet(ctx, zc, r.Recorder, t, t.UID, terminatorKinds)
	if err != nil {
		return err
	}
	if t.Spec.DeletionPolicy == zitiv1alpha1.DeletionPolicyOrphan {
		err = set.release(ctx)
	} else {
		err = set.prune(ctx)
	}
	if err != nil {
		return err
	}
	controllerutil.RemoveFinalizer(t, Finalizer)
	return r.Update(ctx, t)
}

func (r *ZitiTerminatorReconciler) sync(ctx context.Context, t *zitiv1alpha1.ZitiTerminator, conn *zitiv1alpha1.ZitiConnection, zc ziti.Client) error {
	if conn.Spec.RoleScope != zitiv1alpha1.RoleScopeGlobal {
		return &specError{"InvalidSpec", "ZitiTerminator needs roleScope Global on the connection, because it names a service and a router of the whole network"}
	}
	if strings.ContainsAny(t.Spec.Service+t.Spec.Router, `"\`) {
		return &specError{"InvalidSpec", "service and router must not contain quotes or backslashes"}
	}
	serviceID, err := idByName(ctx, zc, ziti.Services, t.Spec.Service)
	if err != nil {
		return err
	}
	routerID, err := idByName(ctx, zc, ziti.EdgeRouters, t.Spec.Router)
	if err != nil {
		return err
	}
	body := desired.TerminatorEntity(t, conn, serviceID, routerID)

	set, err := newEntitySet(ctx, zc, r.Recorder, t, t.UID, terminatorKinds)
	if err != nil {
		return err
	}
	var id string
	if ex, ok := set.existing[ziti.Terminators][""]; ok {
		id = ex.ID()
		switch {
		case refID(ex["service"]) != serviceID || refID(ex["router"]) != routerID || ex["binding"] != body["binding"]:
			// Ziti cannot move a terminator to another service, router, or binding. Replace it.
			id = ""
		case ex["address"] != body["address"] || !sameNumber(ex["cost"], t.Spec.Cost) || ex["precedence"] != body["precedence"]:
			if err := zc.Update(ctx, ziti.Terminators, id, body); err != nil {
				return err
			}
			r.Recorder.Eventf(t, "Normal", "Updated", "updated terminator %s", id)
			set.keep[ziti.Terminators][id] = true
		default:
			set.keep[ziti.Terminators][id] = true
		}
	}
	if id == "" {
		if id, err = zc.Create(ctx, ziti.Terminators, body); err != nil {
			return err
		}
		r.Recorder.Eventf(t, "Normal", "Created", "created terminator %s", id)
		set.keep[ziti.Terminators][id] = true
	}
	if err := set.prune(ctx); err != nil {
		return err
	}
	t.Status.ZitiID = id
	setCond(&t.Status.Conditions, t.Generation, CondSynced, true, "Synced", "", "")
	setCond(&t.Status.Conditions, t.Generation, CondReady, true, "Ready", "", "")
	return nil
}

func idByName(ctx context.Context, zc ziti.Client, kind ziti.Kind, name string) (string, error) {
	found, err := zc.List(ctx, kind, fmt.Sprintf(`name="%s"`, name))
	if err != nil {
		return "", err
	}
	if len(found) == 0 {
		return "", &desired.MissingError{Msg: fmt.Sprintf("no %s named %q in Ziti", strings.TrimSuffix(string(kind), "s"), name)}
	}
	return found[0].ID(), nil
}

// refID reads the id of a reference. Ziti returns an object, the request body carries a plain id.
func refID(v any) string {
	if m, ok := v.(map[string]any); ok {
		id, _ := m["id"].(string)
		return id
	}
	id, _ := v.(string)
	return id
}

func sameNumber(actual any, want int32) bool {
	switch v := actual.(type) {
	case float64:
		return int32(v) == want
	case int32:
		return v == want
	case int:
		return int32(v) == want
	}
	return want == 0 && actual == nil
}

func (r *ZitiTerminatorReconciler) SetupWithManager(mgr ctrl.Manager) error {
	b := ctrl.NewControllerManagedBy(mgr).
		For(&zitiv1alpha1.ZitiTerminator{}).
		Named("zititerminator")
	return watchDeps(b, mgr.GetClient(), func() client.ObjectList { return &zitiv1alpha1.ZitiTerminatorList{} },
		func(o *zitiv1alpha1.ZitiTerminator) string { return o.Spec.ConnectionRef }, true).
		WithOptions(controller.Options{MaxConcurrentReconciles: 1}).
		Complete(r)
}
