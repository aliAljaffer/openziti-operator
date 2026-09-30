// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"errors"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	zitiv1 "github.com/aliAljaffer/openziti-operator/api/v1alpha1"
	"github.com/aliAljaffer/openziti-operator/internal/desired"
	"github.com/aliAljaffer/openziti-operator/internal/ziti"
)

// dependencyRetry is how soon to look again when a resource names a Ziti entity that does not exist yet.
// Such resources are often applied together, so waiting the long resync would feel broken.
const dependencyRetry = 30 * time.Second

// +kubebuilder:rbac:groups=ziti.alialjaffer.com,resources=ziticonfigs;zitiservices;zitiservicepolicies;zitiedgerouterpolicies;zitiserviceedgerouterpolicies,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=ziti.alialjaffer.com,resources=ziticonfigs/status;zitiservices/status;zitiservicepolicies/status;zitiedgerouterpolicies/status;zitiserviceedgerouterpolicies/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=ziti.alialjaffer.com,resources=ziticonfigs/finalizers;zitiservices/finalizers;zitiservicepolicies/finalizers;zitiedgerouterpolicies/finalizers;zitiserviceedgerouterpolicies/finalizers,verbs=update

// resolver looks up Ziti entities by name. It lists each kind once per reconcile.
type resolver struct {
	ctx   context.Context
	zc    ziti.Client
	cache map[ziti.Kind]map[string]string
	err   error
}

func (r *resolver) lookup(kind ziti.Kind, name string) (string, bool) {
	if r.cache == nil {
		r.cache = map[ziti.Kind]map[string]string{}
	}
	names, ok := r.cache[kind]
	if !ok {
		names = map[string]string{}
		list, err := r.zc.List(r.ctx, kind, "")
		if err != nil && r.err == nil {
			r.err = err
		}
		for _, e := range list {
			names[e.Name()] = e.ID()
		}
		r.cache[kind] = names
	}
	id, found := names[name]
	return id, found
}

// entityReconciler serves a kind that maps to exactly one Ziti object.
type entityReconciler[T interface {
	client.Object
	zitiv1.EntityObject
}] struct {
	client.Client
	Scheme   *runtime.Scheme
	Clients  ClientProvider
	Recorder record.EventRecorder

	Name string
	New  func() T
	Kind ziti.Kind
	// Build turns the resource into the body for Ziti.
	Build func(res *resolver, obj T, conn *zitiv1.ZitiConnection) (ziti.Entity, error)
}

func (r *entityReconciler[T]) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	obj := r.New()
	if err := r.Get(ctx, req.NamespacedName, obj); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if !obj.GetDeletionTimestamp().IsZero() {
		return ctrl.Result{}, r.finalize(ctx, obj)
	}
	if obj.EntityCommon().ManagementPolicy != zitiv1.ManagementObserve && controllerutil.AddFinalizer(obj, Finalizer) {
		if err := r.Update(ctx, obj); err != nil {
			return ctrl.Result{}, err
		}
	}

	st := obj.EntityState()
	before := st.DeepCopy()
	conn, zc, err := connect(ctx, r.Client, r.Clients, obj.EntityCommon().ConnectionRef)
	if err == nil {
		err = namespaceAllowed(ctx, r.Client, conn, obj.GetNamespace())
	}
	if err == nil {
		err = r.sync(ctx, obj, conn, zc)
	}

	var se *specError
	var me *desired.MissingError
	var next ctrl.Result
	switch {
	case err == nil:
		next.RequeueAfter = jitter(serviceResync)
	case errors.As(err, &me):
		markFailed(&st.Conditions, obj.GetGeneration(), "TargetNotFound", me.Error())
		next.RequeueAfter = dependencyRetry
		err = nil
	case errors.As(err, &se):
		markFailed(&st.Conditions, obj.GetGeneration(), se.reason, se.msg)
		next.RequeueAfter = jitter(serviceResync)
		err = nil
	case ziti.IsSpecError(err):
		markFailed(&st.Conditions, obj.GetGeneration(), "ZitiRejected", err.Error())
		next.RequeueAfter = jitter(serviceResync)
		err = nil
	default:
		markFailed(&st.Conditions, obj.GetGeneration(), "Error", err.Error())
	}
	st.ObservedGeneration = obj.GetGeneration()

	if !equality.Semantic.DeepEqual(before, st) {
		if uerr := r.Status().Update(ctx, obj); uerr != nil {
			return ctrl.Result{}, uerr
		}
	}
	return next, err
}

