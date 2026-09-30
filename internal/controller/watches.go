// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	zitiv1 "github.com/aliAljaffer/openziti-operator/api/v1alpha1"
)

// watchDeps re-queues dependents when their ZitiConnection changes, and, for namespaced kinds,
// when a namespace label changes (allowedNamespaces).
func watchDeps[T client.Object](b *builder.Builder, c client.Client, newList func() client.ObjectList, ref func(T) string, namespaced bool) *builder.Builder {
	requests := func(ctx context.Context, keep func(T) bool, opts ...client.ListOption) []reconcile.Request {
		return listRequests(ctx, c, newList, keep, opts...)
	}
	b = b.Watches(&zitiv1.ZitiConnection{}, handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, o client.Object) []reconcile.Request {
		return requests(ctx, func(t T) bool {
			return refersTo(ref(t), o.GetName())
		})
	}))
	if namespaced {
		b = b.Watches(&corev1.Namespace{}, handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, o client.Object) []reconcile.Request {
			return requests(ctx, func(T) bool { return true }, client.InNamespace(o.GetName()))
		}), builder.WithPredicates(predicate.LabelChangedPredicate{}))
	}
	return b
}

func listRequests[T client.Object](ctx context.Context, c client.Reader, newList func() client.ObjectList, keep func(T) bool, opts ...client.ListOption) []reconcile.Request {
	l := newList()
	if err := c.List(ctx, l, opts...); err != nil {
		return nil
	}
	var out []reconcile.Request
	_ = meta.EachListItem(l, func(o runtime.Object) error {
		obj := o.(T)
		if keep(obj) {
			out = append(out, reconcile.Request{NamespacedName: types.NamespacedName{Namespace: obj.GetNamespace(), Name: obj.GetName()}})
		}
		return nil
	})
	return out
}

func refersTo(connectionRef, name string) bool {
	if connectionRef == "" {
		connectionRef = "default"
	}
	return connectionRef == name
}
