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
	Finalizer         = "alialjaffer.ziti/finalizer"
	CondSynced        = "Synced"
	CondHosted        = "Hosted"
	CondDialable      = "Dialable"
	CondRoutePath     = "RoutePath"
	CondReady         = "Ready"
	CondAccess        = "AccessResolved"
	serviceResync     = 10 * time.Minute
	maxConcurrentSvcs = 2
)

var deleteOrder = []ziti.Kind{ziti.ServicePolicies, ziti.ServiceEdgeRouterPolicies, ziti.EdgeRouterPolicies, ziti.Services, ziti.Configs}

type ZitiAppReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Clients  ClientProvider
	Recorder record.EventRecorder
}

// +kubebuilder:rbac:groups=alialjaffer.ziti,resources=zitiapps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=alialjaffer.ziti,resources=zitiapps/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=alialjaffer.ziti,resources=zitiapps/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

type specError struct{ reason, msg string }

func (e *specError) Error() string { return e.msg }

func (r *ZitiAppReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var svc zitiv1alpha1.ZitiApp
	if err := r.Get(ctx, req.NamespacedName, &svc); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if !svc.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, r.finalize(ctx, &svc)
	}
	if svc.Spec.ManagementPolicy != zitiv1alpha1.ManagementObserve && controllerutil.AddFinalizer(&svc, Finalizer) {
		if err := r.Update(ctx, &svc); err != nil {
			return ctrl.Result{}, err
		}
	}

	before := svc.Status.DeepCopy()
	conn, zc, err := r.connect(ctx, &svc)
	var out *syncResult
	if err == nil {
		err = namespaceAllowed(ctx, r.Client, conn, svc.Namespace)
	}
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
		markFailed(&svc.Status.Conditions, svc.Generation, se.reason, se.msg)
		next.RequeueAfter = jitter(serviceResync)
		err = nil
	case ziti.IsSpecError(err):
		markFailed(&svc.Status.Conditions, svc.Generation, "ZitiRejected", err.Error())
		next.RequeueAfter = jitter(serviceResync)
		err = nil
	default:
		markFailed(&svc.Status.Conditions, svc.Generation, "Error", err.Error())
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

