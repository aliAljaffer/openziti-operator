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
	"slices"

	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	zitiv1 "github.com/aliAljaffer/openziti-operator/api/v1alpha1"
	"github.com/aliAljaffer/openziti-operator/internal/desired"
)

const (
	CondIdentityReady = "IdentityReady"
)

// ZitiSidecarReconciler writes the patch that adds a Ziti tunneler to a workload. It owns no Ziti entity, so it
// needs no orphan sweeping and nothing to prune.
type ZitiSidecarReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Clients  ClientProvider
	Recorder record.EventRecorder
	// SecretNamespaces are the namespaces where the operator may use Secrets. Empty means every namespace.
	SecretNamespaces []string
}

// +kubebuilder:rbac:groups=ziti.alialjaffer.com,resources=zitisidecars,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=ziti.alialjaffer.com,resources=zitisidecars/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=ziti.alialjaffer.com,resources=zitisidecars/finalizers,verbs=update

func (r *ZitiSidecarReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var sc zitiv1.ZitiSidecar
	if err := r.Get(ctx, req.NamespacedName, &sc); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if !sc.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, r.finalize(ctx, &sc)
	}
	if controllerutil.AddFinalizer(&sc, Finalizer) {
		if err := r.Update(ctx, &sc); err != nil {
			return ctrl.Result{}, err
		}
	}

	before := sc.Status.DeepCopy()
	conn, _, err := connect(ctx, r.Client, r.Clients, sc.Spec.ConnectionRef)
	if err == nil {
		err = namespaceAllowed(ctx, r.Client, conn, sc.Namespace)
	}
	if err == nil {
		err = r.sync(ctx, &sc, conn)
	}

	var se *specError
	var me *desired.MissingError
	var next ctrl.Result
	switch {
	case err == nil:
		next.RequeueAfter = jitter(serviceResync)
	case errors.As(err, &me):
		markFailed(&sc.Status.Conditions, sc.Generation, "TargetNotFound", me.Error())
		next.RequeueAfter = dependencyRetry
		err = nil
	case errors.As(err, &se):
		markFailed(&sc.Status.Conditions, sc.Generation, se.reason, se.msg)
		next.RequeueAfter = jitter(serviceResync)
		err = nil
	default:
		markFailed(&sc.Status.Conditions, sc.Generation, "Error", err.Error())
	}
	sc.Status.ObservedGeneration = sc.Generation

	if !equality.Semantic.DeepEqual(before, &sc.Status) {
		if uerr := r.Status().Update(ctx, &sc); uerr != nil {
			return ctrl.Result{}, uerr
		}
	}
	return next, err
}

func (r *ZitiSidecarReconciler) finalize(ctx context.Context, sc *zitiv1.ZitiSidecar) error {
	if !controllerutil.ContainsFinalizer(sc, Finalizer) {
		return nil
	}
	if ref := sc.Spec.ManifestSecretRef; ref != nil {
		if err := dropSecretKey(ctx, r.Client, sc, types.NamespacedName{Namespace: ref.Namespace, Name: ref.Name}, desired.SidecarSecretKey); err != nil {
			return err
		}
	}
	controllerutil.RemoveFinalizer(sc, Finalizer)
	return r.Update(ctx, sc)
}

func (r *ZitiSidecarReconciler) sync(ctx context.Context, sc *zitiv1.ZitiSidecar, conn *zitiv1.ZitiConnection) error {
	if conn.Status.ControllerVersion == "" {
		return &specError{"ConnectionNotReady", "the ZitiConnection has not reported a controller version yet"}
	}
	var id zitiv1.ZitiIdentity
	if err := r.Get(ctx, types.NamespacedName{Namespace: sc.Namespace, Name: sc.Spec.IdentityRef}, &id); err != nil {
		return &desired.MissingError{Msg: "identityRef: no ZitiIdentity named " + sc.Spec.IdentityRef + " in this namespace"}
	}

	enrolled := meta.IsStatusConditionTrue(id.Status.Conditions, CondReady)
	sc.Status.ZitiName, sc.Status.IdentityEnrolled = id.Status.ZitiName, enrolled
	msg := ""
	if !enrolled {
		msg = "the identity has no identity file for the tunneler yet"
	}
	setCond(&sc.Status.Conditions, sc.Generation, CondIdentityReady, enrolled, "IdentityReady", "IdentityNotEnrolled", msg)
	ready := enrolled
	if !ready {
		setCond(&sc.Status.Conditions, sc.Generation, CondReady, false, "NotReady", "IdentityNotEnrolled", msg)
	} else if ref := sc.Spec.ManifestSecretRef; ref != nil {
		if len(r.SecretNamespaces) > 0 && !slices.Contains(r.SecretNamespaces, ref.Namespace) {
			return &specError{"SecretNamespaceNotAllowed", "the operator may not use Secrets in namespace " + ref.Namespace}
		}
		patch := desired.SidecarPatch(sc, conn.Status.ControllerVersion)
		if err := upsertOwnedSecretKeys(ctx, r.Client, r.Scheme, sc,
			types.NamespacedName{Namespace: ref.Namespace, Name: ref.Name},
			map[string][]byte{desired.SidecarSecretKey: []byte(patch)}); err != nil {
			return err
		}
		setCond(&sc.Status.Conditions, sc.Generation, CondSynced, true, "Synced", "", "")
		setCond(&sc.Status.Conditions, sc.Generation, CondReady, true, "Ready", "", "")
		return nil
	} else {
		setCond(&sc.Status.Conditions, sc.Generation, CondSynced, true, "Synced", "", "")
		setCond(&sc.Status.Conditions, sc.Generation, CondReady, true, "Reported", "", "no manifestSecretRef, so nothing was written")
	}
	return nil
}

func (r *ZitiSidecarReconciler) SetupWithManager(mgr ctrl.Manager) error {
	b := ctrl.NewControllerManagedBy(mgr).
		For(&zitiv1.ZitiSidecar{}).
		Named("zitisidecar")
	return watchDeps(b, mgr.GetClient(), func() client.ObjectList { return &zitiv1.ZitiSidecarList{} },
		func(o *zitiv1.ZitiSidecar) string { return o.Spec.ConnectionRef }, true).
		WithOptions(controller.Options{MaxConcurrentReconciles: 1}).
		Complete(r)
}
