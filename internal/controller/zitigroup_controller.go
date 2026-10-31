// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"fmt"
	"slices"

	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	zitiv1 "github.com/aliAljaffer/openziti-operator/api/v1alpha1"
	"github.com/aliAljaffer/openziti-operator/internal/desired"
)

type ZitiGroupReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

// +kubebuilder:rbac:groups=ziti.alialjaffer.com,resources=zitigroups,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=ziti.alialjaffer.com,resources=zitigroups/status,verbs=get;update;patch

func (r *ZitiGroupReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var group zitiv1.ZitiGroup
	if err := r.Get(ctx, req.NamespacedName, &group); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if !group.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, nil
	}
	before := group.Status.DeepCopy()
	var ids zitiv1.ZitiIdentityList
	if err := r.List(ctx, &ids, client.InNamespace(group.Namespace)); err != nil {
		return ctrl.Result{}, err
	}
	members := desired.GroupMembers(&group, ids.Items)
	group.Status.Members = members
	group.Status.MemberCount = len(members)
	group.Status.ObservedGeneration = group.Generation

	var missing []string
	for _, name := range group.Spec.Members {
		if !slices.ContainsFunc(ids.Items, func(id zitiv1.ZitiIdentity) bool { return id.Name == name }) {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		msg := fmt.Sprintf("identities not found: %v", missing)
		setCond(&group.Status.Conditions, group.Generation, "Ready", false, "", "MemberNotFound", msg)
		r.Recorder.Eventf(&group, "Warning", "MemberNotFound", "%s", msg)
	} else {
		setCond(&group.Status.Conditions, group.Generation, "Ready", true, "Ready", "", "")
	}
	if !equality.Semantic.DeepEqual(before, &group.Status) {
		if err := r.Status().Update(ctx, &group); err != nil {
			return ctrl.Result{}, err
		}
	}
	return ctrl.Result{}, nil
}

func (r *ZitiGroupReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&zitiv1.ZitiGroup{}).
		Watches(&zitiv1.ZitiIdentity{}, handler.EnqueueRequestsFromMapFunc(r.groupsForIdentity)).
		Named("zitigroup").
		Complete(r)
}

func (r *ZitiGroupReconciler) groupsForIdentity(ctx context.Context, obj client.Object) []reconcile.Request {
	var groups zitiv1.ZitiGroupList
	if err := r.List(ctx, &groups, client.InNamespace(obj.GetNamespace())); err != nil {
		return nil
	}
	out := make([]reconcile.Request, 0, len(groups.Items))
	for i := range groups.Items {
		out = append(out, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(&groups.Items[i])})
	}
	return out
}
