// SPDX-License-Identifier: Apache-2.0

package v1alpha1

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	admissionv1 "k8s.io/api/admissionregistration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/yaml"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	zitiv1 "github.com/aliAljaffer/openziti-operator/api/v1alpha1"
)

// This suite is the smoke test of the wiring the unit tests cannot reach: the generated
// ValidatingWebhookConfiguration, a manager serving the real handlers, and the API server calling them.
// It proves a denial the pure validators produce is the denial an edit gets.
var (
	whCtx     context.Context
	whCancel  context.CancelFunc
	testEnv   *envtest.Environment
	k8sClient client.Client
)

func TestWebhookServer(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Webhook server Suite")
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	whCtx, whCancel = context.WithCancel(context.Background())

	scheme := runtime.NewScheme()
	Expect(clientgoscheme.AddToScheme(scheme)).To(Succeed())
	Expect(zitiv1.AddToScheme(scheme)).To(Succeed())

	By("starting the test environment with the generated webhook configuration")
	testEnv = &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "..", "..", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
		WebhookInstallOptions: envtest.WebhookInstallOptions{
			ValidatingWebhooks: []*admissionv1.ValidatingWebhookConfiguration{singleSlashWebhookConfig()},
		},
	}
	if dir := firstEnvTestBinaryDir(); dir != "" {
		testEnv.BinaryAssetsDirectory = dir
	}
	cfg, err := testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme})
	Expect(err).NotTo(HaveOccurred())

	By("starting a manager that serves the webhooks")
	mgr, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme: scheme,
		WebhookServer: webhook.NewServer(webhook.Options{
			Host:    testEnv.WebhookInstallOptions.LocalServingHost,
			Port:    testEnv.WebhookInstallOptions.LocalServingPort,
			CertDir: testEnv.WebhookInstallOptions.LocalServingCertDir,
		}),
		Metrics:                metricsserver.Options{BindAddress: "0"},
		HealthProbeBindAddress: "0",
	})
	Expect(err).NotTo(HaveOccurred())
	Expect(Setup(mgr)).To(Succeed())

	go func() {
		defer GinkgoRecover()
		Expect(mgr.Start(whCtx)).To(Succeed())
	}()

	// The API server ignores an unreachable webhook because failurePolicy is Ignore, so a spec must only
	// run once the server answers. Otherwise the denials below would pass for the wrong reason.
	By("waiting for the webhook server to answer")
	Eventually(func() error {
		return mgr.GetWebhookServer().StartedChecker()(nil)
	}, time.Minute, time.Second).Should(Succeed())

	By("creating the ZitiConnection the other kinds resolve against")
	Expect(k8sClient.Create(whCtx, defaultConnection())).To(Succeed())
})

var _ = AfterSuite(func() {
	By("tearing down the test environment")
	whCancel()
	Expect(testEnv.Stop()).To(Succeed())
})

