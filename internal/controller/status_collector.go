// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
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

	var apps zitiv1.ZitiAppList
	collectConditions(ctx, c.Reader, count, "ZitiApp", &apps,
		func(o *zitiv1.ZitiApp) []metav1.Condition { return o.Status.Conditions },
		func(o *zitiv1.ZitiApp) {
			for _, svc := range o.Status.Services {
				ch <- prometheus.MustNewConstMetric(appServiceTerminatorsDesc, prometheus.GaugeValue,
					float64(len(svc.Terminators)), o.Namespace, o.Name, svc.Name)
			}
		})
	var ids zitiv1.ZitiIdentityList
	collectConditions(ctx, c.Reader, count, "ZitiIdentity", &ids,
		func(o *zitiv1.ZitiIdentity) []metav1.Condition { return o.Status.Conditions },
		func(o *zitiv1.ZitiIdentity) {
			if o.Status.CertNotAfter != nil {
				ch <- prometheus.MustNewConstMetric(identityCertExpiresDesc, prometheus.GaugeValue,
					time.Until(o.Status.CertNotAfter.Time).Seconds(), o.Namespace, o.Name)
			}
		})
	var aps zitiv1.ZitiAccessPolicyList
	collectConditions(ctx, c.Reader, count, "ZitiAccessPolicy", &aps,
		func(o *zitiv1.ZitiAccessPolicy) []metav1.Condition { return o.Status.Conditions }, nil)
	var conns zitiv1.ZitiConnectionList
	collectConditions(ctx, c.Reader, count, "ZitiConnection", &conns,
		func(o *zitiv1.ZitiConnection) []metav1.Condition { return o.Status.Conditions }, nil)
	var routers zitiv1.ZitiRouterList
	collectConditions(ctx, c.Reader, count, "ZitiRouter", &routers,
		func(o *zitiv1.ZitiRouter) []metav1.Condition { return o.Status.Conditions }, nil)
	var cas zitiv1.ZitiCAList
	collectConditions(ctx, c.Reader, count, "ZitiCA", &cas,
		func(o *zitiv1.ZitiCA) []metav1.Condition { return o.Status.Conditions }, nil)
	var signers zitiv1.ZitiJwtSignerList
	collectConditions(ctx, c.Reader, count, "ZitiJwtSigner", &signers,
		func(o *zitiv1.ZitiJwtSigner) []metav1.Condition { return o.Status.Conditions }, nil)
	var cfgs zitiv1.ZitiConfigList
	collectConditions(ctx, c.Reader, count, "ZitiConfig", &cfgs,
		func(o *zitiv1.ZitiConfig) []metav1.Condition { return o.Status.Conditions }, nil)
	var svcs zitiv1.ZitiServiceList
	collectConditions(ctx, c.Reader, count, "ZitiService", &svcs,
		func(o *zitiv1.ZitiService) []metav1.Condition { return o.Status.Conditions }, nil)
	var sps zitiv1.ZitiServicePolicyList
	collectConditions(ctx, c.Reader, count, "ZitiServicePolicy", &sps,
		func(o *zitiv1.ZitiServicePolicy) []metav1.Condition { return o.Status.Conditions }, nil)
	var erps zitiv1.ZitiEdgeRouterPolicyList
	collectConditions(ctx, c.Reader, count, "ZitiEdgeRouterPolicy", &erps,
		func(o *zitiv1.ZitiEdgeRouterPolicy) []metav1.Condition { return o.Status.Conditions }, nil)
	var serps zitiv1.ZitiServiceEdgeRouterPolicyList
	collectConditions(ctx, c.Reader, count, "ZitiServiceEdgeRouterPolicy", &serps,
		func(o *zitiv1.ZitiServiceEdgeRouterPolicy) []metav1.Condition { return o.Status.Conditions }, nil)
	var terms zitiv1.ZitiTerminatorList
	collectConditions(ctx, c.Reader, count, "ZitiTerminator", &terms,
		func(o *zitiv1.ZitiTerminator) []metav1.Condition { return o.Status.Conditions }, nil)
	var sidecars zitiv1.ZitiSidecarList
	collectConditions(ctx, c.Reader, count, "ZitiSidecar", &sidecars,
		func(o *zitiv1.ZitiSidecar) []metav1.Condition { return o.Status.Conditions }, nil)
	var forwards zitiv1.ZitiPortForwardList
	collectConditions(ctx, c.Reader, count, "ZitiPortForward", &forwards,
		func(o *zitiv1.ZitiPortForward) []metav1.Condition { return o.Status.Conditions }, nil)

	for k, n := range count {
		ch <- prometheus.MustNewConstMetric(resourceConditionsDesc, prometheus.GaugeValue, n, k[0], k[1], k[2])
	}
}

// collectConditions lists one kind and counts its conditions by type and status. extra, when set, runs
// for each item too, for the metrics that read more than the conditions. The accessor names the item
// type and reads its status, because the kinds share no conditions method.
func collectConditions[T client.Object](ctx context.Context, reader client.Reader, count map[[3]string]float64, kind string,
	list client.ObjectList, conditions func(T) []metav1.Condition, extra func(T)) {
	if reader.List(ctx, list) != nil {
		return
	}
	items, err := apimeta.ExtractList(list)
	if err != nil {
		return
	}
	for _, raw := range items {
		item, ok := raw.(T)
		if !ok {
			continue
		}
		for _, cond := range conditions(item) {
			count[[3]string{kind, cond.Type, string(cond.Status)}]++
		}
		if extra != nil {
			extra(item)
		}
	}
}
