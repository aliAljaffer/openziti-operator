// SPDX-License-Identifier: Apache-2.0

package controller

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"

	zitiv1 "github.com/aliAljaffer/openziti-operator/api/v1alpha1"
	"github.com/aliAljaffer/openziti-operator/internal/desired"
)

var _ = Describe("CRD validation", func() {
	newService := func(name string, mut func(*zitiv1.ZitiAppSpec)) *zitiv1.ZitiApp {
		s := &zitiv1.ZitiApp{
			Name: name, Namespace: "default",
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
		Entry("adopt is allowed for apps", func(s *zitiv1.ZitiAppSpec) { s.ManagementPolicy = zitiv1.ManagementAdopt }, ""),
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
			Name: "immutable-id", Namespace: "default",
			Spec: zitiv1.ZitiIdentitySpec{ZitiName: "a"},
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

	It("allows a ZitiIdentity certificate only with enrollmentMode None and externalId", func() {
		mk := func(name string, mut func(*zitiv1.ZitiIdentitySpec)) *zitiv1.ZitiIdentity {
			id := &zitiv1.ZitiIdentity{
				Name: name, Namespace: "default",
				Spec: zitiv1.ZitiIdentitySpec{Certificate: &zitiv1.WorkloadCertificate{IssuerRef: zitiv1.CertificateIssuerRef{Name: "ca"}}},
			}
			mut(&id.Spec)
			return id
		}
		Expect(k8sClient.Create(ctx, mk("cert-jwt", func(s *zitiv1.ZitiIdentitySpec) {}))).To(MatchError(ContainSubstring("certificate needs enrollmentMode None and externalId")))
		Expect(k8sClient.Create(ctx, mk("cert-sa", func(s *zitiv1.ZitiIdentitySpec) {
			s.EnrollmentMode, s.AuthPolicy, s.ServiceAccount = zitiv1.EnrollmentNone, "Default", "web"
		}))).To(MatchError(ContainSubstring("certificate needs enrollmentMode None and externalId")))
		ok := mk("cert-ok", func(s *zitiv1.ZitiIdentitySpec) {
			s.EnrollmentMode, s.AuthPolicy, s.ExternalID = zitiv1.EnrollmentNone, "Default", "team-a.web"
		})
		Expect(k8sClient.Create(ctx, ok)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, ok) })
		Expect(ok.Spec.Certificate.IssuerRef.Kind).To(Equal("ClusterIssuer"))
	})

	It("checks the ZitiTerminator address, applies defaults, and keeps service, router, and binding fixed", func() {
		term := func(name, address string) *zitiv1.ZitiTerminator {
			return &zitiv1.ZitiTerminator{Name: name, Namespace: "default",
				Spec: zitiv1.ZitiTerminatorSpec{Service: "web", Router: "r", Address: address}}
		}
		Expect(k8sClient.Create(ctx, term("bad-addr", "10.0.0.5:80"))).To(MatchError(ContainSubstring("should match")))
		ok := term("term-ok", "tcp:10.0.0.5:8443")
		Expect(k8sClient.Create(ctx, ok)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, ok) })
		Expect(ok.Spec.Binding).To(Equal("transport"))
		Expect(ok.Spec.Precedence).To(Equal("default"))
		Expect(ok.Spec.DeletionPolicy).To(Equal(zitiv1.DeletionPolicyDelete))

		ok.Spec.Cost = 5
		ok.Spec.Address = "udp:10.0.0.6:53"
		Expect(k8sClient.Update(ctx, ok)).To(Succeed())
		ok.Spec.Router = "other"
		Expect(k8sClient.Update(ctx, ok)).To(MatchError(ContainSubstring("service, router, and binding are immutable")))
	})

	It("rejects a ZitiRouter port below 1024", func() {
		rt := &zitiv1.ZitiRouter{Name: "low-port", Spec: zitiv1.ZitiRouterSpec{Port: 443}}
		Expect(k8sClient.Create(ctx, rt)).To(MatchError(ContainSubstring("port")))
	})

	It("rejects a ZitiConnection with a non-https URL or no hostingRouters", func() {
		conn := func(url string, routers []string) *zitiv1.ZitiConnection {
			c := &zitiv1.ZitiConnection{
				Name: "conn-case",
				Spec: zitiv1.ZitiConnectionSpec{ManagementURL: url, HostingRouters: routers},
			}
			c.Spec.CABundle.ConfigMapRef = zitiv1.ConfigMapKeyRef{Namespace: "n", Name: "c", Key: "k"}
			c.Spec.Auth.Updb = &zitiv1.UpdbAuth{SecretRef: zitiv1.SecretRef{Namespace: "n", Name: "s"}}
			return c
		}
		Expect(k8sClient.Create(ctx, conn("http://ctrl", []string{"r"}))).To(MatchError(ContainSubstring("should match")))
		Expect(k8sClient.Create(ctx, conn("https://ctrl", nil))).To(MatchError(ContainSubstring("hostingRouters")))
		both := conn("https://ctrl", []string{"r"})
		both.Spec.Auth.Cert = &zitiv1.CertAuth{SecretRef: zitiv1.SecretRef{Namespace: "n", Name: "s"}}
		Expect(k8sClient.Create(ctx, both)).To(MatchError(ContainSubstring("set exactly one of updb and cert")))
		neither := conn("https://ctrl", []string{"r"})
		neither.Spec.Auth.Updb = nil
		Expect(k8sClient.Create(ctx, neither)).To(MatchError(ContainSubstring("set exactly one of updb and cert")))
		certOnly := conn("https://ctrl", []string{"r"})
		certOnly.Name = "conn-cert"
		certOnly.Spec.Auth = zitiv1.ConnectionAuth{Cert: &zitiv1.CertAuth{SecretRef: zitiv1.SecretRef{Namespace: "n", Name: "s"}}}
		Expect(k8sClient.Create(ctx, certOnly)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, certOnly) })
		ok := conn("https://ctrl", []string{"r"})
		Expect(k8sClient.Create(ctx, ok)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, ok) })
		Expect(ok.Spec.RoleScope).To(Equal(zitiv1.RoleScopeNamespaced))
	})

	It("validates ZitiAccessPolicy and makes zitiName and deletionPolicy immutable", func() {
		mk := func(ident, svc []string) *zitiv1.ZitiAccessPolicy {
			return &zitiv1.ZitiAccessPolicy{
				Name: "ap-case", Namespace: "default",
				Spec: zitiv1.ZitiAccessPolicySpec{ZitiName: "a", IdentityRoles: ident, ServiceRoles: svc},
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

var _ = Describe("Service exposure", func() {
	It("creates a ZitiApp the API server accepts, follows annotation changes, and removes it", func() {
		svc := &corev1.Service{
			Name: "expose-case", Namespace: "default", Annotations: map[string]string{
				desired.AnnExpose: "true", desired.AnnPorts: "443, 8000-8005", desired.AnnAllowGroups: "staff",
				desired.AnnAllowIdentites: "alice", desired.AnnEntryRouters: "edge-1",
			},
			Spec: corev1.ServiceSpec{Ports: []corev1.ServicePort{{Port: 80}}},
		}
		Expect(k8sClient.Create(ctx, svc)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, svc) })

		r := &ServiceReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), Recorder: record.NewFakeRecorder(10)}
		key := types.NamespacedName{Namespace: "default", Name: "expose-case"}
		reconcile := func() {
			_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: key})
			Expect(err).NotTo(HaveOccurred())
		}

		reconcile()
		var app zitiv1.ZitiApp
		Expect(k8sClient.Get(ctx, key, &app)).To(Succeed())
		Expect(app.Spec.Expose.Ports).To(HaveLen(2))
		Expect(app.Spec.ConnectionRef).To(Equal("default"))
		Expect(app.Spec.DeletionPolicy).To(Equal(zitiv1.DeletionPolicyDelete))
		Expect(metav1.IsControlledBy(&app, svc)).To(BeTrue())

		reconcile()
		Expect(k8sClient.Get(ctx, key, &app)).To(Succeed())
		version := app.ResourceVersion
		reconcile()
		Expect(k8sClient.Get(ctx, key, &app)).To(Succeed())
		Expect(app.ResourceVersion).To(Equal(version), "an unchanged Service must not rewrite the ZitiApp")

		Expect(k8sClient.Get(ctx, key, svc)).To(Succeed())
		svc.Annotations[desired.AnnAllowGroups] = "staff,ops"
		Expect(k8sClient.Update(ctx, svc)).To(Succeed())
		reconcile()
		Expect(k8sClient.Get(ctx, key, &app)).To(Succeed())
		Expect(app.Spec.Allow.Groups).To(Equal([]string{"staff", "ops"}))

		svc.Annotations[desired.AnnExpose] = "false"
		Expect(k8sClient.Update(ctx, svc)).To(Succeed())
		reconcile()
		Expect(apierrors.IsNotFound(k8sClient.Get(ctx, key, &app))).To(BeTrue(), "the ZitiApp must be deleted")
	})
})

