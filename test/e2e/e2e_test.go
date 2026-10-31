//go:build e2e
// +build e2e

/*
Copyright 2026 The openziti-operator Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package e2e

import (
	"bytes"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openziti/edge-api/rest_util"

	"github.com/aliAljaffer/openziti-operator/test/utils"
)

// namespace where the project is deployed in
const namespace = "ziti-operator-system"

const appNamespace = "team-a"

// These names must match the Docker network names in the connection and app specs.
const (
	zitiControllerName = "ziti-e2e-ctrl"
	zitiTargetName     = "ziti-e2e-target"
)

// serviceAccountName created for the project
const serviceAccountName = "ziti-operator-controller-manager"

// metricsServiceName is the name of the metrics service of the project
const metricsServiceName = "ziti-operator-controller-manager-metrics-service"

// metricsRoleBindingName is the name of the RBAC that will be created to allow get the metrics data
const metricsRoleBindingName = "ziti-operator-metrics-binding"

var _ = Describe("Manager", Ordered, func() {
	var controllerPodName string

	// Before running the tests, set up the environment by creating the namespace,
	// enforce the restricted security policy to the namespace, installing CRDs,
	// and deploying the controller.
	BeforeAll(func() {
		By("starting a Ziti controller and target on the kind network")
		startZiti()

		By("creating manager namespace")
		applyYAML(fmt.Sprintf("apiVersion: v1\nkind: Namespace\nmetadata:\n  name: %s\n", namespace))

		By("labeling the namespace to enforce the restricted security policy")
		cmd := exec.Command("kubectl", "label", "--overwrite", "ns", namespace,
			"pod-security.kubernetes.io/enforce=restricted")
		_, err := utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to label namespace with restricted policy")

		By("storing the Ziti CA and credentials")
		createCredential()

		By("installing CRDs")
		cmd = exec.Command("make", "install")
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to install CRDs")

		By("deploying the controller-manager")
		cmd = exec.Command("make", "deploy", fmt.Sprintf("IMG=%s", managerImage))
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to deploy the controller-manager")
	})

	// After all tests have been executed, clean up by undeploying the controller, uninstalling CRDs,
	// and deleting the namespace.
	AfterAll(func() {
		By("cleaning up the curl pod for metrics")
		cmd := exec.Command("kubectl", "delete", "pod", "curl-metrics", "-n", namespace)
		_, _ = utils.Run(cmd)

		By("deleting the Ziti resources")
		deleteZitiResources()

		By("undeploying the controller-manager")
		cmd = exec.Command("make", "undeploy")
		_, _ = utils.Run(cmd)

		By("uninstalling CRDs")
		cmd = exec.Command("make", "uninstall")
		_, _ = utils.Run(cmd)

		By("removing manager namespace")
		cmd = exec.Command("kubectl", "delete", "ns", namespace)
		_, _ = utils.Run(cmd)

		By("stopping the Ziti controller and target")
		stopZiti()
	})

	// After each test, check for failures and collect logs, events,
	// and pod descriptions for debugging.
	AfterEach(func() {
		specReport := CurrentSpecReport()
		if specReport.Failed() {
			By("Fetching controller manager pod logs")
			cmd := exec.Command("kubectl", "logs", controllerPodName, "-n", namespace)
			controllerLogs, err := utils.Run(cmd)
			if err == nil {
				_, _ = fmt.Fprintf(GinkgoWriter, "Controller logs:\n %s", controllerLogs)
			} else {
				_, _ = fmt.Fprintf(GinkgoWriter, "Failed to get Controller logs: %s", err)
			}

			By("Fetching Kubernetes events")
			cmd = exec.Command("kubectl", "get", "events", "-n", namespace, "--sort-by=.lastTimestamp")
			eventsOutput, err := utils.Run(cmd)
			if err == nil {
				_, _ = fmt.Fprintf(GinkgoWriter, "Kubernetes events:\n%s", eventsOutput)
			} else {
				_, _ = fmt.Fprintf(GinkgoWriter, "Failed to get Kubernetes events: %s", err)
			}

			By("Fetching curl-metrics logs")
			cmd = exec.Command("kubectl", "logs", "curl-metrics", "-n", namespace)
			metricsOutput, err := utils.Run(cmd)
			if err == nil {
				_, _ = fmt.Fprintf(GinkgoWriter, "Metrics logs:\n %s", metricsOutput)
			} else {
				_, _ = fmt.Fprintf(GinkgoWriter, "Failed to get curl-metrics logs: %s", err)
			}

			By("Fetching controller manager pod description")
			cmd = exec.Command("kubectl", "describe", "pod", controllerPodName, "-n", namespace)
			podDescription, err := utils.Run(cmd)
			if err == nil {
				fmt.Println("Pod description:\n", podDescription)
			} else {
				fmt.Println("Failed to describe controller pod")
			}
		}
	})

	SetDefaultEventuallyTimeout(2 * time.Minute)
	SetDefaultEventuallyPollingInterval(time.Second)

	Context("Manager", func() {
		It("should run successfully", func() {
			By("validating that the controller-manager pod is running as expected")
			verifyControllerUp := func(g Gomega) {
				By("getting the name of the controller-manager pod")
				cmd := exec.Command("kubectl", "get",
					"pods", "-l", "control-plane=controller-manager",
					"-o", "go-template={{ range .items }}"+
						"{{ if not .metadata.deletionTimestamp }}"+
						"{{ .metadata.name }}"+
						"{{ \"\\n\" }}{{ end }}{{ end }}",
					"-n", namespace,
				)

				podOutput, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred(), "Failed to retrieve controller-manager pod information")
				podNames := utils.GetNonEmptyLines(podOutput)
				g.Expect(podNames).To(HaveLen(2), "expected 2 controller pods running, one leader and one standby")
				controllerPodName = podNames[0]
				g.Expect(controllerPodName).To(ContainSubstring("controller-manager"))

				By("validating the pod's status")
				cmd = exec.Command("kubectl", "get",
					"pods", controllerPodName, "-o", "jsonpath={.status.phase}",
					"-n", namespace,
				)
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("Running"), "Incorrect controller-manager pod status")
			}
			Eventually(verifyControllerUp).Should(Succeed())
		})

		It("should ensure the metrics endpoint is serving metrics", func() {
			By("creating a ClusterRoleBinding for the service account to allow access to metrics")
			cmd := exec.Command("kubectl", "create", "clusterrolebinding", metricsRoleBindingName,
				"--clusterrole=ziti-operator-metrics-reader",
				fmt.Sprintf("--serviceaccount=%s:%s", namespace, serviceAccountName),
			)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to create ClusterRoleBinding")

			By("validating that the metrics service is available")
			cmd = exec.Command("kubectl", "get", "service", metricsServiceName, "-n", namespace)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Metrics service should exist")

			By("getting the service account token")
			token, err := serviceAccountToken()
			Expect(err).NotTo(HaveOccurred())
			Expect(token).NotTo(BeEmpty())

			By("ensuring the controller pod is ready")
			verifyControllerPodReady := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "pod", controllerPodName, "-n", namespace,
					"-o", "jsonpath={.status.conditions[?(@.type=='Ready')].status}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("True"), "Controller pod not ready")
			}
			Eventually(verifyControllerPodReady, 3*time.Minute, time.Second).Should(Succeed())

			By("verifying that the controller manager is serving the metrics server")
			verifyMetricsServerStarted := func(g Gomega) {
				cmd := exec.Command("kubectl", "logs", controllerPodName, "-n", namespace)
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(ContainSubstring("Serving metrics server"),
					"Metrics server not yet started")
			}
			Eventually(verifyMetricsServerStarted, 3*time.Minute, time.Second).Should(Succeed())

			// +kubebuilder:scaffold:e2e-metrics-webhooks-readiness

			By("creating the curl-metrics pod to access the metrics endpoint")
			cmd = exec.Command("kubectl", "run", "curl-metrics", "--restart=Never",
				"--namespace", namespace,
				"--image=curlimages/curl:latest",
				"--overrides",
				fmt.Sprintf(`{
					"spec": {
						"containers": [{
							"name": "curl",
							"image": "curlimages/curl:latest",
							"command": ["/bin/sh", "-c"],
							"args": [
								"for i in $(seq 1 30); do curl -v -k -H 'Authorization: Bearer %s' https://%s.%s.svc.cluster.local:8443/metrics && exit 0 || sleep 2; done; exit 1"
							],
							"securityContext": {
								"readOnlyRootFilesystem": true,
								"allowPrivilegeEscalation": false,
								"capabilities": {
									"drop": ["ALL"]
								},
								"runAsNonRoot": true,
								"runAsUser": 1000,
								"seccompProfile": {
									"type": "RuntimeDefault"
								}
							}
						}],
						"serviceAccountName": "%s"
					}
				}`, token, metricsServiceName, namespace, serviceAccountName))
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to create curl-metrics pod")

			By("waiting for the curl-metrics pod to complete.")
			verifyCurlUp := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "pods", "curl-metrics",
					"-o", "jsonpath={.status.phase}",
					"-n", namespace)
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("Succeeded"), "curl pod in wrong status")
			}
			Eventually(verifyCurlUp, 5*time.Minute).Should(Succeed())

			By("getting the metrics by checking curl-metrics logs")
			verifyMetricsAvailable := func(g Gomega) {
				metricsOutput, err := getMetricsOutput()
				g.Expect(err).NotTo(HaveOccurred(), "Failed to retrieve logs from curl pod")
				g.Expect(metricsOutput).NotTo(BeEmpty())
				g.Expect(metricsOutput).To(ContainSubstring("< HTTP/1.1 200 OK"))
			}
			Eventually(verifyMetricsAvailable, 2*time.Minute).Should(Succeed())
		})

		// +kubebuilder:scaffold:e2e-webhooks-checks
	})

	Context("Operator", func() {
		It("reconciles a connection, identity, and app against Ziti", func() {
			By("creating the app namespace")
			applyYAML(fmt.Sprintf("apiVersion: v1\nkind: Namespace\nmetadata:\n  name: %s\n", appNamespace))
			DeferCleanup(func() {
				deleteZitiResources()
				cmd := exec.Command("kubectl", "delete", "ns", appNamespace)
				_, _ = utils.Run(cmd)
			})

			By("creating the ZitiConnection")
			applyYAML(connectionYAML())
			Eventually(kubectlCondition("ziticonnection", "", "default", "Connected"), 2*time.Minute).Should(Equal("True"))

			By("creating the ZitiIdentity")
			applyYAML(identityYAML())
			Eventually(kubectlCondition("zitiidentity", appNamespace, "e2e", "Ready"), 2*time.Minute).Should(Equal("True"))

			By("creating the ZitiApp")
			applyYAML(appYAML())
			Eventually(kubectlCondition("zitiapp", appNamespace, "e2e", "Hosted"), 3*time.Minute).Should(Equal("True"))
			Eventually(kubectlCondition("zitiapp", appNamespace, "e2e", "Ready"), 3*time.Minute).Should(Equal("True"))
		})
	})
})

// serviceAccountToken returns a token for the specified service account in the given namespace.
// It uses the Kubernetes TokenRequest API to generate a token by directly sending a request
// and parsing the resulting token from the API response.
func serviceAccountToken() (string, error) {
	const tokenRequestRawString = `{
		"apiVersion": "authentication.k8s.io/v1",
		"kind": "TokenRequest"
	}`

	By("creating temporary file to store the token request")
	secretName := fmt.Sprintf("%s-token-request", serviceAccountName)
	tokenRequestFile := filepath.Join("/tmp", secretName)
	err := os.WriteFile(tokenRequestFile, []byte(tokenRequestRawString), os.FileMode(0o644))
	if err != nil {
		return "", err
	}

	var out string
	verifyTokenCreation := func(g Gomega) {
		By("executing kubectl command to create the token")
		cmd := exec.Command("kubectl", "create", "--raw", fmt.Sprintf(
			"/api/v1/namespaces/%s/serviceaccounts/%s/token",
			namespace,
			serviceAccountName,
		), "-f", tokenRequestFile)

		output, err := cmd.CombinedOutput()
		g.Expect(err).NotTo(HaveOccurred())

		By("parsing the JSON output to extract the token")
		var token tokenRequest
		err = json.Unmarshal(output, &token)
		g.Expect(err).NotTo(HaveOccurred())

		out = token.Status.Token
	}
	Eventually(verifyTokenCreation).Should(Succeed())

	return out, err
}

// getMetricsOutput retrieves and returns the logs from the curl pod used to access the metrics endpoint.
func getMetricsOutput() (string, error) {
	By("getting the curl-metrics logs")
	cmd := exec.Command("kubectl", "logs", "curl-metrics", "-n", namespace)
	return utils.Run(cmd)
}

// tokenRequest is a simplified representation of the Kubernetes TokenRequest API response,
// containing only the token field that we need to extract.
type tokenRequest struct {
	Status struct {
		Token string `json:"token"`
	} `json:"status"`
}

func startZiti() {
	stopZiti()
	run := func(args ...string) {
		out, err := utils.Run(exec.Command("docker", args...))
		ExpectWithOffset(1, err).NotTo(HaveOccurred(), "docker %s failed: %s", strings.Join(args, " "), out)
	}
	run("run", "-d", "--name", zitiControllerName, "--network", "kind", "-p", "1280:1280",
		"openziti/ziti-cli:2.0.4",
		"edge", "quickstart",
		"--ctrl-address", zitiControllerName,
		"--ctrl-port", "1280",
		"--router-address", zitiControllerName,
		"--password", "admin123")
	run("run", "-d", "--name", zitiTargetName, "--network", "kind", "nginx:alpine")
}

func stopZiti() {
	for _, name := range []string{zitiControllerName, zitiTargetName} {
		_, _ = utils.Run(exec.Command("docker", "rm", "-f", name))
	}
}

func createCredential() {
	var caPEM []byte
	Eventually(func(g Gomega) {
		certs, err := rest_util.GetControllerWellKnownCas("https://localhost:1280")
		g.Expect(err).NotTo(HaveOccurred())
		var buf bytes.Buffer
		for _, cert := range certs {
			g.Expect(pem.Encode(&buf, &pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw})).To(Succeed())
		}
		caPEM = buf.Bytes()
	}, 3*time.Minute, 2*time.Second).Should(Succeed())

	file, err := os.CreateTemp("", "ziti-ca-*.crt")
	Expect(err).NotTo(HaveOccurred())
	defer func() { _ = os.Remove(file.Name()) }()
	_, err = file.Write(caPEM)
	Expect(err).NotTo(HaveOccurred())
	Expect(file.Close()).To(Succeed())

	cm, err := utils.Run(exec.Command("kubectl", "create", "configmap", "ziti-root-ca", "-n", namespace,
		"--from-file=ca.crt="+file.Name(), "--dry-run=client", "-o", "yaml"))
	Expect(err).NotTo(HaveOccurred(), "Failed to render the CA ConfigMap")
	applyYAML(cm)

	secret, err := utils.Run(exec.Command("kubectl", "create", "secret", "generic", "ziti-operator-credential",
		"-n", namespace, "--from-literal=username=admin", "--from-literal=password=admin123",
		"--dry-run=client", "-o", "yaml"))
	Expect(err).NotTo(HaveOccurred(), "Failed to render the credential Secret")
	applyYAML(secret)
}

func applyYAML(manifest string) {
	cmd := exec.Command("kubectl", "apply", "-f", "-")
	cmd.Stdin = strings.NewReader(manifest)
	out, err := utils.Run(cmd)
	ExpectWithOffset(1, err).NotTo(HaveOccurred(), "kubectl apply failed: %s", out)
}

func kubectlCondition(kind, ns, name, condition string) func(Gomega) string {
	return func(g Gomega) string {
		jsonpath := fmt.Sprintf("jsonpath={.status.conditions[?(@.type=='%s')].status}", condition)
		args := []string{"get", kind, name, "-o", jsonpath}
		if ns != "" {
			args = append(args, "-n", ns)
		}
		out, err := utils.Run(exec.Command("kubectl", args...))
		g.Expect(err).NotTo(HaveOccurred())
		return out
	}
}

func deleteZitiResources() {
	for _, r := range []struct{ kind, namespace, name string }{
		{"zitiapp", appNamespace, "e2e"},
		{"zitiidentity", appNamespace, "e2e"},
		{"ziticonnection", "", "default"},
	} {
		args := []string{"delete", r.kind, r.name, "--ignore-not-found", "--timeout=30s"}
		if r.namespace != "" {
			args = append(args, "-n", r.namespace)
		}
		if out, err := utils.Run(exec.Command("kubectl", args...)); err != nil {
			patch := []string{"patch", r.kind, r.name, "--type=merge", "-p", `{"metadata":{"finalizers":null}}`}
			if r.namespace != "" {
				patch = append(patch, "-n", r.namespace)
			}
			_, _ = utils.Run(exec.Command("kubectl", patch...))
			_, _ = fmt.Fprintf(GinkgoWriter, "cleanup: %v: %s\n", err, out)
		}
	}
}

func connectionYAML() string {
	return fmt.Sprintf(`apiVersion: ziti.alialjaffer.com/v1alpha1
kind: ZitiConnection
metadata:
  name: default
spec:
  managementUrl: https://%s:1280/edge/management/v1
  caBundle:
    configMapRef:
      namespace: %s
      name: ziti-root-ca
      key: ca.crt
  auth:
    updb:
      secretRef:
        namespace: %s
        name: ziti-operator-credential
  roleScope: Global
  hostingRouters:
    - router-instance-1
`, zitiControllerName, namespace, namespace)
}

func identityYAML() string {
	return fmt.Sprintf(`apiVersion: ziti.alialjaffer.com/v1alpha1
kind: ZitiIdentity
metadata:
  name: e2e
  namespace: %s
spec:
  connectionRef: default
  roleAttributes:
    - team-a
  enrollmentMode: OperatorEnrolled
`, appNamespace)
}

func appYAML() string {
	return fmt.Sprintf(`apiVersion: ziti.alialjaffer.com/v1alpha1
kind: ZitiApp
metadata:
  name: e2e
  namespace: %s
spec:
  connectionRef: default
  expose:
    addresses:
      - e2e.example.test
    ports:
      - 80
    protocols:
      - tcp
  targets:
    - address: %s
      port: 80
  allow:
    groups:
      - team-a
  entryRouters:
    - router-instance-1
`, appNamespace, zitiTargetName)
}
