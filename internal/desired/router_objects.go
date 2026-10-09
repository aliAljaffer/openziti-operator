// SPDX-License-Identifier: Apache-2.0

package desired

import (
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	zitiv1 "github.com/aliAljaffer/openziti-operator/api/v1alpha1"
)

// DataDir is where the ziti-router image keeps its configuration, certificate, and keys.
const DataDir = "/ziti-router"

// EnrollTokenKey is the Secret key the enrollment JWT lives under. The controller writes it, and the router pod
// reads it by reference.
const EnrollTokenKey = "enrollment.jwt"

// StorageRequested is the size the router volume asks for. A router holds a certificate and a key, not data.
const StorageRequested = "100Mi"

// RouterWorkload holds what spec.deployment of a ZitiRouter needs. It carries the same fields as RouterManifest, so
// the objects the operator creates and the manifests it writes for a VM cannot drift apart.
type RouterWorkload struct {
	RouterManifest
	Namespace       string
	Image           string
	ImagePullPolicy corev1.PullPolicy
	ServiceType     corev1.ServiceType
}

// NewRouterWorkload builds the workload inputs for a router. An empty version means the ZitiConnection is not
// connected yet, so there is no image to pin.
func NewRouterWorkload(rt *zitiv1.ZitiRouter, version string) RouterWorkload {
	w := RouterWorkload{
		Name:         rt.Name,
		Address:      rt.Spec.AdvertisedAddress,
		Version:      version,
		StorageClass: rt.Spec.StorageClassName,
		Port:         rt.Spec.Port,
	}
	if w.Port == 0 {
		w.Port = 3022
	}
	if d := rt.Spec.Deployment; d != nil {
		w.Namespace = d.Namespace
		w.Image = d.Image
		w.ImagePullPolicy = corev1.PullPolicy(d.ImagePullPolicy)
		w.ServiceType = corev1.ServiceType(d.ServiceType)
	}
	if w.ImagePullPolicy == "" {
		w.ImagePullPolicy = corev1.PullIfNotPresent
	}
	if w.ServiceType == "" {
		w.ServiceType = corev1.ServiceTypeClusterIP
	}
	return w
}

// Names returns the names of the workload objects: the Deployment, the Service, and the volume claim.
func (w RouterWorkload) Names() (deployment, service, claim string) {
	n := w.k8sName()
	return n, n, n + "-data"
}

// SecretName is where the operator writes the enrollment JWT when the spec does not name a Secret.
func (w RouterWorkload) SecretName() string { return w.k8sName() + "-enrollment" }

func (w RouterWorkload) image() string {
	if w.Image != "" {
		return w.Image
	}
	return w.RouterManifest.image()
}

func (w RouterWorkload) labels() map[string]string { return map[string]string{"app": w.k8sName()} }

// PersistentVolumeClaim is the router volume. The image writes as uid 2171 and a fresh volume is root-owned,
// so the pod sets fsGroup before the container starts.
func (w RouterWorkload) PersistentVolumeClaim() *corev1.PersistentVolumeClaim {
	_, _, claim := w.Names()
	var storageClass *string
	if w.StorageClass != "" {
		storageClass = &w.StorageClass
	}
	return &corev1.PersistentVolumeClaim{
		Name: claim, Namespace: w.Namespace, Labels: w.labels(),
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes:      []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources:        corev1.VolumeResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse(StorageRequested)}},
			StorageClassName: storageClass,
		},
	}
}

// Deployment runs the router. One replica only: a second router process with the same identity would fight the first.
// The JWT is read from the Secret by reference and optional: the operator removes it once the router has enrolled, and
// the router then restarts from its volume alone.
func (w RouterWorkload) Deployment(secretName string) *appsv1.Deployment {
	name, _, claim := w.Names()
	fsGroup, runAsUser, noRoot, noEscalation := int64(routerUID), int64(routerUID), true, false
	replicas := int32(1)
	return &appsv1.Deployment{
		Name: name, Namespace: w.Namespace, Labels: w.labels(),
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Strategy: appsv1.DeploymentStrategy{Type: appsv1.RecreateDeploymentStrategyType},
			Selector: &metav1.LabelSelector{MatchLabels: w.labels()},
			Template: corev1.PodTemplateSpec{
				Labels: w.labels(),
				Spec: corev1.PodSpec{
					SecurityContext: &corev1.PodSecurityContext{
						FSGroup:        &fsGroup,
						RunAsUser:      &runAsUser,
						RunAsNonRoot:   &noRoot,
						SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
					},
					Containers: []corev1.Container{{
						Name:            "router",
						Image:           w.image(),
						ImagePullPolicy: w.ImagePullPolicy,
						SecurityContext: &corev1.SecurityContext{AllowPrivilegeEscalation: &noEscalation},
						Ports:           []corev1.ContainerPort{{Name: "ziti", ContainerPort: w.Port, Protocol: corev1.ProtocolTCP}},
						Env:             w.containerEnv(secretName),
						VolumeMounts:    []corev1.VolumeMount{{Name: "data", MountPath: DataDir}},
						LivenessProbe:   w.liveness(),
					}},
					Volumes: []corev1.Volume{{Name: "data",
						PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: claim}}},
				},
			},
		},
	}
}

func (w RouterWorkload) containerEnv(secretName string) []corev1.EnvVar {
	optional := true
	out := make([]corev1.EnvVar, 0, len(w.env()))
	for _, kv := range w.env() {
		if kv[0] == "ZITI_ENROLL_TOKEN" {
			out = append(out, corev1.EnvVar{Name: kv[0], ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{
				Name:     secretName,
				Key:      EnrollTokenKey,
				Optional: &optional,
			}}})
			continue
		}
		out = append(out, corev1.EnvVar{Name: kv[0], Value: kv[1]})
	}
	return out
}

// liveness checks the port the router listens on. It needs nothing from the image but the socket.
func (w RouterWorkload) liveness() *corev1.Probe {
	return &corev1.Probe{
		TCPSocket:           &corev1.TCPSocketAction{Port: intstr.FromInt32(w.Port)},
		InitialDelaySeconds: 30,
		PeriodSeconds:       30,
	}
}

// Service is the address other routers and clients reach the router on.
func (w RouterWorkload) Service() *corev1.Service {
	name, _, _ := w.Names()
	return &corev1.Service{
		Name: name, Namespace: w.Namespace, Labels: w.labels(),
		Spec: corev1.ServiceSpec{
			Type:     w.ServiceType,
			Selector: w.labels(),
			Ports: []corev1.ServicePort{{
				Name: "ziti", Port: w.Port, TargetPort: intstr.FromInt32(w.Port), Protocol: corev1.ProtocolTCP,
			}},
		},
	}
}
