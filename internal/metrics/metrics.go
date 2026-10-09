// SPDX-License-Identifier: Apache-2.0

// Package metrics holds the operator's Prometheus metrics. Reconcile duration and errors come from controller-runtime.
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	ctrlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"
)

var (
	// APIRequests counts calls to the Ziti Edge Management API. code is the HTTP status, or "error" without a response.
	APIRequests = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "ziti_operator_api_requests_total",
		Help: "Calls to the Ziti Edge Management API by method, entity kind, and result code.",
	}, []string{"method", "kind", "code"})

	// ManagedEntities is the number of Ziti entities that carry this operator's cluster tag, by kind. The sweeper sets it.
	ManagedEntities = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "ziti_operator_managed_entities",
		Help: "Ziti entities tagged for this cluster, by kind.",
	}, []string{"kind"})

	// Orphans is the number of tagged entities whose owning resource no longer exists, by kind.
	Orphans = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "ziti_operator_orphaned_entities",
		Help: "Tagged Ziti entities whose owner resource is gone, by kind.",
	}, []string{"kind"})

	// SweepErrors counts failed sweeps.
	SweepErrors = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "ziti_operator_sweep_errors_total",
		Help: "Orphan sweeps that failed.",
	})

	// Reconciliations counts reconcile loops per custom resource kind.
	Reconciliations = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "ziti_operator_reconcile_total",
		Help: "Reconcile loops per custom resource kind.",
	}, []string{"kind"})
)

func init() {
	ctrlmetrics.Registry.MustRegister(APIRequests, ManagedEntities, Orphans, SweepErrors, Reconciliations)
}
