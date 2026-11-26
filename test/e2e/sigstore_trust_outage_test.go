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
	"fmt"

	. "github.com/onsi/ginkgo/v2" //nolint:revive
	. "github.com/onsi/gomega"    //nolint:revive

	"github.com/sigstore/model-validation-operator/api/v1alpha1"
	utils "github.com/sigstore/model-validation-operator/test/utils"
)

var _ = Describe("Sigstore trust service availability", func() {
	It("reports Sigstore verification failure when trust services are unreachable", func() {
		const (
			modelName        = "sigstore-network-failure"
			podName          = "sigstore-network-failure-pod"
			modelPath        = "/fixtures/v1.1.0-sigstore"
			controlModelName = "sigstore-network-control"
			controlPodName   = "sigstore-network-control-pod"
		)
		DeferCleanup(func() {
			deleteE2EResource("pod", podName)
			deleteE2EResource("pod", controlPodName)
			deleteE2EResource("modelvalidation", modelName)
			deleteE2EResource("modelvalidation", controlModelName)
		})

		// Local trust proves that the signed fixture and validation agent work
		// under the same DNS fault without depending on external service uptime.
		applyAuthModelValidation(controlModelName, sigstoreAuthConfig, v1alpha1.Model{
			Path: modelPath, SignaturePath: modelPath + "/model.sig",
			IgnorePaths: []string{modelPath + "/ignore-me"},
		})
		podTemplateText := `apiVersion: v1
kind: Pod
metadata:
  name: %s
  namespace: %s
  labels:
    validation.ml.sigstore.dev/ml: %s
spec:
  restartPolicy: Never
  terminationGracePeriodSeconds: 1
  dnsPolicy: None
  dnsConfig:
    nameservers: ["127.0.0.1"]
    options:
    - {name: timeout, value: "1"}
    - {name: attempts, value: "1"}
  containers:
  - name: workload
    image: busybox:latest
    imagePullPolicy: IfNotPresent
    command: ["sh", "-c", "sleep 3600"]
    volumeMounts:
    - {name: fixtures, mountPath: /fixtures, readOnly: true}
  volumes:
  - name: fixtures
    hostPath: {path: /tmp/e2e-fixture-data, type: Directory}
`
		controlPod := []byte(fmt.Sprintf(podTemplateText, controlPodName, webhookTestNamespace, controlModelName))
		Expect(utils.KubectlApply(controlPod, nil)).To(Succeed())
		expectSuccessfulValidation(controlPodName)

		By("requiring a trust-root fetch and asserting an actual DNS/connectivity failure")
		config := sigstoreAuthConfig
		config.ClientTrustConfig = nil
		applyAuthModelValidation(modelName, config, v1alpha1.Model{
			Path: modelPath, SignaturePath: modelPath + "/model.sig",
			IgnorePaths: []string{modelPath + "/ignore-me"},
		})
		pod := []byte(fmt.Sprintf(podTemplateText, podName, webhookTestNamespace, modelName))
		Expect(utils.KubectlApply(pod, nil)).To(Succeed())

		expectFailedValidation(podName, "failed to fetch production trust root", "lookup", "127.0.0.1:53")
	})
})