var _ = Describe("Validating webhook against the API server", func() {
	create := func(obj client.Object) error {
		return k8sClient.Create(whCtx, obj)
	}
	remove := func(obj client.Object) {
		_ = k8sClient.Delete(whCtx, obj)
	}

	It("allows a valid ZitiApp and denies one whose zitiName has a quote", func() {
		good := &zitiv1.ZitiApp{
			ObjectMeta: metav1.ObjectMeta{Name: "smoke-app-ok", Namespace: "default"},
			Spec: zitiv1.ZitiAppSpec{
				Expose:  zitiv1.Expose{Addresses: []string{"web.default.svc"}, Ports: []intstr.IntOrString{intstr.FromInt32(80)}},
				Targets: []zitiv1.Target{{Address: "web.default.svc", Port: 8080}},
			},
		}
		Expect(create(good)).To(Succeed())
		DeferCleanup(func() { remove(good) })

		bad := good.DeepCopy()
		bad.Name = "smoke-app-bad"
		bad.Spec.ZitiName = `has"quote`
		Expect(create(bad)).To(MatchError(ContainSubstring("quotes or backslashes")))
	})

	It("allows a valid ZitiIdentity and denies an externalId under a namespaced connection", func() {
		good := &zitiv1.ZitiIdentity{
			ObjectMeta: metav1.ObjectMeta{Name: "smoke-identity-ok", Namespace: "default"},
			Spec:       zitiv1.ZitiIdentitySpec{RoleAttributes: []string{"web"}},
		}
		Expect(create(good)).To(Succeed())
		DeferCleanup(func() { remove(good) })

		bad := good.DeepCopy()
		bad.Name = "smoke-identity-bad"
		bad.Spec.ExternalID = "any-subject"
		Expect(create(bad)).To(MatchError(ContainSubstring("roleScope Global")))
	})

	It("allows a valid ZitiAccessPolicy and denies #all under a namespaced connection", func() {
		good := &zitiv1.ZitiAccessPolicy{
			ObjectMeta: metav1.ObjectMeta{Name: "smoke-policy-ok", Namespace: "default"},
			Spec: zitiv1.ZitiAccessPolicySpec{
				IdentityRoles: []string{"#developers"},
				ServiceRoles:  []string{"#apps"},
			},
		}
		Expect(create(good)).To(Succeed())
		DeferCleanup(func() { remove(good) })

		bad := good.DeepCopy()
		bad.Name = "smoke-policy-bad"
		bad.Spec.IdentityRoles = []string{"#all"}
		Expect(create(bad)).To(MatchError(ContainSubstring("not allowed with roleScope Namespaced")))
	})

	It("denies a ZitiConnection whose allowedNamespaces is not a valid selector", func() {
		bad := defaultConnection()
		bad.Name = "smoke-connection-bad"
		bad.Spec.AllowedNamespaces = &metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{
			{Key: "tier", Operator: "NotAThing"},
		}}
		Expect(create(bad)).To(MatchError(ContainSubstring("allowedNamespaces")))
	})
})

// singleSlashWebhookConfig loads the generated configuration and drops the leading slash from every
// service path. envtest joins the path to the serving URL with a slash of its own, so a leading slash
// makes the URL path //validate-.... Go's mux cleans that to /validate-... and then redirects, so it can
// still reach the handler, but only through a redirect that is easy to lose. A single slash reaches it
// directly.
func singleSlashWebhookConfig() *admissionv1.ValidatingWebhookConfiguration {
	GinkgoHelper()
	f, err := os.Open(filepath.Join("..", "..", "..", "config", "webhook", "manifests.yaml"))
	Expect(err).NotTo(HaveOccurred())
	defer f.Close()

	var cfg admissionv1.ValidatingWebhookConfiguration
	Expect(yaml.NewYAMLOrJSONDecoder(f, 4096).Decode(&cfg)).To(Succeed())
	Expect(cfg.Webhooks).NotTo(BeEmpty())
	for i := range cfg.Webhooks {
		svc := cfg.Webhooks[i].ClientConfig.Service
		Expect(svc).NotTo(BeNil())
		Expect(svc.Path).NotTo(BeNil())
		*svc.Path = strings.TrimPrefix(*svc.Path, "/")
	}
	return &cfg
}

// defaultConnection is a valid cluster-scoped ZitiConnection named "default", the one the other kinds
// fall back to when they set no connectionRef.
func defaultConnection() *zitiv1.ZitiConnection {
	conn := &zitiv1.ZitiConnection{
		ObjectMeta: metav1.ObjectMeta{Name: "default"},
		Spec: zitiv1.ZitiConnectionSpec{
			ManagementURL:  "https://controller.example:443/edge/management/v1",
			HostingRouters: []string{"router-a"},
			EntryRouters:   []string{"router-a"},
		},
	}
	conn.Spec.CABundle.ConfigMapRef = zitiv1.ConfigMapKeyRef{Namespace: "default", Name: "ziti-ca", Key: "ca.crt"}
	conn.Spec.Auth.Updb = &zitiv1.UpdbAuth{SecretRef: zitiv1.SecretRef{Namespace: "default", Name: "ziti-admin"}}
	return conn
}

// firstEnvTestBinaryDir locates the envtest binaries so the suite also runs from an IDE, where the
// Makefile has not exported KUBEBUILDER_ASSETS.
func firstEnvTestBinaryDir() string {
	basePath := filepath.Join("..", "..", "..", "bin", "k8s")
	entries, err := os.ReadDir(basePath)
	if err != nil {
		logf.Log.Error(err, "Failed to read directory", "path", basePath)
		return ""
	}
	for _, entry := range entries {
		if entry.IsDir() {
			return filepath.Join(basePath, entry.Name())
		}
	}
	return ""
}
