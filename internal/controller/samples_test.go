// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/yaml"
)

var _ = Describe("Sample manifests", func() {
	It("passes CRD validation", func() {
		files, err := filepath.Glob(filepath.Join("..", "..", "config", "samples", "ziti_*.yaml"))
		Expect(err).NotTo(HaveOccurred())
		Expect(files).NotTo(BeEmpty())

		var created []*unstructured.Unstructured
		DeferCleanup(func() {
			for _, obj := range created {
				_ = k8sClient.Delete(ctx, obj)
			}
		})

		for _, file := range files {
			data, err := os.ReadFile(file)
			Expect(err).NotTo(HaveOccurred())

			dec := yaml.NewYAMLOrJSONDecoder(bytes.NewReader(data), 4096)
			for {
				obj := &unstructured.Unstructured{}
				err := dec.Decode(obj)
				if errors.Is(err, io.EOF) {
					break
				}
				Expect(err).NotTo(HaveOccurred(), file)
				if obj.GetKind() == "" {
					continue
				}
				if ns := obj.GetNamespace(); ns != "" {
					err := k8sClient.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}})
					if !apierrors.IsAlreadyExists(err) {
						Expect(err).NotTo(HaveOccurred(), file)
					}
				}

				By("creating " + filepath.Base(file) + " " + obj.GetKind() + "/" + obj.GetName())
				Expect(k8sClient.Create(ctx, obj)).To(Succeed(), file)
				created = append(created, obj)
			}
		}
	})
})