var _ = Describe("ZitiJwtSigner and token identities", func() {
	signer := func(mut func(*zitiv1.ZitiJwtSignerSpec)) *zitiv1.ZitiJwtSigner {
		sg := &zitiv1.ZitiJwtSigner{
			Name: "signer-case",
			Spec: zitiv1.ZitiJwtSignerSpec{Audience: "ziti", Keys: zitiv1.SignerKeys{Kubernetes: &zitiv1.KubernetesKeys{}}},
		}
		if mut != nil {
			mut(&sg.Spec)
		}
		return sg
	}

	DescribeTable("ZitiJwtSigner create",
		func(mut func(*zitiv1.ZitiJwtSignerSpec), wantErr string) {
			sg := signer(mut)
			err := k8sClient.Create(ctx, sg)
			DeferCleanup(func() { _ = k8sClient.Delete(ctx, signer(nil)) })
			if wantErr == "" {
				Expect(err).NotTo(HaveOccurred())
				return
			}
			Expect(err).To(MatchError(ContainSubstring(wantErr)))
		},
		Entry("kubernetes keys", nil, ""),
		Entry("jwksEndpoint keys", func(s *zitiv1.ZitiJwtSignerSpec) {
			s.Keys = zitiv1.SignerKeys{JwksEndpoint: &zitiv1.JwksEndpoint{URL: "https://idp.example/keys", Issuer: "https://idp.example"}}
		}, ""),
		Entry("no key source", func(s *zitiv1.ZitiJwtSignerSpec) { s.Keys = zitiv1.SignerKeys{} }, "set exactly one of kubernetes and jwksEndpoint"),
		Entry("two key sources", func(s *zitiv1.ZitiJwtSignerSpec) {
			s.Keys.JwksEndpoint = &zitiv1.JwksEndpoint{URL: "https://idp.example/keys", Issuer: "i"}
		}, "set exactly one of kubernetes and jwksEndpoint"),
		Entry("http jwks url", func(s *zitiv1.ZitiJwtSignerSpec) {
			s.Keys = zitiv1.SignerKeys{JwksEndpoint: &zitiv1.JwksEndpoint{URL: "http://idp.example/keys", Issuer: "i"}}
		}, "should match"),
		Entry("empty audience", func(s *zitiv1.ZitiJwtSignerSpec) { s.Audience = "" }, "audience"),
		Entry("unknown deletionPolicy", func(s *zitiv1.ZitiJwtSignerSpec) { s.DeletionPolicy = "Maybe" }, "Unsupported value"),
	)

	It("applies defaults to a signer and keeps zitiName and deletionPolicy fixed", func() {
		sg := signer(func(s *zitiv1.ZitiJwtSignerSpec) { s.ZitiName = "a" })
		Expect(k8sClient.Create(ctx, sg)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, sg) })
		Expect(sg.Spec.ClaimsProperty).To(Equal("sub"))
		Expect(*sg.Spec.UseExternalID).To(BeTrue())
		Expect(*sg.Spec.CreateAuthPolicy).To(BeTrue())
		Expect(sg.Spec.DeletionPolicy).To(Equal(zitiv1.DeletionPolicyDelete))
		Expect(sg.Spec.ConnectionRef).To(Equal("default"))

		sg.Spec.ZitiName = "b"
		Expect(k8sClient.Update(ctx, sg)).To(MatchError(ContainSubstring("zitiName is immutable")))
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "signer-case"}, sg)).To(Succeed())
		sg.Spec.DeletionPolicy = zitiv1.DeletionPolicyOrphan
		Expect(k8sClient.Update(ctx, sg)).To(MatchError(ContainSubstring("deletionPolicy is immutable")))
	})

	DescribeTable("ZitiIdentity token login rules",
		func(mut func(*zitiv1.ZitiIdentitySpec), wantErr string) {
			id := &zitiv1.ZitiIdentity{Name: "token-case", Namespace: "default"}
			mut(&id.Spec)
			err := k8sClient.Create(ctx, id)
			DeferCleanup(func() { _ = k8sClient.Delete(ctx, id) })
			if wantErr == "" {
				Expect(err).NotTo(HaveOccurred())
				return
			}
			Expect(err).To(MatchError(ContainSubstring(wantErr)))
		},
		Entry("None with a service account and an auth policy", func(s *zitiv1.ZitiIdentitySpec) {
			s.EnrollmentMode, s.AuthPolicy, s.ServiceAccount = zitiv1.EnrollmentNone, "k8s", "web"
		}, ""),
		Entry("None with an external id and an auth policy", func(s *zitiv1.ZitiIdentitySpec) {
			s.EnrollmentMode, s.AuthPolicy, s.ExternalID = zitiv1.EnrollmentNone, "idp", "someone"
		}, ""),
		Entry("None without an auth policy", func(s *zitiv1.ZitiIdentitySpec) {
			s.EnrollmentMode, s.ServiceAccount = zitiv1.EnrollmentNone, "web"
		}, "enrollmentMode None needs authPolicy"),
		Entry("None without a subject", func(s *zitiv1.ZitiIdentitySpec) {
			s.EnrollmentMode, s.AuthPolicy = zitiv1.EnrollmentNone, "k8s"
		}, "enrollmentMode None needs authPolicy"),
		Entry("service account and external id together", func(s *zitiv1.ZitiIdentitySpec) {
			s.ServiceAccount, s.ExternalID = "web", "someone"
		}, "set only one of serviceAccount and externalId"),
		Entry("JwtOnly with a service account", func(s *zitiv1.ZitiIdentitySpec) { s.ServiceAccount = "web" }, ""),
	)
})

