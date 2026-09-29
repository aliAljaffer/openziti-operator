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
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"slices"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/meta"
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
	Finalizer         = "ziti.alialjaffer.com/finalizer"
	CondSynced        = "Synced"
	CondHosted        = "Hosted"
	CondDialable      = "Dialable"
	CondRoutePath     = "RoutePath"
	CondReady         = "Ready"
	serviceResync     = 10 * time.Minute
	maxConcurrentSvcs = 2
)

var deleteOrder = []ziti.Kind{ziti.ServicePolicies, ziti.ServiceEdgeRouterPolicies, ziti.Services, ziti.Configs}

type ZitiServiceReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Clients  ClientProvider
	Recorder record.EventRecorder
}

// +kubebuilder:rbac:groups=ziti.alialjaffer.com,resources=zitiservices,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=ziti.alialjaffer.com,resources=zitiservices/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=ziti.alialjaffer.com,resources=zitiservices/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

type specError struct{ reason, msg string }

func (e *specError) Error() string { return e.msg }

func (r *ZitiServiceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var svc zitiv1alpha1.ZitiService
	if err := r.Get(ctx, req.NamespacedName, &svc); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if !svc.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, r.finalize(ctx, &svc)
	}
	if controllerutil.AddFinalizer(&svc, Finalizer) {
		if err := r.Update(ctx, &svc); err != nil {
			return ctrl.Result{}, err
		}
	}

	before := svc.Status.DeepCopy()
	conn, zc, err := r.connect(ctx, &svc)
	var out *syncResult
	if err == nil {
		out, err = r.sync(ctx, &svc, conn, zc)
	}

	var se *specError
	var next ctrl.Result
	switch {
	case err == nil:
		applyResult(&svc, out)
		next.RequeueAfter = jitter(serviceResync)
	case errors.As(err, &se):
		markFailed(&svc, se.reason, se.msg)
		next.RequeueAfter = jitter(serviceResync)
		err = nil
	case ziti.IsSpecError(err):
		markFailed(&svc, "ZitiRejected", err.Error())
		next.RequeueAfter = jitter(serviceResync)
		err = nil
	default:
		markFailed(&svc, "Error", err.Error())
	}
	svc.Status.ObservedGeneration = svc.Generation

	if !equality.Semantic.DeepEqual(before, &svc.Status) {
		if uerr := r.Status().Update(ctx, &svc); uerr != nil {
			return ctrl.Result{}, uerr
		}
	}
	return next, err
}

func jitter(d time.Duration) time.Duration {
	return d + time.Duration(rand.Int64N(int64(d/10)))
}

func (r *ZitiServiceReconciler) connect(ctx context.Context, svc *zitiv1alpha1.ZitiService) (*zitiv1alpha1.ZitiConnection, ziti.Client, error) {
	return connect(ctx, r.Client, r.Clients, svc.Spec.ConnectionRef)
}

func connect(ctx context.Context, k client.Reader, clients ClientProvider, name string) (*zitiv1alpha1.ZitiConnection, ziti.Client, error) {
	if name == "" {
		name = "default"
	}
	var conn zitiv1alpha1.ZitiConnection
	if err := k.Get(ctx, types.NamespacedName{Name: name}, &conn); err != nil {
		return nil, nil, fmt.Errorf("connection %q: %w", name, err)
	}
	zc, err := clients.For(ctx, &conn)
	return &conn, zc, err
}

func tagFilter(uid types.UID) string {
	return fmt.Sprintf(`tags.%s="%s"`, desired.TagUID, uid)
}

func (r *ZitiServiceReconciler) finalize(ctx context.Context, svc *zitiv1alpha1.ZitiService) error {
	if !controllerutil.ContainsFinalizer(svc, Finalizer) {
		return nil
	}
	if svc.Spec.DeletionPolicy != zitiv1alpha1.DeletionPolicyOrphan {
		_, zc, err := r.connect(ctx, svc)
		if err != nil {
			r.Recorder.Eventf(svc, "Warning", "DeleteBlocked", "cannot reach Ziti: %v", err)
			return err
		}
		for _, kind := range deleteOrder {
			existing, err := zc.List(ctx, kind, tagFilter(svc.UID))
			if err != nil {
				return err
			}
			for _, e := range existing {
				if err := zc.Delete(ctx, kind, e.ID()); err != nil {
					return err
				}
				r.Recorder.Eventf(svc, "Normal", "Deleted", "deleted %s %s", kind, e.Name())
			}
		}
	}
	controllerutil.RemoveFinalizer(svc, Finalizer)
	return r.Update(ctx, svc)
}

type syncResult struct {
	name        string
	ids         zitiv1alpha1.EntityIDs
	terminators []zitiv1alpha1.Terminator
	report      check.ServiceReport
}

