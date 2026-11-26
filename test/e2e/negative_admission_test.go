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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"time"

	. "github.com/onsi/ginkgo/v2" //nolint:revive
	. "github.com/onsi/gomega"    //nolint:revive
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/sigstore/model-validation-operator/internal/constants"
	utils "github.com/sigstore/model-validation-operator/test/utils"
)

const webhookRecoveryPodKind = "Pod"

var _ = Describe("Negative and failure scenarios", Ordered, func() {
	It("rejects malformed ModelValidation resources at the API boundary", func() {
		malformed := []byte(fmt.Sprintf(`apiVersion: ml.sigstore.dev/v1alpha1
kind: ModelValidation
metadata:
  name: malformed-validation
  namespace: %s
spec:
  config: {}
  model:
    path: relative/path
    signaturePath: ""
`, webhookTestNamespace))

		err := utils.KubectlApply(malformed, nil)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(Or(ContainSubstring("exactly one validation method"),
			ContainSubstring("spec.model.path"), ContainSubstring("spec.model.signaturePath")))
	})

	It("fails closed when a labeled pod references a missing CR", func() {
		const podName = "missing-cr-pod"
		DeferCleanup(deleteE2EResource, "pod", podName)

		err := utils.KubectlApply(podTemplate, authFixturePodData(podName, "does-not-exist"))
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("does-not-exist"))
	})

	It("fails closed while all webhook replicas are unavailable", func() {
		const (
			deploymentName = "model-validation-controller-manager"
			modelName      = "operator-down-model"
			podName        = "operator-down-pod"
		)

		Expect(utils.KubectlApply(modelValidationTemplate, utils.CRTemplateData{
			ModelName: modelName,
			Namespace: webhookTestNamespace,
		})).To(Succeed())
		DeferCleanup(deleteE2EResource, "modelvalidation", modelName)

		replicaText, err := utils.KubectlGet("deployment", deploymentName, operatorNamespace,
			"jsonpath={.spec.replicas}")
		Expect(err).NotTo(HaveOccurred())
		replicas, err := strconv.Atoi(replicaText)
		Expect(err).NotTo(HaveOccurred())

		DeferCleanup(func() {
			_, restoreErr := utils.Run(exec.Command("kubectl", "scale", "deployment", deploymentName,
				"-n", operatorNamespace, fmt.Sprintf("--replicas=%d", replicas), "--request-timeout=15s"))
			Expect(restoreErr).NotTo(HaveOccurred())
			_, restoreErr = utils.Run(exec.Command("kubectl", "rollout", "status", "deployment/"+deploymentName,
				"-n", operatorNamespace, "--timeout=120s"))
			Expect(restoreErr).NotTo(HaveOccurred())
			_, restoreErr = utils.Run(exec.Command("kubectl", "wait", "pod",
				"-l", "control-plane=controller-manager", "-n", operatorNamespace,
				"--for=condition=Ready", "--timeout=120s"))
			Expect(restoreErr).NotTo(HaveOccurred())
			Eventually(webhookEndpointAddresses, 60*time.Second, 2*time.Second).ShouldNot(BeEmpty())

			By("proving real admission has recovered before running later failure scenarios")
			// Exercise the full API server -> Service -> webhook path after the
			// managers restart. Use dry-run so retries do not leave Pods or
			// tracking state behind.
			probe := corev1.Pod{
				TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: webhookRecoveryPodKind},
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: "webhook-recovery-probe-", Namespace: webhookTestNamespace,
					Labels: map[string]string{constants.ModelValidationLabel: modelName},
				},
				Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "workload", Image: "busybox:latest"}}},
			}
			body, marshalErr := json.Marshal(probe)
			Expect(marshalErr).NotTo(HaveOccurred())
			consecutiveSuccesses := 0
			Eventually(func() error {
				ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
				defer cancel()
				cmd := exec.CommandContext(ctx, "kubectl", "create", "--dry-run=server", "--request-timeout=10s",
					"-f", "-", "-o", "json")
				cmd.Stdin = bytes.NewReader(body)
				var stderr bytes.Buffer
				cmd.Stderr = &stderr
				output, probeErr := cmd.Output()
				if probeErr != nil {
					consecutiveSuccesses = 0
					return fmt.Errorf("webhook recovery admission failed: %w: %s", probeErr, stderr.String())
				}
				var admitted corev1.Pod
				if decodeErr := json.Unmarshal(output, &admitted); decodeErr != nil {
					consecutiveSuccesses = 0
					return decodeErr
				}
				if !utils.HasValidationContainer(&admitted) {
					consecutiveSuccesses = 0
					return fmt.Errorf("webhook recovery admission did not inject the validation container")
				}
				consecutiveSuccesses++
				if consecutiveSuccesses < 3 {
					return fmt.Errorf("waiting for three consecutive successful admission probes: got %d", consecutiveSuccesses)
				}
				return nil
			}, 90*time.Second, time.Second).Should(Succeed(),
				"the restored webhook must accept and mutate real admission requests")
		})

		if os.Getenv("E2E_COLLECT_COVERAGE") == "true" {
			By("preserving manager coverage before intentionally terminating every replica")
			_, snapshotErr := utils.Run(exec.Command("make", "snapshot-e2e-coverage"))
			Expect(snapshotErr).NotTo(HaveOccurred(), "coverage-enabled runs must retain pre-disruption counters")
		}

		_, err = utils.Run(exec.Command("kubectl", "scale", "deployment", deploymentName,
			"-n", operatorNamespace, "--replicas=0", "--request-timeout=15s"))
		Expect(err).NotTo(HaveOccurred())
		By("waiting for every manager Pod to finish terminating")
		Eventually(func() ([]corev1.Pod, error) {
			output, listErr := utils.Run(exec.Command("kubectl", "get", "pods",
				"-l", "control-plane=controller-manager", "-n", operatorNamespace,
				"--request-timeout=15s", "-o", "json"))
			if listErr != nil {
				return nil, listErr
			}
			var pods corev1.PodList
			if listErr = json.Unmarshal([]byte(output), &pods); listErr != nil {
				return nil, listErr
			}
			return pods.Items, nil
		}, 90*time.Second, 2*time.Second).Should(BeEmpty(),
			"terminating managers can still serve admission after their Ready endpoints disappear")
		Eventually(webhookEndpointAddresses, 60*time.Second, 2*time.Second).Should(BeEmpty())

		err = utils.KubectlApply(podTemplate, authFixturePodData(podName, modelName))
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(Or(ContainSubstring("failed calling webhook"),
			ContainSubstring("no endpoints available")))
	})

})

func webhookEndpointAddresses() ([]string, error) {
	output, err := utils.Run(exec.Command("kubectl", "get", "endpointslice",
		"-l", "kubernetes.io/service-name=model-validation-webhook",
		"-n", operatorNamespace, "--request-timeout=15s", "-o", "json"))
	if err != nil {
		return nil, err
	}
	var slices discoveryv1.EndpointSliceList
	if err := json.Unmarshal([]byte(output), &slices); err != nil {
		return nil, err
	}
	var addresses []string
	for _, slice := range slices.Items {
		for _, endpoint := range slice.Endpoints {
			if endpoint.Conditions.Ready != nil && *endpoint.Conditions.Ready {
				addresses = append(addresses, endpoint.Addresses...)
			}
		}
	}
	return addresses, nil
}