var _ = Describe("ZitiCA", func() {
	ca := func(mut func(*zitiv1.ZitiCASpec)) *zitiv1.ZitiCA {
		c := &zitiv1.ZitiCA{
			Name: "ca-case",
			Spec: zitiv1.ZitiCASpec{Certificate: zitiv1.CertificateSource{SecretRef: zitiv1.SecretRef{Namespace: "cert-manager", Name: "issuer"}}},
		}
		if mut != nil {
			mut(&c.Spec)
		}
		return c
	}

	It("applies defaults and keeps zitiName and deletionPolicy fixed", func() {
		c := ca(func(s *zitiv1.ZitiCASpec) { s.ZitiName = "a" })
		Expect(k8sClient.Create(ctx, c)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, c) })
		Expect(c.Spec.Certificate.CertKey).To(Equal("tls.crt"))
		Expect(*c.Spec.AuthEnabled).To(BeTrue())
		Expect(c.Spec.Verification.SignWithSecretKey).To(BeFalse())
		Expect(c.Spec.ExternalIDClaim.Location).To(Equal("COMMON_NAME"))
		Expect(c.Spec.ExternalIDClaim.Matcher).To(Equal("ALL"))
		Expect(c.Spec.ExternalIDClaim.Parser).To(Equal("NONE"))
		Expect(c.Spec.DeletionPolicy).To(Equal(zitiv1.DeletionPolicyDelete))

		c.Spec.ZitiName = "b"
		Expect(k8sClient.Update(ctx, c)).To(MatchError(ContainSubstring("zitiName is immutable")))
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "ca-case"}, c)).To(Succeed())
		c.Spec.DeletionPolicy = zitiv1.DeletionPolicyOrphan
		Expect(k8sClient.Update(ctx, c)).To(MatchError(ContainSubstring("deletionPolicy is immutable")))
	})

	It("accepts verification through an issuer and rejects it together with signWithSecretKey", func() {
		both := ca(func(s *zitiv1.ZitiCASpec) {
			s.Verification.SignWithSecretKey = true
			s.Verification.IssuerRef = &zitiv1.VerificationIssuer{Name: "workloads", Namespace: "cert-manager"}
		})
		Expect(k8sClient.Create(ctx, both)).To(MatchError(ContainSubstring("set only one of signWithSecretKey and issuerRef")))
		only := ca(func(s *zitiv1.ZitiCASpec) {
			s.Verification.IssuerRef = &zitiv1.VerificationIssuer{Name: "workloads", Namespace: "cert-manager"}
		})
		Expect(k8sClient.Create(ctx, only)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, only) })
		Expect(only.Spec.Verification.IssuerRef.Kind).To(Equal("ClusterIssuer"))
	})

	DescribeTable("rejects bad values",
		func(mut func(*zitiv1.ZitiCASpec), wantErr string) {
			err := k8sClient.Create(ctx, ca(mut))
			DeferCleanup(func() { _ = k8sClient.Delete(ctx, ca(nil)) })
			Expect(err).To(MatchError(ContainSubstring(wantErr)))
		},
		Entry("unknown claim location", func(s *zitiv1.ZitiCASpec) { s.ExternalIDClaim.Location = "SUBJECT" }, "Unsupported value"),
		Entry("unknown matcher", func(s *zitiv1.ZitiCASpec) { s.ExternalIDClaim.Matcher = "REGEX" }, "Unsupported value"),
		Entry("unknown parser", func(s *zitiv1.ZitiCASpec) { s.ExternalIDClaim.Parser = "CUT" }, "Unsupported value"),
		Entry("negative index", func(s *zitiv1.ZitiCASpec) { s.ExternalIDClaim.Index = -1 }, "greater than or equal to 0"),
	)
})

