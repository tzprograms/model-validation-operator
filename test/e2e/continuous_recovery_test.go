//go:build integration

// Copyright 2026 The Sigstore Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package e2e

import (
	"errors"
	"fmt"
	"os/exec"
	"time"

	. "github.com/onsi/ginkgo/v2" //nolint:revive
	. "github.com/onsi/gomega"    //nolint:revive
	corev1 "k8s.io/api/core/v1"

	"github.com/sigstore/model-validation-operator/internal/constants"
	utils "github.com/sigstore/model-validation-operator/test/utils"
)

var _ = Describe("Continuous validation readiness recovery", func() {
	It("removes two workloads from readiness after tampering and restores them after repair", func() {
		const modelName = "continuous-recovery-model"
		podNames := []string{"continuous-recovery-one", "continuous-recovery-two"}
		DeferCleanup(func() {
			for _, name := range podNames {
				deleteE2EResource("pod", name)
			}
			deleteE2EResource("modelvalidation", modelName)
		})

		resource := []byte(fmt.Sprintf(`apiVersion: ml.sigstore.dev/v1alpha1
kind: ModelValidation
metadata:
  name: %s
  namespace: %s
spec:
  config:
    publicKeyConfig:
      keyPath: /keys/test_public_key.pub
  model:
    path: /validated-model
    signaturePath: /validated-model/model.sig
  imagePullPolicy: IfNotPresent
  continuousValidation:
    enabled: true
    interval: 1m
    failureThreshold: 1
`, modelName, webhookTestNamespace))
		Expect(utils.KubectlApply(resource, nil)).To(Succeed())

		// Each workload gets a private copy. Tampering never changes the shared
		// node fixture used by existing public-key or legacy-sidecar tests.
		for _, name := range podNames {
			pod := []byte(fmt.Sprintf(`apiVersion: v1
kind: Pod
metadata:
  name: %s
  namespace: %s
  labels:
    validation.ml.sigstore.dev/ml: %s
spec:
  restartPolicy: Never
  terminationGracePeriodSeconds: 1
  initContainers:
  - name: copy-signed-model
    image: busybox:latest
    imagePullPolicy: IfNotPresent
    command: ["sh", "-ec", "cp -R /source/. /validated-model/ && chmod -R a+rX /validated-model"]
    volumeMounts:
    - {name: source, mountPath: /source, readOnly: true}
    - {name: model-copy, mountPath: /validated-model}
  containers:
  - name: workload
    image: busybox:latest
    imagePullPolicy: IfNotPresent
    command: ["sh", "-c", "sleep 3600"]
    volumeMounts:
    - {name: source, mountPath: /source, readOnly: true}
    - {name: model-copy, mountPath: /validated-model}
    - {name: keys, mountPath: /keys, readOnly: true}
  volumes:
  - name: source
    hostPath: {path: /tmp/e2e-model-data, type: Directory}
  - name: keys
    hostPath: {path: /tmp/e2e-keys-data, type: Directory}
  - name: model-copy
    emptyDir: {}
`, name, webhookTestNamespace, modelName))
			Expect(utils.KubectlApply(pod, nil)).To(Succeed())
		}

		uids := make(map[string]string, len(podNames))
		for _, name := range podNames {
			var pod corev1.Pod
			Expect(utils.KubectlGetJSON("pod", name, webhookTestNamespace, &pod)).To(Succeed())
			uids[name] = string(pod.UID)
		}
		expectReadiness := func(want bool) {
			readyCondition := corev1.ConditionFalse
			if want {
				readyCondition = corev1.ConditionTrue
			}
			Eventually(func(g Gomega) {
				for _, name := range podNames {
					var pod corev1.Pod
					g.Expect(utils.KubectlGetJSON("pod", name, webhookTestNamespace, &pod)).To(Succeed())
					g.Expect(string(pod.UID)).To(Equal(uids[name]), "recovery must use the same Pod")
					found := false
					for _, status := range pod.Status.InitContainerStatuses {
						if status.Name == constants.ModelValidationInitContainerName {
							found = true
							g.Expect(status.Ready).To(Equal(want), "validation readiness for %s", name)
							g.Expect(status.RestartCount).To(BeZero(), "recovery must not require an agent restart")
						}
					}
					g.Expect(found).To(BeTrue(), "the validation sidecar must execute for %s", name)
					g.Expect(pod.Status.Conditions).To(ContainElement(SatisfyAll(
						HaveField("Type", corev1.PodReady),
						HaveField("Status", readyCondition),
					)))
				}
			}, 3*time.Minute, 3*time.Second).Should(Succeed())
		}
		expectReadiness(true)

		restoreModels := func() error {
			var failures []error
			for _, name := range podNames {
				_, err := utils.Run(exec.Command("kubectl", "exec", name, "-n", webhookTestNamespace,
					"-c", "workload", "--request-timeout=15s", "--", "sh", "-ec",
					"cp /source/saved_model.pb /validated-model/saved_model.pb"))
				if err != nil {
					failures = append(failures, fmt.Errorf("restore model in %s: %w", name, err))
				}
			}
			return errors.Join(failures...)
		}
		DeferCleanup(func() { Expect(restoreModels()).To(Succeed()) })

		By("tampering with the signed file in every workload copy")
		for _, name := range podNames {
			_, err := utils.Run(exec.Command("kubectl", "exec", name, "-n", webhookTestNamespace,
				"-c", "workload", "--request-timeout=15s", "--", "sh", "-ec",
				"printf tampered > /validated-model/saved_model.pb"))
			Expect(err).NotTo(HaveOccurred())
		}
		expectReadiness(false)
		for _, name := range podNames {
			logs, err := utils.Run(exec.Command("kubectl", "logs", name, "-n", webhookTestNamespace,
				"-c", constants.ModelValidationInitContainerName, "--request-timeout=15s"))
			Expect(err).NotTo(HaveOccurred())
			Expect(logs).To(ContainSubstring("hash mismatch for 'saved_model.pb'"))
		}

		By("restoring every signed file and waiting for periodic validation to recover readiness")
		Expect(restoreModels()).To(Succeed())
		expectReadiness(true)
	})
})
