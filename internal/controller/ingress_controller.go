/*
Copyright 2026 The openziti-operator Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	zitiv1alpha1 "github.com/aliAljaffer/openziti-operator/api/v1alpha1"
	"github.com/aliAljaffer/openziti-operator/internal/desired"
)

// IngressReconciler creates one ZitiApp for an annotated HTTP Ingress with one Service backend.
type IngressReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

// +kubebuilder:rbac:groups=networking.k8s.io,resources=ingresses,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch
// +kubebuilder:rbac:groups=ziti.alialjaffer.com,resources=zitiapps,verbs=get;list;watch;create;update;patch;delete

func (r *IngressReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var ing networkingv1.Ingress
	if err := r.Get(ctx, req.NamespacedName, &ing); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if !ing.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, nil
	}

	appKey := client.ObjectKey{Namespace: ing.Namespace, Name: ingressAppName(&ing)}
	var app zitiv1alpha1.ZitiApp
	err := r.Get(ctx, appKey, &app)
	if client.IgnoreNotFound(err) != nil {
		return ctrl.Result{}, err
	}
	exists := err == nil
	owned := exists && metav1.IsControlledBy(&app, &ing)

	if !desired.Exposed(ing.Annotations) {
		if owned {
			r.Recorder.Eventf(&ing, "Normal", "Unexposed", "deleting ZitiApp %s", app.Name)
			return ctrl.Result{}, client.IgnoreNotFound(r.Delete(ctx, &app))
		}
		return ctrl.Result{}, nil
	}
	if exists && !owned {
		r.Recorder.Eventf(&ing, "Warning", "AppConflict", "ZitiApp %s exists and is not owned by this Ingress", app.Name)
		return ctrl.Result{}, nil
	}

	backendName, err := desired.IngressBackendName(&ing)
	if err != nil {
		r.Recorder.Eventf(&ing, "Warning", "InvalidAnnotations", "%v", err)
		return ctrl.Result{}, nil
	}
	var backend corev1.Service
	if err := r.Get(ctx, client.ObjectKey{Namespace: ing.Namespace, Name: backendName}, &backend); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{RequeueAfter: dependencyRetry}, nil
		}
		return ctrl.Result{}, err
	}
	spec, err := desired.IngressAppSpec(&ing, &backend)
	if err != nil {
		r.Recorder.Eventf(&ing, "Warning", "InvalidAnnotations", "%v", err)
		return ctrl.Result{}, nil
	}
	app = zitiv1alpha1.ZitiApp{}
	app.Name, app.Namespace = ingressAppName(&ing), ing.Namespace
	_, err = controllerutil.CreateOrUpdate(ctx, r.Client, &app, func() error {
		if app.CreationTimestamp.IsZero() {
			app.Spec = spec
		} else {
			keep := app.Spec
			app.Spec = spec
			app.Spec.DeletionPolicy, app.Spec.ManagementPolicy = keep.DeletionPolicy, keep.ManagementPolicy
			if spec.ZitiName == "" {
				app.Spec.ZitiName = keep.ZitiName
			}
			if spec.ConnectionRef == "" {
				app.Spec.ConnectionRef = keep.ConnectionRef
			}
		}
		return controllerutil.SetControllerReference(&ing, &app, r.Scheme)
	})
	if apierrors.IsInvalid(err) {
		r.Recorder.Eventf(&ing, "Warning", "InvalidAnnotations", "the ZitiApp was rejected: %v", err)
		return ctrl.Result{}, nil
	}
	return ctrl.Result{}, err
}

func ingressAppName(ing *networkingv1.Ingress) string { return "ingress-" + ing.Name }

var watchedIngress = predicate.Funcs{
	CreateFunc: func(e event.CreateEvent) bool { return desired.Exposed(e.Object.GetAnnotations()) },
	UpdateFunc: func(e event.UpdateEvent) bool {
		return desired.Exposed(e.ObjectOld.GetAnnotations()) || desired.Exposed(e.ObjectNew.GetAnnotations())
	},
	DeleteFunc:  func(event.DeleteEvent) bool { return false },
	GenericFunc: func(e event.GenericEvent) bool { return desired.Exposed(e.Object.GetAnnotations()) },
}

func (r *IngressReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&networkingv1.Ingress{}, builder.WithPredicates(watchedIngress)).
		Watches(&corev1.Service{}, handler.EnqueueRequestsFromMapFunc(r.ingressesForService)).
		Owns(&zitiv1alpha1.ZitiApp{}).
		Named("ingress").
		Complete(r)
}

func (r *IngressReconciler) ingressesForService(ctx context.Context, obj client.Object) []reconcile.Request {
	var list networkingv1.IngressList
	if err := r.List(ctx, &list, client.InNamespace(obj.GetNamespace())); err != nil {
		return nil
	}
	var out []reconcile.Request
	for i := range list.Items {
		ing := &list.Items[i]
		if !desired.Exposed(ing.Annotations) {
			continue
		}
		name, err := desired.IngressBackendName(ing)
		if err == nil && name == obj.GetName() {
			out = append(out, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(ing)})
		}
	}
	return out
}