func (r *entityReconciler[T]) finalize(ctx context.Context, obj T) error {
	if !controllerutil.ContainsFinalizer(obj, Finalizer) {
		return nil
	}
	c := obj.EntityCommon()
	if c.ManagementPolicy == zitiv1.ManagementObserve {
		controllerutil.RemoveFinalizer(obj, Finalizer)
		return r.Update(ctx, obj)
	}
	_, zc, err := connect(ctx, r.Client, r.Clients, c.ConnectionRef)
	if err != nil {
		r.Recorder.Eventf(obj, "Warning", "DeleteBlocked", "cannot reach Ziti: %v", err)
		return err
	}
	set, err := newEntitySet(ctx, zc, r.Recorder, obj, obj.GetUID(), []ziti.Kind{r.Kind})
	if err != nil {
		return err
	}
	if c.DeletionPolicy == zitiv1.DeletionPolicyOrphan {
		err = set.release(ctx)
	} else {
		err = set.prune(ctx)
	}
	if err != nil {
		return err
	}
	controllerutil.RemoveFinalizer(obj, Finalizer)
	return r.Update(ctx, obj)
}

func (r *entityReconciler[T]) sync(ctx context.Context, obj T, conn *zitiv1.ZitiConnection, zc ziti.Client) error {
	name := desired.EntityName(obj.EntityCommon(), &metav1.ObjectMeta{Name: obj.GetName(), Namespace: obj.GetNamespace()})
	if strings.ContainsAny(name, `"\`) {
		return &specError{"InvalidSpec", "zitiName must not contain quotes or backslashes"}
	}
	if obj.EntityCommon().ManagementPolicy == zitiv1.ManagementObserve {
		return r.observe(ctx, obj, zc, name)
	}
	res := &resolver{ctx: ctx, zc: zc}
	body, err := r.Build(res, obj, conn)
	if res.err != nil {
		return res.err
	}
	if err != nil {
		if _, ok := errors.AsType[*desired.MissingError](err); ok {
			return err
		}
		return &specError{"InvalidSpec", err.Error()}
	}

	set, err := newEntitySet(ctx, zc, r.Recorder, obj, obj.GetUID(), []ziti.Kind{r.Kind})
	if err != nil {
		return err
	}
	set.adopt = obj.EntityCommon().ManagementPolicy == zitiv1.ManagementAdopt
	id, err := set.ensure(ctx, r.Kind, body)
	if err != nil {
		return err
	}
	if err := set.prune(ctx); err != nil {
		return err
	}
	st := obj.EntityState()
	st.ZitiID = id
	setCond(&st.Conditions, obj.GetGeneration(), CondSynced, true, "Synced", "", "")
	setCond(&st.Conditions, obj.GetGeneration(), CondReady, true, "Ready", "", "")
	return nil
}

// observe reports on an object that was created outside this operator. It never writes to Ziti.
func (r *entityReconciler[T]) observe(ctx context.Context, obj T, zc ziti.Client, name string) error {
	found, err := zc.List(ctx, r.Kind, `name="`+name+`"`)
	if err != nil {
		return err
	}
	if len(found) == 0 {
		return &specError{"NotFound", string(r.Kind) + " " + `"` + name + `"` + " not found in Ziti"}
	}
	st := obj.EntityState()
	st.ZitiID = found[0].ID()
	setCond(&st.Conditions, obj.GetGeneration(), CondSynced, true, "Observed", "", "")
	setCond(&st.Conditions, obj.GetGeneration(), CondReady, true, "Observed", "", "")
	return nil
}

func (r *entityReconciler[T]) SetupWithManager(mgr ctrl.Manager) error {
	gvk, err := apiutil.GVKForObject(r.New(), mgr.GetScheme())
	if err != nil {
		return err
	}
	gvk.Kind += "List"
	b := ctrl.NewControllerManagedBy(mgr).
		For(r.New()).
		Named(r.Name)
	return watchDeps(b, mgr.GetClient(), func() client.ObjectList {
		l, _ := mgr.GetScheme().New(gvk)
		return l.(client.ObjectList)
	}, func(o T) string { return o.EntityCommon().ConnectionRef }, true).
		WithOptions(controller.Options{MaxConcurrentReconciles: 1}).
		Complete(r)
}
