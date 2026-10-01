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
	"fmt"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	zitiv1alpha1 "github.com/aliAljaffer/openziti-operator/api/v1alpha1"
	"github.com/aliAljaffer/openziti-operator/internal/providerflags"
)

const (
	ConditionConnected = "Connected"
	connectionResync   = 10 * time.Minute
	connectionRetry    = time.Minute
)

type ZitiConnectionReconciler struct {
	client.Client
	Scheme  *runtime.Scheme
	Clients ClientProvider
	// CreateConnection makes the reconciler create a ZitiConnection named
	// "default" when none exists. The resource has no owner, so it survives the
	// release being uninstalled. The spec is built from ConnectionFlags and
	// Namespace.
	CreateConnection bool
	// ConnectionFlags defines the connection the manager creates and uses.
	ConnectionFlags providerflags.Flags
	// Namespace is the operator namespace, where the CA bundle ConfigMap and, by
	// default, the credential Secret live.
	Namespace string
}

// +kubebuilder:rbac:groups=ziti.alialjaffer.com,resources=ziticonnections,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=ziti.alialjaffer.com,resources=ziticonnections/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=ziti.alialjaffer.com,resources=ziticonnections/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=secrets;configmaps,verbs=get;list;watch

// Reconcile ensures a ZitiConnection exists when Create is set, then reports
// whether the controller is reachable.
func (r *ZitiConnectionReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	if r.CreateConnection {
		if err := r.ensureDefaultConnection(ctx); err != nil {
			return ctrl.Result{}, err
		}
	}
	return r.reconcileConnection(ctx, req)
}

// ensureDefaultConnection creates the "default" ZitiConnection from the manager
// flags when it is missing. It writes nothing if a connection already exists,
// so a user may replace or add their own.
func (r *ZitiConnectionReconciler) ensureDefaultConnection(ctx context.Context) error {
	const name = "default"
	var existing zitiv1alpha1.ZitiConnection
	err := r.Get(ctx, types.NamespacedName{Name: name}, &existing)
	if err == nil || !apierrors.IsNotFound(err) {
		return err
	}
	spec, err := r.ConnectionFlags.Decode(r.Namespace)
	if err != nil {
		return fmt.Errorf("--create-connection: %w", err)
	}
	return r.Create(ctx, &zitiv1alpha1.ZitiConnection{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec:       spec,
	})
}

func (r *ZitiConnectionReconciler) reconcileConnection(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var conn zitiv1alpha1.ZitiConnection
	if err := r.Get(ctx, req.NamespacedName, &conn); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	before := conn.Status.DeepCopy()

	cond := metav1.Condition{Type: ConditionConnected, ObservedGeneration: conn.Generation}
	after := connectionResync
	zc, err := r.Clients.For(ctx, &conn)
	var version string
	if err == nil {
		version, err = zc.Version(ctx)
	}
	if err != nil {
		cond.Status, cond.Reason, cond.Message = metav1.ConditionFalse, "ConnectionFailed", err.Error()
		after = connectionRetry
	} else {
		cond.Status, cond.Reason = metav1.ConditionTrue, "Connected"
		conn.Status.ControllerVersion = version
	}
	meta.SetStatusCondition(&conn.Status.Conditions, cond)

	if !equality.Semantic.DeepEqual(before, &conn.Status) {
		if err := r.Status().Update(ctx, &conn); err != nil {
			return ctrl.Result{}, err
		}
	}
	return ctrl.Result{RequeueAfter: after}, nil
}

func (r *ZitiConnectionReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&zitiv1alpha1.ZitiConnection{}).
		Named("ziticonnection").
		Complete(r)
}
