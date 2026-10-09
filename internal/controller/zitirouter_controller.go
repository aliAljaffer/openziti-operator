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
	"fmt"
	"slices"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	zitiv1alpha1 "github.com/aliAljaffer/openziti-operator/api/v1alpha1"
	"github.com/aliAljaffer/openziti-operator/internal/check"
	"github.com/aliAljaffer/openziti-operator/internal/desired"
	"github.com/aliAljaffer/openziti-operator/internal/ziti"
)

const (
	SecretKeyCompose    = "docker-compose.yml"
	SecretKeyDeployment = "deployment.yaml"
	CondEnrolled        = "Enrolled"
	CondOnline          = "Online"
	CondServing         = "Serving"
)

var routerKinds = []ziti.Kind{ziti.EdgeRouters}

type ZitiRouterReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Clients  ClientProvider
	Recorder record.EventRecorder
	// SecretNamespaces are the namespaces where the operator may use Secrets. Empty means every namespace.
	SecretNamespaces []string
}

// +kubebuilder:rbac:groups=ziti.alialjaffer.com,resources=zitirouters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=ziti.alialjaffer.com,resources=zitirouters/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=ziti.alialjaffer.com,resources=zitirouters/finalizers,verbs=update

func (r *ZitiRouterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var rt zitiv1alpha1.ZitiRouter
	if err := r.Get(ctx, req.NamespacedName, &rt); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if !rt.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, r.finalize(ctx, &rt)
	}
	if controllerutil.AddFinalizer(&rt, Finalizer) {
		if err := r.Update(ctx, &rt); err != nil {
			return ctrl.Result{}, err
		}
	}

	before := rt.Status.DeepCopy()
	conn, zc, err := connect(ctx, r.Client, r.Clients, rt.Spec.ConnectionRef)
	if err == nil {
		err = r.checkSecretNamespace(&rt)
	}
	if err == nil {
		err = r.sync(ctx, &rt, conn, zc)
	}

	var se *specError
	var next ctrl.Result
	switch {
	case err == nil && rt.Status.Enrolled && rt.Status.Online:
		next.RequeueAfter = jitter(serviceResync)
	case err == nil:
		next.RequeueAfter = jitter(pendingRecheck)
	case errors.As(err, &se):
		markFailed(&rt.Status.Conditions, rt.Generation, se.reason, se.msg)
		next.RequeueAfter = jitter(serviceResync)
		err = nil
	case ziti.IsSpecError(err):
		markFailed(&rt.Status.Conditions, rt.Generation, "ZitiRejected", err.Error())
		next.RequeueAfter = jitter(serviceResync)
		err = nil
	default:
		markFailed(&rt.Status.Conditions, rt.Generation, "Error", err.Error())
	}
	rt.Status.ObservedGeneration = rt.Generation

	if !equality.Semantic.DeepEqual(before, &rt.Status) {
		if uerr := r.Status().Update(ctx, &rt); uerr != nil {
			return ctrl.Result{}, uerr
		}
	}
	return next, err
}

func (r *ZitiRouterReconciler) checkSecretNamespace(rt *zitiv1alpha1.ZitiRouter) error {
	ref := rt.Spec.EnrollmentSecretRef
	if ref != nil && len(r.SecretNamespaces) > 0 && !slices.Contains(r.SecretNamespaces, ref.Namespace) {
		return &specError{"SecretNamespaceNotAllowed", fmt.Sprintf("the operator may not use Secrets in namespace %q, add it to rbac.secretNamespaces (--secret-namespaces)", ref.Namespace)}
	}
	return nil
}

func (r *ZitiRouterReconciler) finalize(ctx context.Context, rt *zitiv1alpha1.ZitiRouter) error {
	if !controllerutil.ContainsFinalizer(rt, Finalizer) {
		return nil
	}
	_, zc, err := connect(ctx, r.Client, r.Clients, rt.Spec.ConnectionRef)
	if err != nil {
		r.Recorder.Eventf(rt, "Warning", "DeleteBlocked", "cannot reach Ziti: %v", err)
		return err
	}
	set, err := newEntitySet(ctx, zc, r.Recorder, rt, rt.UID, routerKinds)
	if err != nil {
		return err
	}
	if rt.Spec.DeletionPolicy == zitiv1alpha1.DeletionPolicyOrphan {
		err = set.release(ctx)
	} else {
		err = set.prune(ctx)
	}
	if err != nil {
		return err
	}
	controllerutil.RemoveFinalizer(rt, Finalizer)
	return r.Update(ctx, rt)
}

