// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	zitiv1 "github.com/aliAljaffer/openziti-operator/api/v1alpha1"
)

var conditionsDesc = prometheus.NewDesc("ziti_operator_resource_conditions",
	"Custom resources by kind, condition type, and condition status.", []string{"kind", "type", "status"}, nil)

// ConditionCollector counts conditions of every custom resource at scrape time. It reads the manager cache.
type ConditionCollector struct{ Reader client.Reader }

func (c *ConditionCollector) Describe(ch chan<- *prometheus.Desc) { ch <- conditionsDesc }

func (c *ConditionCollector) Collect(ch chan<- prometheus.Metric) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	count := map[[3]string]float64{}
	add := func(kind string, conds []metav1.Condition) {
		for _, cond := range conds {
			count[[3]string{kind, cond.Type, string(cond.Status)}]++
		}
	}
	var apps zitiv1.ZitiAppList
	if c.Reader.List(ctx, &apps) == nil {
		for _, o := range apps.Items {
			add("ZitiApp", o.Status.Conditions)
		}
	}
	var ids zitiv1.ZitiIdentityList
	if c.Reader.List(ctx, &ids) == nil {
		for _, o := range ids.Items {
			add("ZitiIdentity", o.Status.Conditions)
		}
	}
	var aps zitiv1.ZitiAccessPolicyList
	if c.Reader.List(ctx, &aps) == nil {
		for _, o := range aps.Items {
			add("ZitiAccessPolicy", o.Status.Conditions)
		}
	}
	var conns zitiv1.ZitiConnectionList
	if c.Reader.List(ctx, &conns) == nil {
		for _, o := range conns.Items {
			add("ZitiConnection", o.Status.Conditions)
		}
	}
	for k, n := range count {
		ch <- prometheus.MustNewConstMetric(conditionsDesc, prometheus.GaugeValue, n, k[0], k[1], k[2])
	}
}
