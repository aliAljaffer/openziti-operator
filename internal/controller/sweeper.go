// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"fmt"
	"time"

	apimeta "k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	zitiv1 "github.com/aliAljaffer/openziti-operator/api/v1alpha1"
	"github.com/aliAljaffer/openziti-operator/internal/desired"
	"github.com/aliAljaffer/openziti-operator/internal/metrics"
	"github.com/aliAljaffer/openziti-operator/internal/ziti"
)

type OrphanPolicy string

const (
	OrphanReport OrphanPolicy = "report"
	OrphanDelete OrphanPolicy = "delete"
)

// sweepKinds is every kind the operator creates. Order matters for delete: policies before services before configs.
var sweepKinds = []ziti.Kind{
	ziti.ServicePolicies, ziti.ServiceEdgeRouterPolicies, ziti.EdgeRouterPolicies, ziti.AuthPolicies,
	ziti.Services, ziti.Configs, ziti.ExternalJWTSigners, ziti.CertificateAuthorities, ziti.Identities, ziti.EdgeRouters,
}

// OrphanSweeper finds Ziti entities that carry this cluster's tag but belong to a resource that no longer exists.
// A crash or a removed finalizer can leave them. With policy report it only reports. With policy delete it deletes them.
// Entities that were adopted are released instead of deleted.
type OrphanSweeper struct {
	Reader   client.Reader
	Clients  ClientProvider
	Policy   OrphanPolicy
	Interval time.Duration
	Recorder record.EventRecorder
}

// Start runs until ctx ends. The manager runs it on the leader only.
func (s *OrphanSweeper) Start(ctx context.Context) error {
	log := ctrl.Log.WithName("sweeper")
	for {
		if err := s.SweepAll(ctx); err != nil {
			metrics.SweepErrors.Inc()
			log.Error(err, "Sweep failed")
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(jitter(s.Interval)):
		}
	}
}

func (s *OrphanSweeper) NeedLeaderElection() bool { return true }

func (s *OrphanSweeper) SweepAll(ctx context.Context) error {
	var conns zitiv1.ZitiConnectionList
	if err := s.Reader.List(ctx, &conns); err != nil {
		return err
	}
	for i := range conns.Items {
		if err := s.Sweep(ctx, &conns.Items[i]); err != nil {
			return fmt.Errorf("connection %s: %w", conns.Items[i].Name, err)
		}
	}
	return nil
}

// Sweep checks one connection. It lists Ziti first and Kubernetes second, so an entity that a reconcile creates during
// the sweep always has a live owner in the second list.
func (s *OrphanSweeper) Sweep(ctx context.Context, conn *zitiv1.ZitiConnection) error {
	log := ctrl.Log.WithName("sweeper")
	zc, err := s.Clients.For(ctx, conn)
	if err != nil {
		return err
	}
	cluster := conn.Spec.ClusterID
	if cluster == "" {
		cluster = "default"
	}
	found := map[ziti.Kind][]ziti.Entity{}
	for _, kind := range sweepKinds {
		list, err := zc.List(ctx, kind, fmt.Sprintf(`tags.%s="%s"`, desired.TagCluster, cluster))
		if err != nil {
			return err
		}
		found[kind] = list
	}
	live, err := s.liveOwners(ctx)
	if err != nil {
		return err
	}

	for _, kind := range sweepKinds {
		orphans := 0
		for _, e := range found[kind] {
			uid, _ := e.Tags()[desired.TagUID].(string)
			if live[types.UID(uid)] {
				continue
			}
			orphans++
			ref := fmt.Sprintf("%s/%s/%s", e.Tags()[desired.TagKind], e.Tags()[desired.TagNamespace], e.Tags()[desired.TagName])
			log.Info("Found orphaned entity", "kind", kind, "name", e.Name(), "id", e.ID(), "owner", ref, "policy", s.Policy)
			if s.Policy != OrphanDelete {
				continue
			}
			if isAdopted(e) {
				err = patchTags(ctx, zc, kind, e)
			} else {
				err = zc.Delete(ctx, kind, e.ID())
			}
			if err != nil {
				return err
			}
			log.Info("Removed orphaned entity", "kind", kind, "name", e.Name(), "adopted", isAdopted(e))
		}
		metrics.ManagedEntities.WithLabelValues(string(kind)).Set(float64(len(found[kind])))
		metrics.Orphans.WithLabelValues(string(kind)).Set(float64(orphans))
	}
	return nil
}

func patchTags(ctx context.Context, zc ziti.Client, kind ziti.Kind, e ziti.Entity) error {
	tags := desired.ReleaseTags(e.Tags())
	if kind == ziti.Configs {
		return zc.Update(ctx, kind, e.ID(), ziti.Entity{"name": e.Name(), "configTypeId": e["configTypeId"], "data": e["data"], "tags": tags})
	}
	return zc.Patch(ctx, kind, e.ID(), ziti.Entity{"tags": tags})
}

// ownerLists has one empty list per kind that owns Ziti entities. Add every new owner kind here, or the sweeper
// would take its entities for orphans.
func ownerLists() []client.ObjectList {
	return []client.ObjectList{
		&zitiv1.ZitiAppList{}, &zitiv1.ZitiIdentityList{}, &zitiv1.ZitiAccessPolicyList{},
		&zitiv1.ZitiJwtSignerList{}, &zitiv1.ZitiCAList{}, &zitiv1.ZitiRouterList{},
		&zitiv1.ZitiConfigList{}, &zitiv1.ZitiServiceList{}, &zitiv1.ZitiServicePolicyList{},
		&zitiv1.ZitiEdgeRouterPolicyList{}, &zitiv1.ZitiServiceEdgeRouterPolicyList{},
	}
}

func (s *OrphanSweeper) liveOwners(ctx context.Context) (map[types.UID]bool, error) {
	live := map[types.UID]bool{}
	for _, list := range ownerLists() {
		if err := s.Reader.List(ctx, list); err != nil {
			return nil, err
		}
		items, err := apimeta.ExtractList(list)
		if err != nil {
			return nil, err
		}
		for _, item := range items {
			live[item.(client.Object).GetUID()] = true
		}
	}
	return live, nil
}