var _ = Describe("one-to-one kinds", func() {
	meta := func(name string) metav1.ObjectMeta { return metav1.ObjectMeta{Name: name, Namespace: "default"} }

	It("ZitiConfig keeps the data body, applies defaults, and fixes type, zitiName, and deletionPolicy", func() {
		cfg := &zitiv1.ZitiConfig{ObjectMeta: meta("cfg-case"), Spec: zitiv1.ZitiConfigSpec{
			EntitySpec: zitiv1.EntitySpec{ZitiName: "a"}, Type: "host.v2",
			Data: apiextensionsv1.JSON{Raw: []byte(`{"terminators":[{"address":"10.0.0.5","port":8443,"protocol":"tcp"}]}`)},
		}}
		Expect(k8sClient.Create(ctx, cfg)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, cfg) })
		Expect(cfg.Spec.ConnectionRef).To(Equal("default"))
		Expect(cfg.Spec.DeletionPolicy).To(Equal(zitiv1.DeletionPolicyDelete))
		Expect(string(cfg.Spec.Data.Raw)).To(ContainSubstring(`"terminators":[{"address":"10.0.0.5"`))

		for want, mut := range map[string]func(*zitiv1.ZitiConfigSpec){
			"zitiName is immutable":       func(s *zitiv1.ZitiConfigSpec) { s.ZitiName = "b" },
			"deletionPolicy is immutable": func(s *zitiv1.ZitiConfigSpec) { s.DeletionPolicy = zitiv1.DeletionPolicyOrphan },
			"type is immutable":           func(s *zitiv1.ZitiConfigSpec) { s.Type = "host.v1" },
		} {
			var cur zitiv1.ZitiConfig
			Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: "default", Name: "cfg-case"}, &cur)).To(Succeed())
			mut(&cur.Spec)
			Expect(k8sClient.Update(ctx, &cur)).To(MatchError(ContainSubstring(want)))
		}
	})

	It("ZitiConfig rejects data that is not an object and a missing type", func() {
		bad := &zitiv1.ZitiConfig{ObjectMeta: meta("cfg-bad"), Spec: zitiv1.ZitiConfigSpec{Type: "host.v1", Data: apiextensionsv1.JSON{Raw: []byte(`[1,2]`)}}}
		Expect(k8sClient.Create(ctx, bad)).To(MatchError(ContainSubstring("data")))
		noType := &zitiv1.ZitiConfig{ObjectMeta: meta("cfg-notype"), Spec: zitiv1.ZitiConfigSpec{Data: apiextensionsv1.JSON{Raw: []byte(`{}`)}}}
		Expect(k8sClient.Create(ctx, noType)).To(MatchError(ContainSubstring("type")))
	})

	It("ZitiService applies defaults and rejects a negative idle time", func() {
		svc := &zitiv1.ZitiService{ObjectMeta: meta("svc-case")}
		Expect(k8sClient.Create(ctx, svc)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, svc) })
		Expect(svc.Spec.TerminatorStrategy).To(Equal("smartrouting"))
		Expect(*svc.Spec.EncryptionRequired).To(BeTrue())
		bad := &zitiv1.ZitiService{ObjectMeta: meta("svc-bad"), Spec: zitiv1.ZitiServiceSpec{MaxIdleTimeMillis: -1}}
		Expect(k8sClient.Create(ctx, bad)).To(MatchError(ContainSubstring("greater than or equal to 0")))
	})

	It("ZitiServicePolicy needs a type and roles, and fixes the type", func() {
		mk := func(mut func(*zitiv1.ZitiServicePolicySpec)) *zitiv1.ZitiServicePolicy {
			p := &zitiv1.ZitiServicePolicy{ObjectMeta: meta("sp-case"), Spec: zitiv1.ZitiServicePolicySpec{Type: "Dial", IdentityRoles: []string{"#a"}, ServiceRoles: []string{"#b"}}}
			if mut != nil {
				mut(&p.Spec)
			}
			return p
		}
		Expect(k8sClient.Create(ctx, mk(func(s *zitiv1.ZitiServicePolicySpec) { s.Type = "Both" }))).To(MatchError(ContainSubstring("Unsupported value")))
		Expect(k8sClient.Create(ctx, mk(func(s *zitiv1.ZitiServicePolicySpec) { s.IdentityRoles = nil }))).To(MatchError(ContainSubstring("identityRoles")))
		Expect(k8sClient.Create(ctx, mk(func(s *zitiv1.ZitiServicePolicySpec) { s.ServiceRoles = nil }))).To(MatchError(ContainSubstring("serviceRoles")))
		Expect(k8sClient.Create(ctx, mk(func(s *zitiv1.ZitiServicePolicySpec) { s.Semantic = "SomeOf" }))).To(MatchError(ContainSubstring("Unsupported value")))
		p := mk(nil)
		Expect(k8sClient.Create(ctx, p)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, p) })
		Expect(p.Spec.Semantic).To(Equal("AnyOf"))
		p.Spec.Type = "Bind"
		Expect(k8sClient.Update(ctx, p)).To(MatchError(ContainSubstring("type is immutable")))
	})

	It("router policies need their roles and default the semantic", func() {
		Expect(k8sClient.Create(ctx, &zitiv1.ZitiEdgeRouterPolicy{ObjectMeta: meta("erp-bad"), Spec: zitiv1.ZitiEdgeRouterPolicySpec{IdentityRoles: []string{"#a"}}})).
			To(MatchError(ContainSubstring("edgeRouterRoles")))
		erp := &zitiv1.ZitiEdgeRouterPolicy{ObjectMeta: meta("erp-case"), Spec: zitiv1.ZitiEdgeRouterPolicySpec{IdentityRoles: []string{"#a"}, EdgeRouterRoles: []string{"#all"}}}
		Expect(k8sClient.Create(ctx, erp)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, erp) })
		Expect(erp.Spec.Semantic).To(Equal("AnyOf"))

		Expect(k8sClient.Create(ctx, &zitiv1.ZitiServiceEdgeRouterPolicy{ObjectMeta: meta("serp-bad"), Spec: zitiv1.ZitiServiceEdgeRouterPolicySpec{ServiceRoles: []string{"#a"}}})).
			To(MatchError(ContainSubstring("edgeRouterRoles")))
		serp := &zitiv1.ZitiServiceEdgeRouterPolicy{ObjectMeta: meta("serp-case"), Spec: zitiv1.ZitiServiceEdgeRouterPolicySpec{ServiceRoles: []string{"#a"}, EdgeRouterRoles: []string{"#all"}}}
		Expect(k8sClient.Create(ctx, serp)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, serp) })
		Expect(serp.Spec.Semantic).To(Equal("AnyOf"))
	})
})

