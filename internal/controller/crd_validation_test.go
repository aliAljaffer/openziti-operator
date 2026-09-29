// SPDX-License-Identifier: Apache-2.0

package controller

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"

	zitiv1 "github.com/aliAljaffer/openziti-operator/api/v1alpha1"
)

var _ = Describe("CRD validation", func() {
	newService := func(name string, mut func(*zitiv1.ZitiAppSpec)) *zitiv1.ZitiApp {
		s := &zitiv1.ZitiApp{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
			Spec: zitiv1.ZitiAppSpec{
				Expose:  zitiv1.Expose{Addresses: []string{"app.example.com"}, Ports: []intstr.IntOrString{intstr.FromInt32(443)}},
				Targets: []zitiv1.Target{{Address: "10.0.0.5", Port: 8443}},
			},
		}
		if mut != nil {
			mut(&s.Spec)
		}
		return s
	}

	DescribeTable("ZitiApp create",
		func(mut func(*zitiv1.ZitiAppSpec), wantErr string) {
			err := k8sClient.Create(ctx, newService("create-case", mut))
			DeferCleanup(func() { _ = k8sClient.Delete(ctx, newService("create-case", nil)) })
			if wantErr == "" {
				Expect(err).NotTo(HaveOccurred())
				return
			}
			Expect(err).To(MatchError(ContainSubstring(wantErr)))
		},
		Entry("valid address target", nil, ""),
		Entry("valid kubernetesService target", func(s *zitiv1.ZitiAppSpec) {
			s.Targets = []zitiv1.Target{{KubernetesService: "web", Port: 80}}
		}, ""),
		Entry("several targets", func(s *zitiv1.ZitiAppSpec) {
			s.Targets = []zitiv1.Target{{Address: "10.0.0.5"}, {Address: "10.0.0.6", Port: 8443, Cost: 10}}
		}, ""),
		Entry("port and range", func(s *zitiv1.ZitiAppSpec) {
			s.Expose.Ports = []intstr.IntOrString{intstr.FromInt32(443), intstr.FromString("8000-8005"), intstr.FromString("22")}
		}, ""),
		Entry("address and kubernetesService", func(s *zitiv1.ZitiAppSpec) { s.Targets[0].KubernetesService = "web" }, "set exactly one of address and kubernetesService"),
		Entry("target without address", func(s *zitiv1.ZitiAppSpec) { s.Targets = []zitiv1.Target{{Port: 80}} }, "set exactly one of address and kubernetesService"),
		Entry("kubernetesService without port", func(s *zitiv1.ZitiAppSpec) {
			s.Targets = []zitiv1.Target{{KubernetesService: "web"}}
		}, "port is required with kubernetesService"),
		Entry("no targets", func(s *zitiv1.ZitiAppSpec) { s.Targets = nil }, "expose and targets are required"),
		Entry("target port out of range", func(s *zitiv1.ZitiAppSpec) { s.Targets[0].Port = 70000 }, "less than or equal to 65535"),
		Entry("port out of range", func(s *zitiv1.ZitiAppSpec) { s.Expose.Ports = []intstr.IntOrString{intstr.FromInt32(70000)} }, "a port is 1-65535"),
		Entry("port zero", func(s *zitiv1.ZitiAppSpec) { s.Expose.Ports = []intstr.IntOrString{intstr.FromInt32(0)} }, "a port is 1-65535"),
		Entry("observe needs no expose or targets", func(s *zitiv1.ZitiAppSpec) {
			s.ManagementPolicy = zitiv1.ManagementObserve
			s.Expose, s.Targets = zitiv1.Expose{}, nil
		}, ""),
		Entry("adopt is not allowed for apps", func(s *zitiv1.ZitiAppSpec) { s.ManagementPolicy = zitiv1.ManagementAdopt }, "Unsupported value"),
		Entry("no addresses", func(s *zitiv1.ZitiAppSpec) { s.Expose.Addresses = nil }, "addresses"),
		Entry("unknown protocol", func(s *zitiv1.ZitiAppSpec) { s.Expose.Protocols = []string{"icmp"} }, "Unsupported value"),
		Entry("unknown deletionPolicy", func(s *zitiv1.ZitiAppSpec) { s.DeletionPolicy = "Maybe" }, "Unsupported value"),
	)

	It("makes ZitiApp zitiName and deletionPolicy immutable and applies defaults", func() {
		svc := newService("immutable-svc", func(s *zitiv1.ZitiAppSpec) { s.ZitiName = "a.example.com" })
		Expect(k8sClient.Create(ctx, svc)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, svc) })
		Expect(svc.Spec.DeletionPolicy).To(Equal(zitiv1.DeletionPolicyDelete))
		Expect(svc.Spec.Expose.Protocols).To(Equal([]string{"tcp"}))

		svc.Spec.ZitiName = "b.example.com"
		Expect(k8sClient.Update(ctx, svc)).To(MatchError(ContainSubstring("zitiName is immutable")))
		Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: "default", Name: "immutable-svc"}, svc)).To(Succeed())
		svc.Spec.DeletionPolicy = zitiv1.DeletionPolicyOrphan
		Expect(k8sClient.Update(ctx, svc)).To(MatchError(ContainSubstring("deletionPolicy is immutable")))
	})

	It("makes ZitiIdentity zitiName, deletionPolicy and enrollmentMode immutable and applies defaults", func() {
		id := &zitiv1.ZitiIdentity{
			ObjectMeta: metav1.ObjectMeta{Name: "immutable-id", Namespace: "default"},
			Spec:       zitiv1.ZitiIdentitySpec{ZitiName: "a"},
		}
		Expect(k8sClient.Create(ctx, id)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, id) })
		Expect(id.Spec.EnrollmentMode).To(Equal(zitiv1.EnrollmentJWTOnly))
		Expect(id.Spec.DeletionPolicy).To(Equal(zitiv1.DeletionPolicyDelete))
		Expect(id.Spec.ConnectionRef).To(Equal("default"))

		for want, mut := range map[string]func(*zitiv1.ZitiIdentitySpec){
			"zitiName is immutable":       func(s *zitiv1.ZitiIdentitySpec) { s.ZitiName = "b" },
			"deletionPolicy is immutable": func(s *zitiv1.ZitiIdentitySpec) { s.DeletionPolicy = zitiv1.DeletionPolicyOrphan },
			"enrollmentMode is immutable": func(s *zitiv1.ZitiIdentitySpec) { s.EnrollmentMode = zitiv1.EnrollmentOperator },
		} {
			var cur zitiv1.ZitiIdentity
			Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: "default", Name: "immutable-id"}, &cur)).To(Succeed())
			mut(&cur.Spec)
			Expect(k8sClient.Update(ctx, &cur)).To(MatchError(ContainSubstring(want)))
		}
	})

	It("rejects a ZitiConnection with a non-https URL or no hostingRouters", func() {
		conn := func(url string, routers []string) *zitiv1.ZitiConnection {
			c := &zitiv1.ZitiConnection{
				ObjectMeta: metav1.ObjectMeta{Name: "conn-case"},
				Spec:       zitiv1.ZitiConnectionSpec{ManagementURL: url, HostingRouters: routers},
			}
			c.Spec.CABundle.ConfigMapRef = zitiv1.ConfigMapKeyRef{Namespace: "n", Name: "c", Key: "k"}
			c.Spec.Auth.Updb.SecretRef = zitiv1.SecretRef{Namespace: "n", Name: "s"}
			return c
		}
		Expect(k8sClient.Create(ctx, conn("http://ctrl", []string{"r"}))).To(MatchError(ContainSubstring("should match")))
		Expect(k8sClient.Create(ctx, conn("https://ctrl", nil))).To(MatchError(ContainSubstring("hostingRouters")))
		ok := conn("https://ctrl", []string{"r"})
		Expect(k8sClient.Create(ctx, ok)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, ok) })
		Expect(ok.Spec.RoleScope).To(Equal(zitiv1.RoleScopeNamespaced))
	})

	It("validates ZitiAccessPolicy and makes zitiName and deletionPolicy immutable", func() {
		mk := func(ident, svc []string) *zitiv1.ZitiAccessPolicy {
			return &zitiv1.ZitiAccessPolicy{
				ObjectMeta: metav1.ObjectMeta{Name: "ap-case", Namespace: "default"},
				Spec:       zitiv1.ZitiAccessPolicySpec{ZitiName: "a", IdentityRoles: ident, ServiceRoles: svc},
			}
		}
		Expect(k8sClient.Create(ctx, mk(nil, []string{"#x"}))).To(MatchError(ContainSubstring("identityRoles")))
		Expect(k8sClient.Create(ctx, mk([]string{"#x"}, nil))).To(MatchError(ContainSubstring("serviceRoles")))
		ap := mk([]string{"#x"}, []string{"#y"})
		Expect(k8sClient.Create(ctx, ap)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, ap) })
		Expect(ap.Spec.DeletionPolicy).To(Equal(zitiv1.DeletionPolicyDelete))

		ap.Spec.ZitiName = "b"
		Expect(k8sClient.Update(ctx, ap)).To(MatchError(ContainSubstring("zitiName is immutable")))
		Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: "default", Name: "ap-case"}, ap)).To(Succeed())
		ap.Spec.DeletionPolicy = zitiv1.DeletionPolicyOrphan
		Expect(k8sClient.Update(ctx, ap)).To(MatchError(ContainSubstring("deletionPolicy is immutable")))
	})
})