func (r *ZitiAppReconciler) connect(ctx context.Context, svc *zitiv1alpha1.ZitiApp) (*zitiv1alpha1.ZitiConnection, ziti.Client, error) {
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

func (r *ZitiAppReconciler) finalize(ctx context.Context, svc *zitiv1alpha1.ZitiApp) error {
	if !controllerutil.ContainsFinalizer(svc, Finalizer) {
		return nil
	}
	if svc.Spec.ManagementPolicy != zitiv1alpha1.ManagementObserve {
		_, zc, err := r.connect(ctx, svc)
		if err != nil {
			r.Recorder.Eventf(svc, "Warning", "DeleteBlocked", "cannot reach Ziti: %v", err)
			return err
		}
		set, err := newEntitySet(ctx, zc, r.Recorder, svc, svc.UID, deleteOrder)
		if err != nil {
			return err
		}
		if svc.Spec.DeletionPolicy == zitiv1alpha1.DeletionPolicyOrphan {
			err = set.release(ctx)
		} else {
			err = set.prune(ctx)
		}
		if err != nil {
			return err
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
	missing     []string
}

func (r *ZitiAppReconciler) sync(ctx context.Context, svc *zitiv1alpha1.ZitiApp, conn *zitiv1alpha1.ZitiConnection, zc ziti.Client) (*syncResult, error) {
	b := &desired.Builder{Svc: svc, Conn: conn}
	name := b.ZitiName()
	if strings.ContainsAny(name, `"\`) {
		return nil, &specError{"InvalidSpec", "zitiName must not contain quotes or backslashes"}
	}

	routers, err := zc.List(ctx, ziti.EdgeRouters, "")
	if err != nil {
		return nil, err
	}
	if svc.Spec.ManagementPolicy == zitiv1alpha1.ManagementObserve {
		return r.observeExisting(ctx, zc, name, routers)
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
	for _, n := range []string{"intercept.v1", "host.v2"} {
		if typeIDs[n] == "" {
			return nil, fmt.Errorf("config type %s not found in Ziti", n)
		}
	}

	identityIDs, err := r.identityIDs(ctx, zc, svc.Spec.Allow.Identities)
	if err != nil {
		return nil, err
	}
	allowed, missing, err := b.AllowedRoles(identityIDs)
	if err != nil {
		return nil, &specError{"InvalidSpec", err.Error()}
	}

	icBody, err := b.InterceptConfig(typeIDs["intercept.v1"])
	if err != nil {
		return nil, &specError{"InvalidSpec", err.Error()}
	}
	hcBody, err := b.HostConfig(typeIDs["host.v2"])
	if err != nil {
		return nil, &specError{"InvalidSpec", err.Error()}
	}
	if _, err := b.Service("", ""); err != nil {
		return nil, &specError{"InvalidSpec", err.Error()}
	}
	for _, f := range []func(string) (ziti.Entity, error){b.Bind, b.SERP} {
		if _, err := f(""); err != nil {
			return nil, &specError{"InvalidSpec", err.Error()}
		}
	}
	if _, err := b.ERP(allowed); err != nil {
		return nil, &specError{"InvalidSpec", err.Error()}
	}

	set, err := newEntitySet(ctx, zc, r.Recorder, svc, svc.UID, deleteOrder)
	if err != nil {
		return nil, err
	}
	ensure := set.ensure

	var ids zitiv1alpha1.EntityIDs
	if ids.Intercept, err = ensure(ctx, ziti.Configs, icBody); err != nil {
		return nil, err
	}
	if ids.Host, err = ensure(ctx, ziti.Configs, hcBody); err != nil {
		return nil, err
	}
	svcBody, _ := b.Service(ids.Intercept, ids.Host)
	if ids.Service, err = ensure(ctx, ziti.Services, svcBody); err != nil {
		return nil, err
	}
	bind, _ := b.Bind(ids.Service)
	if ids.Bind, err = ensure(ctx, ziti.ServicePolicies, bind); err != nil {
		return nil, err
	}
	serp, _ := b.SERP(ids.Service)
	if ids.SERP, err = ensure(ctx, ziti.ServiceEdgeRouterPolicies, serp); err != nil {
		return nil, err
	}
	if dial := b.Dial(ids.Service, allowed); dial != nil {
		if ids.Dial, err = ensure(ctx, ziti.ServicePolicies, dial); err != nil {
			return nil, err
		}
	}
	if erp, _ := b.ERP(allowed); erp != nil {
		if ids.ERP, err = ensure(ctx, ziti.EdgeRouterPolicies, erp); err != nil {
			return nil, err
		}
	}

	if err := set.prune(ctx); err != nil {
		return nil, err
	}

	out, err := r.observe(ctx, zc, name, ids, routers, configTypes, []ziti.Entity{
		withID(icBody, ids.Intercept), withID(hcBody, ids.Host),
	}, withID(svcBody, ids.Service))
	if out != nil {
		out.missing = missing
	}
	return out, err
}

// identityIDs maps the wanted identity names to Ziti ids. Names Ziti does not know are left out.
func (r *ZitiAppReconciler) identityIDs(ctx context.Context, zc ziti.Client, names []string) (map[string]string, error) {
	out := map[string]string{}
	if len(names) == 0 {
		return out, nil
	}
	all, err := zc.List(ctx, ziti.Identities, "")
	if err != nil {
		return nil, err
	}
	for _, e := range all {
		if slices.Contains(names, e.Name()) {
			out[e.Name()] = e.ID()
		}
	}
	return out, nil
}

// observeExisting reports on a service that was created outside this operator. It never writes to Ziti.
func (r *ZitiAppReconciler) observeExisting(ctx context.Context, zc ziti.Client, name string, routers []ziti.Entity) (*syncResult, error) {
	found, err := zc.List(ctx, ziti.Services, fmt.Sprintf(`name="%s"`, name))
	if err != nil {
		return nil, err
	}
	if len(found) == 0 {
		return nil, &specError{"NotFound", fmt.Sprintf("service %q not found in Ziti", name)}
	}
	svc := found[0]
	configTypes, err := zc.List(ctx, ziti.ConfigTypes, "")
	if err != nil {
		return nil, err
	}
	typeNames := map[string]string{}
	for _, t := range configTypes {
		typeNames[t.ID()] = t.Name()
	}
	all, err := zc.List(ctx, ziti.Configs, "")
	if err != nil {
		return nil, err
	}
	ids := zitiv1alpha1.EntityIDs{Service: svc.ID()}
	var configs []ziti.Entity
	for _, c := range all {
		if !slices.Contains(anyStrings(svc["configs"]), c.ID()) {
			continue
		}
		configs = append(configs, c)
		switch typeNames[fmt.Sprint(c["configTypeId"])] {
		case "intercept.v1":
			ids.Intercept = c.ID()
		case "host.v1", "host.v2":
			ids.Host = c.ID()
		}
	}
	return r.observe(ctx, zc, name, ids, routers, configTypes, configs, svc)
}

func anyStrings(v any) []string {
	l, _ := v.([]any)
	out := make([]string, 0, len(l))
	for _, e := range l {
		if s, ok := e.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func withID(e ziti.Entity, id string) ziti.Entity {
	raw, _ := json.Marshal(e)
	var out ziti.Entity
	_ = json.Unmarshal(raw, &out)
	out["id"] = id
	return out
}

func (r *ZitiAppReconciler) observe(ctx context.Context, zc ziti.Client, name string, ids zitiv1alpha1.EntityIDs,
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

func setCond(conds *[]metav1.Condition, gen int64, typ string, ok bool, reasonTrue, reasonFalse, msg string) {
	c := metav1.Condition{Type: typ, ObservedGeneration: gen}
	if ok {
		c.Status, c.Reason = metav1.ConditionTrue, reasonTrue
	} else {
		c.Status, c.Reason, c.Message = metav1.ConditionFalse, reasonFalse, msg
	}
	meta.SetStatusCondition(conds, c)
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

func applyResult(svc *zitiv1alpha1.ZitiApp, o *syncResult) {
	svc.Status.ZitiName, svc.Status.IDs, svc.Status.Terminators = o.name, o.ids, o.terminators
	fs := o.report.Findings
	setCond(&svc.Status.Conditions, svc.Generation, CondSynced, true, "Synced", "", "")
	reason, msg := findingsFor(fs, check.NoTerminator, check.NoBind, check.InertBind, check.MissingConfig, check.ProtocolMismatch)
	setCond(&svc.Status.Conditions, svc.Generation, CondHosted, o.report.Hosted, "Hosted", reasonOr(reason, "NotHosted"), msg)
	reason, msg = findingsFor(fs, check.NoDialer)
	setCond(&svc.Status.Conditions, svc.Generation, CondDialable, o.report.Dialable, "Dialable", reasonOr(reason, "NotDialable"), msg)
	reason, msg = findingsFor(fs, check.OfflinePath, check.NoCommonRouter)
	setCond(&svc.Status.Conditions, svc.Generation, CondRoutePath, o.report.RoutePath, "RoutePath", reasonOr(reason, "NoRoutePath"), msg)
	ready := o.report.Hosted && o.report.Dialable && o.report.RoutePath
	var all []string
	for _, f := range fs {
		all = append(all, f.Message)
	}
	setCond(&svc.Status.Conditions, svc.Generation, CondReady, ready, "Ready", "NotReady", strings.Join(all, "; "))
	if len(svc.Spec.Allow.Identities) > 0 || len(o.missing) > 0 {
		setCond(&svc.Status.Conditions, svc.Generation, CondAccess, len(o.missing) == 0, "Resolved", "IdentityNotFound",
			"identities not found in Ziti: "+strings.Join(o.missing, ", "))
	} else {
		meta.RemoveStatusCondition(&svc.Status.Conditions, CondAccess)
	}
}

func reasonOr(r, def string) string {
	if r == "" {
		return def
	}
	return r
}

func markFailed(conds *[]metav1.Condition, gen int64, reason, msg string) {
	for _, t := range []string{CondSynced, CondReady} {
		setCond(conds, gen, t, false, "", reason, msg)
	}
}

func (r *ZitiAppReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&zitiv1alpha1.ZitiApp{}).
		Named("zitiapp").
		WithOptions(controller.Options{MaxConcurrentReconciles: maxConcurrentSvcs}).
		Complete(r)
}
