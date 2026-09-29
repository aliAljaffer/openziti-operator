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

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	zitiv1alpha1 "github.com/aliAljaffer/openziti-operator/api/v1alpha1"
	"github.com/aliAljaffer/openziti-operator/internal/desired"
)

// ServiceReconciler turns an annotated Kubernetes Service into a ZitiApp that the Service owns.
type ServiceReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch
// +kubebuilder:rbac:groups=ziti.alialjaffer.com,resources=zitiapps,verbs=get;list;watch;create;update;patch;delete

func (r *ServiceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var svc corev1.Service
	if err := r.Get(ctx, req.NamespacedName, &svc); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if !svc.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, nil
	}

	var app zitiv1alpha1.ZitiApp
	err := r.Get(ctx, req.NamespacedName, &app)
	if client.IgnoreNotFound(err) != nil {
		return ctrl.Result{}, err
	}
	exists := err == nil
	owned := exists && metav1.IsControlledBy(&app, &svc)

	if !desired.Exposed(svc.Annotations) {
		if owned {
			r.Recorder.Eventf(&svc, "Normal", "Unexposed", "deleting ZitiApp %s", app.Name)
			return ctrl.Result{}, client.IgnoreNotFound(r.Delete(ctx, &app))
		}
		return ctrl.Result{}, nil
	}
	if exists && !owned {
		r.Recorder.Eventf(&svc, "Warning", "AppConflict", "ZitiApp %s exists and is not owned by this Service", app.Name)
		return ctrl.Result{}, nil
	}

	spec, err := desired.AppSpec(&svc)
	if err != nil {
		r.Recorder.Eventf(&svc, "Warning", "InvalidAnnotations", "%v", err)
		return ctrl.Result{}, nil
	}
	app = zitiv1alpha1.ZitiApp{}
	app.Name, app.Namespace = svc.Name, svc.Namespace
	_, err = controllerutil.CreateOrUpdate(ctx, r.Client, &app, func() error {
		if app.CreationTimestamp.IsZero() {
			app.Spec = spec
		} else {
			// deletionPolicy, zitiName, and defaulted fields are immutable or set by the API server.
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
		return controllerutil.SetControllerReference(&svc, &app, r.Scheme)
	})
	if apierrors.IsInvalid(err) {
		r.Recorder.Eventf(&svc, "Warning", "InvalidAnnotations", "the ZitiApp was rejected: %v", err)
		return ctrl.Result{}, nil
	}
	return ctrl.Result{}, err
}

// watched selects Services that are exposed, or that were exposed before the change.
var watched = predicate.Funcs{
	CreateFunc: func(e event.CreateEvent) bool { return desired.Exposed(e.Object.GetAnnotations()) },
	UpdateFunc: func(e event.UpdateEvent) bool {
		return desired.Exposed(e.ObjectOld.GetAnnotations()) || desired.Exposed(e.ObjectNew.GetAnnotations())
	},
	DeleteFunc:  func(event.DeleteEvent) bool { return false },
	GenericFunc: func(e event.GenericEvent) bool { return desired.Exposed(e.Object.GetAnnotations()) },
}

func (r *ServiceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Service{}, builder.WithPredicates(watched)).
		Owns(&zitiv1alpha1.ZitiApp{}).
		Named("service").
		Complete(r)
}
