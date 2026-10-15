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

var (
	resourceConditionsDesc = prometheus.NewDesc("ziti_operator_resource_conditions",
		"Custom resources by kind, condition type, and condition status.", []string{"kind", "type", "status"}, nil)
	identityCertExpiresDesc = prometheus.NewDesc("ziti_operator_identity_certificate_expires_seconds",
		"Seconds until the earliest client certificate of a ZitiIdentity expires. Negative when expired.",
		[]string{"namespace", "name"}, nil)
	appServiceTerminatorsDesc = prometheus.NewDesc("ziti_operator_app_service_terminators",
		"Terminators hosting each Ziti service of a ZitiApp.", []string{"namespace", "name", "service"}, nil)
)

// StatusCollector reads every custom resource from the manager cache at scrape time and reports its
// conditions, identity certificate expiry, and app terminators. It computes the values here instead of
// writing them at reconcile time, so a series never outlives the resource that produced it.
type StatusCollector struct{ Reader client.Reader }

func (c *StatusCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- resourceConditionsDesc
	ch <- identityCertExpiresDesc
	ch <- appServiceTerminatorsDesc
}

func (c *StatusCollector) Collect(ch chan<- prometheus.Metric) {
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
			for _, svc := range o.Status.Services {
				ch <- prometheus.MustNewConstMetric(appServiceTerminatorsDesc, prometheus.GaugeValue,
					float64(len(svc.Terminators)), o.Namespace, o.Name, svc.Name)
			}
		}
	}
	var ids zitiv1.ZitiIdentityList
	if c.Reader.List(ctx, &ids) == nil {
		for _, o := range ids.Items {
			add("ZitiIdentity", o.Status.Conditions)
			if o.Status.CertNotAfter != nil {
				ch <- prometheus.MustNewConstMetric(identityCertExpiresDesc, prometheus.GaugeValue,
					time.Until(o.Status.CertNotAfter.Time).Seconds(), o.Namespace, o.Name)
			}
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
	var routers zitiv1.ZitiRouterList
	if c.Reader.List(ctx, &routers) == nil {
		for _, o := range routers.Items {
			add("ZitiRouter", o.Status.Conditions)
		}
	}
	var cas zitiv1.ZitiCAList
	if c.Reader.List(ctx, &cas) == nil {
		for _, o := range cas.Items {
			add("ZitiCA", o.Status.Conditions)
		}
	}
	var signers zitiv1.ZitiJwtSignerList
	if c.Reader.List(ctx, &signers) == nil {
		for _, o := range signers.Items {
			add("ZitiJwtSigner", o.Status.Conditions)
		}
	}
	var cfgs zitiv1.ZitiConfigList
	if c.Reader.List(ctx, &cfgs) == nil {
		for _, o := range cfgs.Items {
			add("ZitiConfig", o.Status.Conditions)
		}
	}
	var svcs zitiv1.ZitiServiceList
	if c.Reader.List(ctx, &svcs) == nil {
		for _, o := range svcs.Items {
			add("ZitiService", o.Status.Conditions)
		}
	}
	var sps zitiv1.ZitiServicePolicyList
	if c.Reader.List(ctx, &sps) == nil {
		for _, o := range sps.Items {
			add("ZitiServicePolicy", o.Status.Conditions)
		}
	}
	var erps zitiv1.ZitiEdgeRouterPolicyList
	if c.Reader.List(ctx, &erps) == nil {
		for _, o := range erps.Items {
			add("ZitiEdgeRouterPolicy", o.Status.Conditions)
		}
	}
	var serps zitiv1.ZitiServiceEdgeRouterPolicyList
	if c.Reader.List(ctx, &serps) == nil {
		for _, o := range serps.Items {
			add("ZitiServiceEdgeRouterPolicy", o.Status.Conditions)
		}
	}
	var terms zitiv1.ZitiTerminatorList
	if c.Reader.List(ctx, &terms) == nil {
		for _, o := range terms.Items {
			add("ZitiTerminator", o.Status.Conditions)
		}
	}
	var sidecars zitiv1.ZitiSidecarList
	if c.Reader.List(ctx, &sidecars) == nil {
		for _, o := range sidecars.Items {
			add("ZitiSidecar", o.Status.Conditions)
		}
	}
	var forwards zitiv1.ZitiPortForwardList
	if c.Reader.List(ctx, &forwards) == nil {
		for _, o := range forwards.Items {
			add("ZitiPortForward", o.Status.Conditions)
		}
	}

	for k, n := range count {
		ch <- prometheus.MustNewConstMetric(resourceConditionsDesc, prometheus.GaugeValue, n, k[0], k[1], k[2])
	}
}