var _ = Describe("ZitiRouter", func() {
	router := func(mut func(*zitiv1.ZitiRouterSpec)) *zitiv1.ZitiRouter {
		r := &zitiv1.ZitiRouter{Name: "router-case"}
		if mut != nil {
			mut(&r.Spec)
		}
		return r
	}

	It("applies defaults and keeps zitiName and deletionPolicy fixed", func() {
		r := router(func(s *zitiv1.ZitiRouterSpec) {
			s.ZitiName = "a"
			s.EnrollmentSecretRef = &zitiv1.SecretRef{Namespace: "routers", Name: "jwt"}
		})
		Expect(k8sClient.Create(ctx, r)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, r) })
		Expect(r.Spec.ConnectionRef).To(Equal("default"))
		Expect(r.Spec.DeletionPolicy).To(Equal(zitiv1.DeletionPolicyDelete))

		r.Spec.ZitiName = "b"
		Expect(k8sClient.Update(ctx, r)).To(MatchError(ContainSubstring("zitiName is immutable")))
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "router-case"}, r)).To(Succeed())
		r.Spec.DeletionPolicy = zitiv1.DeletionPolicyOrphan
		Expect(k8sClient.Update(ctx, r)).To(MatchError(ContainSubstring("deletionPolicy is immutable")))
	})

	DescribeTable("rejects bad values",
		func(mut func(*zitiv1.ZitiRouterSpec), wantErr string) {
			Expect(k8sClient.Create(ctx, router(mut))).To(MatchError(ContainSubstring(wantErr)))
		},
		Entry("negative cost", func(s *zitiv1.ZitiRouterSpec) { s.Cost = -1 }, "greater than or equal to 0"),
		Entry("cost over the limit", func(s *zitiv1.ZitiRouterSpec) { s.Cost = 70000 }, "less than or equal to 65535"),
		Entry("unknown deletionPolicy", func(s *zitiv1.ZitiRouterSpec) { s.DeletionPolicy = "Maybe" }, "Unsupported value"),
		Entry("deployment without a namespace", func(s *zitiv1.ZitiRouterSpec) {
			s.Deployment = &zitiv1.ZitiRouterDeployment{}
		}, "spec.deployment.namespace"),
		Entry("bad deployment namespace", func(s *zitiv1.ZitiRouterSpec) {
			s.AdvertisedAddress = "edge.example.com"
			s.Deployment = &zitiv1.ZitiRouterDeployment{Namespace: "Not A Name"}
		}, "spec.deployment.namespace"),
		Entry("unknown serviceType", func(s *zitiv1.ZitiRouterSpec) {
			s.AdvertisedAddress = "edge.example.com"
			s.Deployment = &zitiv1.ZitiRouterDeployment{Namespace: "routers", ServiceType: "Ingress"}
		}, "Unsupported value"),
	)

	It("applies the deployment defaults and allows an empty address", func() {
		r := router(func(s *zitiv1.ZitiRouterSpec) {
			s.Deployment = &zitiv1.ZitiRouterDeployment{Namespace: "routers"}
		})
		Expect(k8sClient.Create(ctx, r)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, r) })
		Expect(r.Spec.Deployment.ServiceType).To(Equal("LoadBalancer"))
		Expect(r.Spec.Deployment.ImagePullPolicy).To(Equal("IfNotPresent"))
		// A router that clients reach through other routers needs no address of its own.
		Expect(r.Spec.AdvertisedAddress).To(BeEmpty())
	})
})
