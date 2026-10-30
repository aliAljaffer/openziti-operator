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

	corev1 "k8s.io/api/core/v1"
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
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	zitiv1alpha1 "github.com/aliAljaffer/openziti-operator/api/v1alpha1"
	"github.com/aliAljaffer/openziti-operator/internal/check"
	"github.com/aliAljaffer/openziti-operator/internal/desired"
	"github.com/aliAljaffer/openziti-operator/internal/metrics"
	"github.com/aliAljaffer/openziti-operator/internal/ziti"
)

const (
	Finalizer          = "ziti.alialjaffer.com/finalizer"
	ReasonNameConflict = "NameConflict"
	CondSynced         = "Synced"
	CondHosted         = "Hosted"
	CondDialable       = "Dialable"
	CondRoutePath      = "RoutePath"
	CondReady          = "Ready"
	CondAccess         = "AccessResolved"
	maxConcurrentSvcs  = 2
)

var deleteOrder = []ziti.Kind{ziti.ServicePolicies, ziti.ServiceEdgeRouterPolicies, ziti.EdgeRouterPolicies, ziti.Services, ziti.Configs}

type ZitiAppReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Clients  ClientProvider
	Recorder record.EventRecorder
}

// +kubebuilder:rbac:groups=ziti.alialjaffer.com,resources=zitiapps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=ziti.alialjaffer.com,resources=zitiapps/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=ziti.alialjaffer.com,resources=zitiapps/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

type specError struct{ reason, msg string }

func (e *specError) Error() string { return e.msg }