func (r *ZitiRouterReconciler) sync(ctx context.Context, rt *zitiv1alpha1.ZitiRouter, conn *zitiv1alpha1.ZitiConnection, zc ziti.Client) error {
	name := desired.RouterName(rt, conn)
	if strings.ContainsAny(name, `"\`) {
		return &specError{"InvalidSpec", "zitiName must not contain quotes or backslashes"}
	}
	set, err := newEntitySet(ctx, zc, r.Recorder, rt, rt.UID, routerKinds)
	if err != nil {
		return err
	}
	id, err := set.ensure(ctx, ziti.EdgeRouters, desired.Router(rt, conn))
	if err != nil {
		return err
	}
	if err := set.prune(ctx); err != nil {
		return err
	}
	setCond(&rt.Status.Conditions, rt.Generation, CondSynced, true, "Synced", "", "")

	list, err := zc.List(ctx, ziti.EdgeRouters, tagFilter(rt.UID))
	if err != nil {
		return err
	}
	if len(list) == 0 {
		return errors.New("the router is not in Ziti yet")
	}
	enrolled, _ := list[0]["isVerified"].(bool)
	online, _ := list[0]["isOnline"].(bool)
	rt.Status.RouterID, rt.Status.ZitiName, rt.Status.Enrolled, rt.Status.Online = id, name, enrolled, online

	report, err := servingReport(ctx, zc, list[0])
	if err != nil {
		return err
	}
	missing := report.Missing()
	rt.Status.Services = report.Services
	setCond(&rt.Status.Conditions, rt.Generation, CondServing, len(missing) == 0, "Serving", check.NoTerminator,
		"no terminator for: "+strings.Join(missing, ", "))

	if err := r.deliverEnrollment(ctx, rt, conn, zc, id, enrolled); err != nil {
		return err
	}

	setCond(&rt.Status.Conditions, rt.Generation, CondEnrolled, enrolled, "Enrolled", "PendingEnrollment", "the router has not enrolled yet")
	setCond(&rt.Status.Conditions, rt.Generation, CondOnline, online, "Online", "Offline", "the router is not connected to the controller")
	notReady := "the router has not enrolled yet"
	if enrolled {
		notReady = "the router is not connected to the controller"
	}
	setCond(&rt.Status.Conditions, rt.Generation, CondReady, enrolled && online, "Ready", "NotReady", notReady)
	return nil
}

// servingReport says which services the router terminates and which ones it was picked to terminate.
// Ziti cannot filter terminators by router, so it reads the three kinds whole.
func servingReport(ctx context.Context, zc ziti.Client, rt ziti.Entity) (check.RouterReport, error) {
	var g check.Graph
	err := check.LoadInto(ctx, zc, &g, ziti.Services, ziti.ServiceEdgeRouterPolicies, ziti.Terminators)
	return g.Router(rt), err
}

// deliverEnrollment keeps a valid enrollment JWT in the Secret until the router has enrolled, then removes it.
func (r *ZitiRouterReconciler) deliverEnrollment(ctx context.Context, rt *zitiv1alpha1.ZitiRouter, conn *zitiv1alpha1.ZitiConnection, zc ziti.Client, id string, enrolled bool) error {
	ref := rt.Spec.EnrollmentSecretRef
	var key types.NamespacedName
	if ref != nil {
		key = types.NamespacedName{Namespace: ref.Namespace, Name: ref.Name}
	}
	if enrolled {
		rt.Status.EnrollmentExpiresAt = nil
		if ref == nil {
			return nil
		}
		return dropSecretKey(ctx, r.Client, rt, key, SecretKeyJWT, SecretKeyCompose, SecretKeyDeployment)
	}

	jwt, expires, err := zc.Enrollment(ctx, ziti.EdgeRouters, id)
	if err != nil {
		return err
	}
	if jwt == "" || (!expires.IsZero() && expires.Before(time.Now())) {
		if err := zc.ReEnroll(ctx, ziti.EdgeRouters, id); err != nil {
			return err
		}
		r.Recorder.Eventf(rt, "Normal", "EnrollmentRenewed", "issued a new enrollment JWT")
		if jwt, expires, err = zc.Enrollment(ctx, ziti.EdgeRouters, id); err != nil {
			return err
		}
	}
	if !expires.IsZero() {
		rt.Status.EnrollmentExpiresAt = &metav1.Time{Time: expires}
	}
	if ref == nil || jwt == "" {
		return nil
	}
	values := map[string][]byte{SecretKeyJWT: []byte(jwt)}
	if conn.Status.ControllerVersion != "" {
		port := rt.Spec.Port
		if port == 0 {
			port = 3022
		}
		m := desired.RouterManifest{Name: desired.RouterName(rt, conn), JWT: jwt, Address: rt.Spec.AdvertisedAddress, Version: conn.Status.ControllerVersion, Port: port, StorageClass: rt.Spec.StorageClassName}
		values[SecretKeyCompose], values[SecretKeyDeployment] = []byte(m.Compose()), []byte(m.Deployment())
	}
	return upsertOwnedSecretKeys(ctx, r.Client, r.Scheme, rt, key, values)
}

func (r *ZitiRouterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	b := ctrl.NewControllerManagedBy(mgr).
		For(&zitiv1alpha1.ZitiRouter{}).
		Named("zitirouter")
	return watchDeps(b, mgr.GetClient(), func() client.ObjectList { return &zitiv1alpha1.ZitiRouterList{} },
		func(o *zitiv1alpha1.ZitiRouter) string { return o.Spec.ConnectionRef }, false).
		WithOptions(controller.Options{MaxConcurrentReconciles: 1}).
		Complete(r)
}
