/*
Copyright 2026 The openziti-operator Authors.

Licensed under the Apache License, Version 2.0 (the "License");
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
	"errors"
	"fmt"
	"slices"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	zitiv1 "github.com/aliAljaffer/openziti-operator/api/v1alpha1"
	"github.com/aliAljaffer/openziti-operator/internal/desired"
	"github.com/aliAljaffer/openziti-operator/internal/ziti"
)

const (
	CondPortForwardIdentity = "IdentityReady"
	CondPortForwardWorkload = "Workload"
)

// ZitiPortForwardReconciler runs a client proxy in a dedicated Deployment that the user can target with
// kubectl port-forward. It does not open a local port on the operator host.
type ZitiPortForwardReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Clients  ClientProvider
	Recorder record.EventRecorder
	// SecretNamespaces are the namespaces where the operator may use Secrets. Empty means every namespace.
	SecretNamespaces []string
}

// +kubebuilder:rbac:groups=ziti.alialjaffer.com,resources=zitiportforwards,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=ziti.alialjaffer.com,resources=zitiportforwards/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=ziti.alialjaffer.com,resources=zitiidentities,verbs=get;list;watch

func (r *ZitiPortForwardReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var pf zitiv1.ZitiPortForward
	if err := r.Get(ctx, req.NamespacedName, &pf); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	before := pf.Status.DeepCopy()
	conn, zc, err := connect(ctx, r.Client, r.Clients, pf.Spec.ConnectionRef)
	if err == nil {
		err = namespaceAllowed(ctx, r.Client, conn, pf.Namespace)
	}
	if err == nil {
		err = r.sync(ctx, &pf, conn)
	}

	var se *specError
	var me *desired.MissingError
	var next ctrl.Result
	switch {
	case err == nil:
		next.RequeueAfter = jitter(serviceResync)
	case errors.As(err, &me):
		markFailed(&pf.Status.Conditions, pf.Generation, "TargetNotFound", me.Error())
		next.RequeueAfter = dependencyRetry
		err = nil
	case errors.As(err, &se):
		markFailed(&pf.Status.Conditions, pf.Generation, se.reason, se.msg)
		next.RequeueAfter = jitter(serviceResync)
		err = nil
	case ziti.IsSpecError(err):
		markFailed(&pf.Status.Conditions, pf.Generation, "ZitiRejected", err.Error())
		next.RequeueAfter = jitter(serviceResync)
		err = nil
	default:
		markFailed(&pf.Status.Conditions, pf.Generation, "Error", err.Error())
	}
	_ = zc
	pf.Status.ObservedGeneration = pf.Generation
	if !equality.Semantic.DeepEqual(before, &pf.Status) {
		if uerr := r.Status().Update(ctx, &pf); uerr != nil {
			return ctrl.Result{}, uerr
		}
	}
	return next, err
}

func (r *ZitiPortForwardReconciler) sync(ctx context.Context, pf *zitiv1.ZitiPortForward, conn *zitiv1.ZitiConnection) error {
	if conn.Status.ControllerVersion == "" {
		return &specError{"ControllerVersionUnknown", "the ZitiConnection has not reported a controller version yet"}
	}
	var identity zitiv1.ZitiIdentity
	if err := r.Get(ctx, types.NamespacedName{Namespace: pf.Namespace, Name: pf.Spec.IdentityRef}, &identity); err != nil {
		return &desired.MissingError{Msg: fmt.Sprintf("identityRef: no ZitiIdentity named %q in this namespace", pf.Spec.IdentityRef)}
	}
	secretName := identity.Spec.SecretName
	if secretName == "" {
		secretName = identity.Name
	}
	if len(r.SecretNamespaces) > 0 && !slices.Contains(r.SecretNamespaces, pf.Namespace) {
		return &specError{"SecretNamespaceNotAllowed", fmt.Sprintf("the operator may not read Secrets in namespace %q, add it to rbac.secretNamespaces (--secret-namespaces)", pf.Namespace)}
	}
	var secret corev1.Secret
	if err := r.Get(ctx, types.NamespacedName{Namespace: pf.Namespace, Name: secretName}, &secret); err != nil ||
		len(secret.Data[SecretKeyIdentity]) == 0 || !meta.IsStatusConditionTrue(identity.Status.Conditions, CondReady) {
		pf.Status.DeploymentName = ""
		setCond(&pf.Status.Conditions, pf.Generation, CondPortForwardIdentity, false, "IdentityReady", "IdentityNotEnrolled", "the identity has no enrolled identity.json yet")
		setCond(&pf.Status.Conditions, pf.Generation, CondPortForwardWorkload, false, "", "IdentityNotEnrolled", "the proxy is stopped until the identity is enrolled")
		setCond(&pf.Status.Conditions, pf.Generation, CondReady, false, "Ready", "IdentityNotEnrolled", "the identity has no enrolled identity.json yet")
		return r.deleteOwnedDeployment(ctx, pf)
	}
	setCond(&pf.Status.Conditions, pf.Generation, CondPortForwardIdentity, true, "IdentityReady", "", "")

	want := desired.PortForwardDeployment(pf, secretName, conn.Status.ControllerVersion)
	var current appsv1.Deployment
	err := r.Get(ctx, client.ObjectKeyFromObject(want), &current)
	if apierrors.IsNotFound(err) {
		if err := controllerutil.SetControllerReference(pf, want, r.Scheme); err != nil {
			return err
		}
		want.Labels[ManagedByLabel] = ManagedByLabelValue
		if err := r.Create(ctx, want); err != nil {
			if apierrors.IsAlreadyExists(err) {
				return &specError{ReasonNameConflict, "a Deployment with this name already exists and is not owned by this ZitiPortForward"}
			}
			return err
		}
		r.Recorder.Eventf(pf, "Normal", "Created", "created proxy Deployment %s/%s", want.Namespace, want.Name)
	} else if err != nil {
		return err
	} else if !metav1.IsControlledBy(&current, pf) {
		return &specError{ReasonNameConflict, "a Deployment with this name already exists and is not owned by this ZitiPortForward"}
	} else {
		_, err := controllerutil.CreateOrUpdate(ctx, r.Client, &current, func() error {
			current.Spec = want.Spec
			current.Labels = want.Labels
			return controllerutil.SetControllerReference(pf, &current, r.Scheme)
		})
		if err != nil {
			return err
		}
	}

	pf.Status.DeploymentName = want.Name
	var observed appsv1.Deployment
	if err := r.Get(ctx, client.ObjectKeyFromObject(want), &observed); err != nil {
		return err
	}
	ready := observed.Status.ReadyReplicas > 0
	message := "the proxy pod is not ready yet"
	if ready {
		message = "run kubectl port-forward deployment/" + want.Name + " " + fmt.Sprintf("%d:%d", pf.Spec.Port, pf.Spec.Port)
	}
	setCond(&pf.Status.Conditions, pf.Generation, CondPortForwardWorkload, ready, "Running", "DeploymentUnavailable", message)
	setCond(&pf.Status.Conditions, pf.Generation, CondReady, ready, "Ready", "NotReady", message)
	return nil
}

func (r *ZitiPortForwardReconciler) deleteOwnedDeployment(ctx context.Context, pf *zitiv1.ZitiPortForward) error {
	var dep appsv1.Deployment
	err := r.Get(ctx, types.NamespacedName{Namespace: pf.Namespace, Name: pf.Name}, &dep)
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !metav1.IsControlledBy(&dep, pf) {
		return nil
	}
	return client.IgnoreNotFound(r.Delete(ctx, &dep))
}

func (r *ZitiPortForwardReconciler) SetupWithManager(mgr ctrl.Manager) error {
	b := ctrl.NewControllerManagedBy(mgr).
		For(&zitiv1.ZitiPortForward{}).
		Owns(&appsv1.Deployment{}).
		Named("zitiportforward")
	b = b.Watches(&zitiv1.ZitiIdentity{}, handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
		var list zitiv1.ZitiPortForwardList
		if err := mgr.GetClient().List(ctx, &list, client.InNamespace(obj.GetNamespace())); err != nil {
			return nil
		}
		var out []reconcile.Request
		for i := range list.Items {
			pf := &list.Items[i]
			if pf.Spec.IdentityRef == obj.GetName() {
				out = append(out, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(pf)})
			}
		}
		return out
	}))
	return watchDeps(b, mgr.GetClient(), func() client.ObjectList { return &zitiv1.ZitiPortForwardList{} },
		func(o *zitiv1.ZitiPortForward) string { return o.Spec.ConnectionRef }, true).
		WithOptions(controller.Options{MaxConcurrentReconciles: 1}).
		Complete(r)
}