func (r *ZitiAppReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	metrics.Reconciliations.WithLabelValues("zitiapp").Inc()
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
	var missing *desired.MissingError
	var next ctrl.Result
	switch {
	case err == nil:
		applyResult(&svc, out)
		next.RequeueAfter = jitter(ServiceResync)
	case errors.As(err, &missing):
		markFailed(r.Recorder, &svc, &svc.Status.Conditions, svc.Generation, "TargetNotFound", missing.Error())
		next.RequeueAfter = DependencyRetry
		err = nil
	case errors.As(err, &se):
		markFailed(r.Recorder, &svc, &svc.Status.Conditions, svc.Generation, se.reason, se.msg)
		next.RequeueAfter = jitter(ServiceResync)
		err = nil
	case ziti.IsSpecError(err):
		markFailed(r.Recorder, &svc, &svc.Status.Conditions, svc.Generation, "ZitiRejected", err.Error())
		next.RequeueAfter = jitter(ServiceResync)
		err = nil
	default:
		markFailed(r.Recorder, &svc, &svc.Status.Conditions, svc.Generation, "Error", err.Error())
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
	if d < 10 {
		return d
	}
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

type serviceResult struct {
	name        string
	ids         zitiv1alpha1.EntityIDs
	terminators []zitiv1alpha1.Terminator
	report      check.ServiceReport
}

type syncResult struct {
	services []serviceResult
	missing  []string
}

func (r *ZitiAppReconciler) sync(ctx context.Context, svc *zitiv1alpha1.ZitiApp, conn *zitiv1alpha1.ZitiConnection, zc ziti.Client) (*syncResult, error) {
	resolved, err := r.resolveServiceSelector(ctx, svc)
	if err != nil {
		return nil, err
	}
	svc = resolved
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

	groups, err := b.Groups()
	if err != nil {
		return nil, &specError{"InvalidSpec", err.Error()}
	}

	identityIDs, err := r.identityIDs(ctx, zc, svc.Spec.Allow.Identities)
	if err != nil {
		return nil, err
	}
	allowed, missing, err := b.AllowedRoles(identityIDs)
	if err != nil {
		return nil, &specError{"InvalidSpec", err.Error()}
	}
	if err := b.CheckShape(groups, allowed); err != nil {
		return nil, &specError{"InvalidSpec", err.Error()}
	}

	set, err := newEntitySet(ctx, zc, r.Recorder, svc, svc.UID, deleteOrder)
	if err != nil {
		return nil, err
	}
	set.adopt = svc.Spec.ManagementPolicy == zitiv1alpha1.ManagementAdopt

	results := make([]groupResult, 0, len(groups))
	for _, g := range groups {
		res, err := ensureGroup(ctx, b.WithGroup(g), g, typeIDs, allowed, set.ensure)
		if err != nil {
			return nil, err
		}
		results = append(results, res)
	}
	if erp, _ := b.ERP(allowed); erp != nil {
		erpID, err := set.ensure(ctx, ziti.EdgeRouterPolicies, erp)
		if err != nil {
			return nil, err
		}
		results[0].ids.ERP = erpID
	}

	if err := set.prune(ctx); err != nil {
		return nil, err
	}

	out := &syncResult{missing: missing}
	for _, res := range results {
		sr, err := r.observe(ctx, zc, res.group.Name, res.ids, routers, configTypes, []ziti.Entity{
			withID(res.ic, res.ids.Intercept), withID(res.hc, res.ids.Host),
		}, withID(res.svc, res.ids.Service))
		if err != nil {
			return nil, err
		}
		out.services = append(out.services, *sr)
	}
	return out, nil
}

type groupResult struct {
	group desired.Group
	ids   zitiv1alpha1.EntityIDs
	ic    ziti.Entity
	hc    ziti.Entity
	svc   ziti.Entity
}

// ensureGroup writes the configs, service, and policies of one group and returns their ids.
func ensureGroup(ctx context.Context, gb *desired.Builder, g desired.Group, typeIDs map[string]string,
	allowed []string, ensure func(context.Context, ziti.Kind, ziti.Entity) (string, error)) (groupResult, error) {
	icBody, _ := gb.InterceptConfig(typeIDs["intercept.v1"])
	hcBody, _ := gb.HostConfig(typeIDs["host.v2"])
	res := groupResult{group: g, ic: icBody, hc: hcBody}
	var err error
	if res.ids.Intercept, err = ensure(ctx, ziti.Configs, icBody); err != nil {
		return res, err
	}
	if res.ids.Host, err = ensure(ctx, ziti.Configs, hcBody); err != nil {
		return res, err
	}
	res.svc, _ = gb.Service(res.ids.Intercept, res.ids.Host)
	if res.ids.Service, err = ensure(ctx, ziti.Services, res.svc); err != nil {
		return res, err
	}
	bind, _ := gb.Bind(res.ids.Service)
	if res.ids.Bind, err = ensure(ctx, ziti.ServicePolicies, bind); err != nil {
		return res, err
	}
	serp, _ := gb.SERP(res.ids.Service)
	if res.ids.SERP, err = ensure(ctx, ziti.ServiceEdgeRouterPolicies, serp); err != nil {
		return res, err
	}
	if dial := gb.Dial(res.ids.Service, allowed); dial != nil {
		if res.ids.Dial, err = ensure(ctx, ziti.ServicePolicies, dial); err != nil {
			return res, err
		}
	}
	return res, nil
}

func (r *ZitiAppReconciler) resolveServiceSelector(ctx context.Context, svc *zitiv1alpha1.ZitiApp) (*zitiv1alpha1.ZitiApp, error) {
	selector := svc.Spec.Expose.Selector
	if selector == nil {
		return svc, nil
	}
	labelSelector, err := metav1.LabelSelectorAsSelector(selector)
	if err != nil || labelSelector.Empty() {
		return nil, &specError{"InvalidSpec", "expose.selector must match at least one Service label"}
	}
	var services corev1.ServiceList
	if err := r.List(ctx, &services, client.InNamespace(svc.Namespace), client.MatchingLabelsSelector{Selector: labelSelector}); err != nil {
		return nil, err
	}
	if len(services.Items) == 0 {
		return nil, &desired.MissingError{Msg: "expose.selector matches no Services in this namespace"}
	}
	resolved := svc.DeepCopy()
	if err := desired.ResolveServiceSelector(&resolved.Spec, svc.Namespace, services.Items); err != nil {
		return nil, &specError{"InvalidSpec", err.Error()}
	}
	return resolved, nil
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
	sr, err := r.observe(ctx, zc, name, ids, routers, configTypes, configs, svc)
	if err != nil {
		return nil, err
	}
	return &syncResult{services: []serviceResult{*sr}}, nil
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
	routers, configTypes, configs []ziti.Entity, svc ziti.Entity) (*serviceResult, error) {
	g := &check.Graph{Services: []ziti.Entity{svc}, Configs: configs, ConfigTypes: configTypes, Routers: routers}
	err := check.LoadInto(ctx, zc, g,
		ziti.ServicePolicies, ziti.ServiceEdgeRouterPolicies, ziti.EdgeRouterPolicies, ziti.Identities, ziti.Terminators)
	if err != nil {
		return nil, err
	}
	out := &serviceResult{name: name, ids: ids, report: g.Service(svc)}
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
	services := make([]zitiv1alpha1.ServiceStatus, 0, len(o.services))
	var fs []check.Finding
	hosted, dialable, routePath := len(o.services) > 0, len(o.services) > 0, len(o.services) > 0
	for _, s := range o.services {
		services = append(services, zitiv1alpha1.ServiceStatus{Name: s.name, IDs: s.ids, Terminators: s.terminators})
		hosted = hosted && s.report.Hosted
		dialable = dialable && s.report.Dialable
		routePath = routePath && s.report.RoutePath
		fs = append(fs, s.report.Findings...)
	}
	svc.Status.Services = services
	if len(o.services) > 0 {
		p := o.services[0]
		svc.Status.ZitiName, svc.Status.IDs, svc.Status.Terminators = p.name, p.ids, p.terminators
	}
	setCond(&svc.Status.Conditions, svc.Generation, CondSynced, true, "Synced", "", "")
	reason, msg := findingsFor(fs, check.NoTerminator, check.NoBind, check.InertBind, check.MissingConfig, check.ProtocolMismatch)
	setCond(&svc.Status.Conditions, svc.Generation, CondHosted, hosted, "Hosted", reasonOr(reason, "NotHosted"), msg)
	reason, msg = findingsFor(fs, check.NoDialer)
	setCond(&svc.Status.Conditions, svc.Generation, CondDialable, dialable, "Dialable", reasonOr(reason, "NotDialable"), msg)
	reason, msg = findingsFor(fs, check.OfflinePath, check.NoCommonRouter)
	setCond(&svc.Status.Conditions, svc.Generation, CondRoutePath, routePath, "RoutePath", reasonOr(reason, "NoRoutePath"), msg)
	ready := hosted && dialable && routePath
	all := make([]string, 0, len(fs))
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

func markFailed(rec record.EventRecorder, obj client.Object, conds *[]metav1.Condition, gen int64, reason, msg string) {
	msg = withRemediation(reason, msg)
	if rec != nil {
		rec.Eventf(obj, corev1.EventTypeWarning, reason, "%s", msg)
	}
	for _, t := range []string{CondSynced, CondReady} {
		setCond(conds, gen, t, false, "", reason, msg)
	}
}

var remediationHints = map[string]string{
	"InvalidSpec":                "Fix the spec; nothing was written to Ziti.",
	"TargetNotFound":             "Create the missing entity; the operator checks again soon.",
	"NotFound":                   "Fix zitiName, or create the entity in Ziti.",
	"ZitiRejected":               "Fix the spec; the operator retries later.",
	"Error":                      "The operator retries with backoff.",
	"NamespaceNotAllowed":        "Label the namespace, or change allowedNamespaces.",
	"InvalidConnection":          "Fix the allowedNamespaces selector.",
	"ConnectionFailed":           "Check the Secret, the CA ConfigMap, and the URL.",
	"SecretConflict":             "Set another secretName.",
	"KeyAmbiguous":               "Run the operator in the cluster, or use keys.jwksEndpoint.",
	"SecretNamespaceNotAllowed":  "Add the namespace to rbac.secretNamespaces.",
	"ServiceNamespaceNotAllowed": "Add the namespace to rbac.serviceNamespaces, or apply the Service yourself.",
}

// withRemediation appends the short action from docs/conditions.md to a failure message.
func withRemediation(reason, msg string) string {
	hint := remediationHints[reason]
	if hint == "" || strings.Contains(msg, hint) {
		return msg
	}
	return msg + " " + hint
}

func (r *ZitiAppReconciler) SetupWithManager(mgr ctrl.Manager) error {
	b := ctrl.NewControllerManagedBy(mgr).
		For(&zitiv1alpha1.ZitiApp{}).
		Named("zitiapp")
	b = b.Watches(&corev1.Service{}, handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
		return listRequests(ctx, mgr.GetClient(), func() client.ObjectList { return &zitiv1alpha1.ZitiAppList{} },
			func(app *zitiv1alpha1.ZitiApp) bool { return app.Spec.Expose.Selector != nil },
			client.InNamespace(obj.GetNamespace()))
	}))
	b = b.Watches(&corev1.Service{}, handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
		return listRequests(ctx, mgr.GetClient(), func() client.ObjectList { return &zitiv1alpha1.ZitiAppList{} },
			func(app *zitiv1alpha1.ZitiApp) bool {
				return app.Namespace == obj.GetNamespace() && app.Spec.Expose.Selector != nil
			},
			client.InNamespace(obj.GetNamespace()))
	}))
	return watchDeps(b, mgr.GetClient(), func() client.ObjectList { return &zitiv1alpha1.ZitiAppList{} },
		func(o *zitiv1alpha1.ZitiApp) string { return o.Spec.ConnectionRef }, true).
		WithOptions(controller.Options{MaxConcurrentReconciles: maxConcurrentSvcs}).
		Complete(r)
}
