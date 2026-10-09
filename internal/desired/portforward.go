// SPDX-License-Identifier: Apache-2.0

package desired

import (
	"fmt"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	zitiv1 "github.com/aliAljaffer/openziti-operator/api/v1alpha1"
)

// IdentityFileKey is the Secret key where ZitiIdentity stores its enrolled client identity.
const IdentityFileKey = "identity.json"

// PortForwardDeployment builds the one-replica proxy pod that `kubectl port-forward` targets.
func PortForwardDeployment(pf *zitiv1.ZitiPortForward, identitySecret, version string) *appsv1.Deployment {
	labels := map[string]string{"app": pf.Name}
	replicas := int32(1)
	uid, noRoot, noEscalation := int64(2171), true, false
	return &appsv1.Deployment{
		Name: pf.Name, Namespace: pf.Namespace, Labels: labels,
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Strategy: appsv1.DeploymentStrategy{Type: appsv1.RecreateDeploymentStrategyType},
			Selector: &metav1.LabelSelector{MatchLabels: labels},
			Template: corev1.PodTemplateSpec{
				Labels: labels,
				Spec: corev1.PodSpec{
					SecurityContext: &corev1.PodSecurityContext{RunAsUser: &uid, RunAsNonRoot: &noRoot,
						SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault}},
					Containers: []corev1.Container{{
						Name:            "ziti-proxy",
						Image:           "openziti/ziti-router:" + strings.TrimPrefix(version, "v"),
						ImagePullPolicy: corev1.PullIfNotPresent,
						Command: []string{"ziti", "tunnel", "proxy", "-i", "/ziti/" + IdentityFileKey,
							"--svcPollRate", "15", fmt.Sprintf("%s:%d", pf.Spec.Service, pf.Spec.Port)},
						SecurityContext: &corev1.SecurityContext{AllowPrivilegeEscalation: &noEscalation},
						Ports:           []corev1.ContainerPort{{Name: "proxy", ContainerPort: pf.Spec.Port, Protocol: corev1.ProtocolTCP}},
						VolumeMounts:    []corev1.VolumeMount{{Name: "identity", MountPath: "/ziti", ReadOnly: true}},
						ReadinessProbe:  tcpProbe(pf.Spec.Port),
					}},
					Volumes: []corev1.Volume{{Name: "identity",
						Secret: &corev1.SecretVolumeSource{SecretName: identitySecret}}},
				},
			},
		},
	}
}

func tcpProbe(port int32) *corev1.Probe {
	return &corev1.Probe{
		TCPSocket:           &corev1.TCPSocketAction{Port: intstr.FromInt32(port)},
		InitialDelaySeconds: 5,
		PeriodSeconds:       10,
	}
}
