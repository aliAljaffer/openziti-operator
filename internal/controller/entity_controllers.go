// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	zitiv1 "github.com/aliAljaffer/openziti-operator/api/v1alpha1"
	"github.com/aliAljaffer/openziti-operator/internal/check"
	"github.com/aliAljaffer/openziti-operator/internal/desired"
	"github.com/aliAljaffer/openziti-operator/internal/ziti"
)

const CondInUse = "InUse"

func buildConfig(res *resolver, o *zitiv1.ZitiConfig, conn *zitiv1.ZitiConnection) (ziti.Entity, error) {
	typeID, ok := res.lookup(ziti.ConfigTypes, o.Spec.Type)
	if !ok {
		return nil, fmt.Errorf("config type %q not found in Ziti", o.Spec.Type)
	}
	return desired.ConfigEntity(o, conn, typeID)
}

func buildService(res *resolver, o *zitiv1.ZitiService, conn *zitiv1.ZitiConnection) (ziti.Entity, error) {
	ids := make([]string, 0, len(o.Spec.Configs))
	for _, name := range o.Spec.Configs {
		id, ok := res.lookup(ziti.Configs, name)
		if !ok {
			return nil, &desired.MissingError{Msg: fmt.Sprintf("configs: no config named %q in Ziti", name)}
		}
		ids = append(ids, id)
	}
	return desired.ServiceEntity(o, conn, ids)
}

func buildServicePolicy(res *resolver, o *zitiv1.ZitiServicePolicy, conn *zitiv1.ZitiConnection) (ziti.Entity, error) {
	ns := o.Namespace
	identities, err := desired.ResolveRoles(conn, ns, "identityRoles", o.Spec.IdentityRoles, ziti.Identities, res.lookup)
	if err != nil {
		return nil, err
	}
	services, err := desired.ResolveRoles(conn, ns, "serviceRoles", o.Spec.ServiceRoles, ziti.Services, res.lookup)
	if err != nil {
		return nil, err
	}
	posture, err := desired.ResolveRoles(conn, ns, "postureCheckRoles", o.Spec.PostureCheckRoles, ziti.PostureChecks, res.lookup)
	if err != nil {
		return nil, err
	}
	return desired.ServicePolicyEntity(o, conn, identities, services, posture), nil
}

func buildEdgeRouterPolicy(res *resolver, o *zitiv1.ZitiEdgeRouterPolicy, conn *zitiv1.ZitiConnection) (ziti.Entity, error) {
	identities, err := desired.ResolveRoles(conn, o.Namespace, "identityRoles", o.Spec.IdentityRoles, ziti.Identities, res.lookup)
	if err != nil {
		return nil, err
	}
	routers, err := desired.ResolveRoles(conn, o.Namespace, "edgeRouterRoles", o.Spec.EdgeRouterRoles, ziti.EdgeRouters, res.lookup)
	if err != nil {
		return nil, err
	}
	return desired.EdgeRouterPolicyEntity(o, conn, identities, routers), nil
}

func buildServiceEdgeRouterPolicy(res *resolver, o *zitiv1.ZitiServiceEdgeRouterPolicy, conn *zitiv1.ZitiConnection) (ziti.Entity, error) {
	services, err := desired.ResolveRoles(conn, o.Namespace, "serviceRoles", o.Spec.ServiceRoles, ziti.Services, res.lookup)
	if err != nil {
		return nil, err
	}
	routers, err := desired.ResolveRoles(conn, o.Namespace, "edgeRouterRoles", o.Spec.EdgeRouterRoles, ziti.EdgeRouters, res.lookup)
	if err != nil {
		return nil, err
	}
	return desired.ServiceEdgeRouterPolicyEntity(o, conn, services, routers), nil
}

// auditConfig reports a config that no service uses. Ziti has no filter for the services of a config,
// so it reads the services whole.
func auditConfig(ctx context.Context, zc ziti.Client, o *zitiv1.ZitiConfig, id string) ([]networkCond, error) {
	services, err := zc.List(ctx, ziti.Services, "")
	if err != nil {
		return nil, err
	}
	users := check.ConfigUsers(services, id)
	return []networkCond{{CondInUse, len(users) > 0, "InUse", check.UnusedConfig,
		"no service uses this config"}}, nil
}

// SetupEntityControllers registers the reconcilers of the kinds that map one to one to a Ziti object.
func SetupEntityControllers(mgr ctrl.Manager, clients ClientProvider) error {
	c, scheme, rec := mgr.GetClient(), mgr.GetScheme(), mgr.GetEventRecorderFor("ziti-operator")
	for _, setup := range []func() error{
		func() error {
			r := newEntityReconciler(c, scheme, clients, rec, "ziticonfig", ziti.Configs,
				func() *zitiv1.ZitiConfig { return &zitiv1.ZitiConfig{} }, buildConfig)
			r.Audit = auditConfig
			return r.SetupWithManager(mgr)
		},
		func() error {
			return newEntityReconciler(c, scheme, clients, rec, "zitiservice", ziti.Services,
				func() *zitiv1.ZitiService { return &zitiv1.ZitiService{} }, buildService).SetupWithManager(mgr)
		},
		func() error {
			return newEntityReconciler(c, scheme, clients, rec, "zitiservicepolicy", ziti.ServicePolicies,
				func() *zitiv1.ZitiServicePolicy { return &zitiv1.ZitiServicePolicy{} }, buildServicePolicy).SetupWithManager(mgr)
		},
		func() error {
			return newEntityReconciler(c, scheme, clients, rec, "zitiedgerouterpolicy", ziti.EdgeRouterPolicies,
				func() *zitiv1.ZitiEdgeRouterPolicy { return &zitiv1.ZitiEdgeRouterPolicy{} }, buildEdgeRouterPolicy).SetupWithManager(mgr)
		},
		func() error {
			return newEntityReconciler(c, scheme, clients, rec, "zitiserviceedgerouterpolicy", ziti.ServiceEdgeRouterPolicies,
				func() *zitiv1.ZitiServiceEdgeRouterPolicy { return &zitiv1.ZitiServiceEdgeRouterPolicy{} }, buildServiceEdgeRouterPolicy).SetupWithManager(mgr)
		},
	} {
		if err := setup(); err != nil {
			return err
		}
	}
	return nil
}

func newEntityReconciler[T interface {
	client.Object
	zitiv1.EntityObject
}](c client.Client, scheme *runtime.Scheme, clients ClientProvider, rec record.EventRecorder, name string, kind ziti.Kind,
	newObj func() T, build func(*resolver, T, *zitiv1.ZitiConnection) (ziti.Entity, error)) *entityReconciler[T] {
	return &entityReconciler[T]{Client: c, Scheme: scheme, Clients: clients, Recorder: rec, Name: name, New: newObj, Kind: kind, Build: build}
}