func (r *ZitiServiceReconciler) sync(ctx context.Context, svc *zitiv1alpha1.ZitiService, conn *zitiv1alpha1.ZitiConnection, zc ziti.Client) (*syncResult, error) {
	b := &desired.Builder{Svc: svc, Conn: conn}
	name := b.ZitiName()
	if strings.ContainsAny(name, `"\`) {
		return nil, &specError{"InvalidSpec", "zitiName must not contain quotes or backslashes"}
	}

	routers, err := zc.List(ctx, ziti.EdgeRouters, "")
	if err != nil {
		return nil, err
	}
	b.RouterIDs = map[string]string{}
	for _, rt := range routers {
		b.RouterIDs[rt.Name()] = rt.ID()
	}
	configTypes, err := zc.List(ctx, ziti.ConfigTypes, "")
	if err != nil {
		return nil, err
	}
	typeIDs := map[string]string{}
	for _, t := range configTypes {
		typeIDs[t.Name()] = t.ID()
	}
	for _, n := range []string{"intercept.v1", "host.v1"} {
		if typeIDs[n] == "" {
			return nil, fmt.Errorf("config type %s not found in Ziti", n)
		}
	}

	icBody := b.InterceptConfig(typeIDs["intercept.v1"])
	hcBody, err := b.HostConfig(typeIDs["host.v1"])
	if err != nil {
		return nil, &specError{"InvalidSpec", err.Error()}
	}
	if _, err := b.Service("", ""); err != nil {
		return nil, &specError{"InvalidSpec", err.Error()}
	}
	for _, f := range []func(string) (ziti.Entity, error){b.Bind, b.SERP, b.Dial} {
		if _, err := f(""); err != nil {
			return nil, &specError{"InvalidSpec", err.Error()}
		}
	}

	existing := map[ziti.Kind]map[string]ziti.Entity{}
	for _, kind := range deleteOrder {
		list, err := zc.List(ctx, kind, tagFilter(svc.UID))
		if err != nil {
			return nil, err
		}
		existing[kind] = map[string]ziti.Entity{}
		for _, e := range list {
			existing[kind][e.Name()] = e
		}
	}
	keep := map[ziti.Kind]map[string]bool{}
	ensure := func(kind ziti.Kind, body ziti.Entity) (string, error) {
		if keep[kind] == nil {
			keep[kind] = map[string]bool{}
		}
		if ex, ok := existing[kind][body.Name()]; ok {
			keep[kind][ex.ID()] = true
			if !desired.Matches(body, ex) {
				if err := zc.Update(ctx, kind, ex.ID(), body); err != nil {
					return "", err
				}
				r.Recorder.Eventf(svc, "Normal", "Updated", "updated %s %s", kind, body.Name())
			}
			return ex.ID(), nil
		}
		clash, err := zc.List(ctx, kind, fmt.Sprintf(`name="%s"`, body.Name()))
		if err != nil {
			return "", err
		}
		if len(clash) > 0 {
			return "", &specError{"NameConflict", fmt.Sprintf("%s %q already exists and is not managed by this operator", kind, body.Name())}
		}
		id, err := zc.Create(ctx, kind, body)
		if err != nil {
			return "", err
		}
		keep[kind][id] = true
		r.Recorder.Eventf(svc, "Normal", "Created", "created %s %s", kind, body.Name())
		return id, nil
	}

	var ids zitiv1alpha1.EntityIDs
	if ids.Intercept, err = ensure(ziti.Configs, icBody); err != nil {
		return nil, err
	}
	if ids.Host, err = ensure(ziti.Configs, hcBody); err != nil {
		return nil, err
	}
	svcBody, _ := b.Service(ids.Intercept, ids.Host)
	if ids.Service, err = ensure(ziti.Services, svcBody); err != nil {
		return nil, err
	}
	bind, _ := b.Bind(ids.Service)
	if ids.Bind, err = ensure(ziti.ServicePolicies, bind); err != nil {
		return nil, err
	}
	serp, _ := b.SERP(ids.Service)
	if ids.SERP, err = ensure(ziti.ServiceEdgeRouterPolicies, serp); err != nil {
		return nil, err
	}
	if dial, _ := b.Dial(ids.Service); dial != nil {
		if ids.Dial, err = ensure(ziti.ServicePolicies, dial); err != nil {
			return nil, err
		}
	}

	for _, kind := range deleteOrder {
		for _, e := range existing[kind] {
			if keep[kind][e.ID()] {
				continue
			}
			if err := zc.Delete(ctx, kind, e.ID()); err != nil {
				return nil, err
			}
			r.Recorder.Eventf(svc, "Normal", "Deleted", "deleted %s %s", kind, e.Name())
		}
	}

	return r.observe(ctx, zc, name, ids, routers, configTypes, []ziti.Entity{
		withID(icBody, ids.Intercept), withID(hcBody, ids.Host),
	}, withID(svcBody, ids.Service))
}

func withID(e ziti.Entity, id string) ziti.Entity {
	raw, _ := json.Marshal(e)
	var out ziti.Entity
	_ = json.Unmarshal(raw, &out)
	out["id"] = id
	return out
}

func (r *ZitiServiceReconciler) observe(ctx context.Context, zc ziti.Client, name string, ids zitiv1alpha1.EntityIDs,
	routers, configTypes, configs []ziti.Entity, svc ziti.Entity) (*syncResult, error) {
	g := &check.Graph{Services: []ziti.Entity{svc}, Configs: configs, ConfigTypes: configTypes, Routers: routers}
	for _, l := range []struct {
		kind ziti.Kind
		dst  *[]ziti.Entity
	}{
		{ziti.ServicePolicies, &g.ServicePolicies},
		{ziti.ServiceEdgeRouterPolicies, &g.SERPs},
		{ziti.EdgeRouterPolicies, &g.ERPs},
		{ziti.Identities, &g.Identities},
		{ziti.Terminators, &g.Terminators},
	} {
		list, err := zc.List(ctx, l.kind, "")
		if err != nil {
			return nil, err
		}
		*l.dst = list
	}
	out := &syncResult{name: name, ids: ids, report: g.Service(svc)}
	routerNames := map[string]string{}
	for _, rt := range routers {
		routerNames[rt.ID()] = rt.Name()
	}
	for _, t := range g.Terminators {
		if t["serviceId"] == ids.Service {
			rid, _ := t["routerId"].(string)
			out.terminators = append(out.terminators, zitiv1alpha1.Terminator{Router: routerNames[rid]})
		}
	}
	slices.SortFunc(out.terminators, func(a, b zitiv1alpha1.Terminator) int { return strings.Compare(a.Router, b.Router) })
	return out, nil
}

func setCond(svc *zitiv1alpha1.ZitiService, typ string, ok bool, reasonTrue, reasonFalse, msg string) {
	c := metav1.Condition{Type: typ, ObservedGeneration: svc.Generation}
	if ok {
		c.Status, c.Reason = metav1.ConditionTrue, reasonTrue
	} else {
		c.Status, c.Reason, c.Message = metav1.ConditionFalse, reasonFalse, msg
	}
	meta.SetStatusCondition(&svc.Status.Conditions, c)
}

func findingsFor(fs []check.Finding, codes ...string) (string, string) {
	var reason string
	var msgs []string
	for _, f := range fs {
		if slices.Contains(codes, f.Code) {
			if reason == "" {
				reason = f.Code
			}
			msgs = append(msgs, f.Message)
		}
	}
	return reason, strings.Join(msgs, "; ")
}

func applyResult(svc *zitiv1alpha1.ZitiService, o *syncResult) {
	svc.Status.ZitiName, svc.Status.IDs, svc.Status.Terminators = o.name, o.ids, o.terminators
	fs := o.report.Findings
	setCond(svc, CondSynced, true, "Synced", "", "")
	reason, msg := findingsFor(fs, check.NoTerminator, check.NoBind, check.InertBind, check.MissingConfig, check.ProtocolMismatch)
	setCond(svc, CondHosted, o.report.Hosted, "Hosted", reasonOr(reason, "NotHosted"), msg)
	reason, msg = findingsFor(fs, check.NoDialer)
	setCond(svc, CondDialable, o.report.Dialable, "Dialable", reasonOr(reason, "NotDialable"), msg)
	reason, msg = findingsFor(fs, check.OfflinePath, check.NoCommonRouter)
	setCond(svc, CondRoutePath, o.report.RoutePath, "RoutePath", reasonOr(reason, "NoRoutePath"), msg)
	ready := o.report.Hosted && o.report.Dialable && o.report.RoutePath
	var all []string
	for _, f := range fs {
		all = append(all, f.Message)
	}
	setCond(svc, CondReady, ready, "Ready", "NotReady", strings.Join(all, "; "))
}

func reasonOr(r, def string) string {
	if r == "" {
		return def
	}
	return r
}

func markFailed(svc *zitiv1alpha1.ZitiService, reason, msg string) {
	for _, t := range []string{CondSynced, CondReady} {
		meta.SetStatusCondition(&svc.Status.Conditions, metav1.Condition{
			Type: t, Status: metav1.ConditionFalse, Reason: reason, Message: msg, ObservedGeneration: svc.Generation,
		})
	}
}

func (r *ZitiServiceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&zitiv1alpha1.ZitiService{}).
		Named("zitiservice").
		WithOptions(controller.Options{MaxConcurrentReconciles: maxConcurrentSvcs}).
		Complete(r)
}
