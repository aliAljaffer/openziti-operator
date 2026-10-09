// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"

	zitiv1 "github.com/aliAljaffer/openziti-operator/api/v1alpha1"
	"github.com/aliAljaffer/openziti-operator/internal/desired"
)

// The API server fills in cluster IPs, IP families, traffic policy, and session affinity on a Service. The fake
// client does none of that, so only envtest can show whether the operator settles instead of rewriting forever.
var _ = Describe("Router Service against the API server", func() {
	serial := 0

	var name string
	var owner *zitiv1.ZitiRouter
	var r *ZitiRouterReconciler

	BeforeEach(func() {
		serial++
		name = fmt.Sprintf("edge-%d", serial)
		owner = &zitiv1.ZitiRouter{Name: name, UID: types.UID("uid-" + name)}
		r = &ZitiRouterReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), Recorder: record.NewFakeRecorder(50)}
	})

	workload := func(serviceType string) desired.RouterWorkload {
		return desired.NewRouterWorkload(&zitiv1.ZitiRouter{
			Name: name,
			UID:  owner.UID,
			Spec: zitiv1.ZitiRouterSpec{
				AdvertisedAddress: "edge.example.com",
				Deployment:        &zitiv1.ZitiRouterDeployment{Namespace: "default", ServiceType: serviceType},
			},
		}, "v2.0.4")
	}

	// The Service is seeded the way the operator leaves it, so the settled check is what runs.
	create := func(serviceType string) types.NamespacedName {
		By("creating the " + serviceType + " Service")
		svc := workload(serviceType).Service()
		Expect(r.create(ctx, owner, svc)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, svc) })
		return types.NamespacedName{Namespace: "default", Name: svc.Name}
	}

	get := func(key types.NamespacedName) corev1.Service {
		GinkgoHelper()
		var svc corev1.Service
		Expect(k8sClient.Get(ctx, key, &svc)).To(Succeed())
		return svc
	}

	It("keeps the cluster IP and node port the API server allocated", func() {
		key := create("NodePort")
		svc := get(key)
		Expect(svc.Spec.ClusterIP).NotTo(BeEmpty())
		Expect(svc.Spec.Ports[0].NodePort).NotTo(BeZero())
		Expect(svc.OwnerReferences).To(HaveLen(1))

		Expect(r.applyService(ctx, owner, workload("NodePort"))).To(Succeed())

		after := get(key)
		Expect(after.Spec.ClusterIP).To(Equal(svc.Spec.ClusterIP))
		Expect(after.Spec.Ports[0].NodePort).To(Equal(svc.Spec.Ports[0].NodePort))
	})

	It("does not rewrite a Service that already matches", func() {
		key := create("NodePort")
		Expect(r.applyService(ctx, owner, workload("NodePort"))).To(Succeed())
		before := get(key)

		Expect(r.applyService(ctx, owner, workload("NodePort"))).To(Succeed())
		Expect(r.applyService(ctx, owner, workload("NodePort"))).To(Succeed())

		Expect(get(key).ResourceVersion).To(Equal(before.ResourceVersion))
	})

	// A naive whole-spec compare sees the fields the API server defaulted and never settles.
	It("settles even though the API server defaulted fields the operator never sets", func() {
		key := create("NodePort")
		svc := get(key)
		Expect(svc.Spec.SessionAffinity).NotTo(BeEmpty())
		Expect(svc.Spec.IPFamilies).NotTo(BeEmpty())
		Expect(svc.Spec.InternalTrafficPolicy).NotTo(BeNil())

		Expect(serviceSettled(&svc, workload("NodePort").Service())).To(BeTrue())
		Expect(equality.Semantic.DeepEqual(svc.Spec, workload("NodePort").Service().Spec)).To(BeFalse())
	})

	It("carries the node port across a NodePort to LoadBalancer change", func() {
		key := create("NodePort")
		Expect(r.applyService(ctx, owner, workload("NodePort"))).To(Succeed())
		before := get(key)

		Expect(r.applyService(ctx, owner, workload("LoadBalancer"))).To(Succeed())

		after := get(key)
		Expect(after.Spec.Type).To(Equal(corev1.ServiceTypeLoadBalancer))
		Expect(after.Spec.Ports[0].NodePort).To(Equal(before.Spec.Ports[0].NodePort))
		Expect(after.Spec.ClusterIP).To(Equal(before.Spec.ClusterIP))
	})

	It("keeps the annotations and finalizers a load balancer provider adds", func() {
		key := create("LoadBalancer")
		live := get(key)
		live.Annotations = map[string]string{"service.beta.kubernetes.io/aws-load-balancer-type": "nlb"}
		live.Finalizers = []string{"service.kubernetes.io/load-balancer-cleanup"}
		Expect(k8sClient.Update(ctx, &live)).To(Succeed())

		Expect(r.applyService(ctx, owner, workload("NodePort"))).To(Succeed())

		after := get(key)
		Expect(after.Spec.Type).To(Equal(corev1.ServiceTypeNodePort))
		Expect(after.Annotations).To(HaveKeyWithValue("service.beta.kubernetes.io/aws-load-balancer-type", "nlb"))
		Expect(after.Finalizers).To(ContainElement("service.kubernetes.io/load-balancer-cleanup"))
		Expect(after.OwnerReferences).To(HaveLen(1))
	})

	It("drops the node port going to ClusterIP, which rejects one", func() {
		key := create("NodePort")
		Expect(r.applyService(ctx, owner, workload("NodePort"))).To(Succeed())
		before := get(key)

		Expect(r.applyService(ctx, owner, workload("ClusterIP"))).To(Succeed())

		after := get(key)
		Expect(after.Spec.Type).To(Equal(corev1.ServiceTypeClusterIP))
		Expect(after.Spec.Ports[0].NodePort).To(BeZero())
		Expect(after.Spec.ClusterIP).To(Equal(before.Spec.ClusterIP))
	})
})
