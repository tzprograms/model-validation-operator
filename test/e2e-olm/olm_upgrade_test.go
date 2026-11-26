//go:build upgrade

// Copyright 2025 The Sigstore Authors.
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

package e2eolm

import (
	"fmt"
	"os/exec"
	"time"

	. "github.com/onsi/ginkgo/v2" //nolint:revive
	. "github.com/onsi/gomega"    //nolint:revive
	corev1 "k8s.io/api/core/v1"

	"github.com/sigstore/model-validation-operator/api/v1alpha1"
	"github.com/sigstore/model-validation-operator/internal/constants"
	utils "github.com/sigstore/model-validation-operator/test/utils"
)

func cleanupResource(resource, name, namespace string) {
	args := []string{"delete", resource}
	if name != "" {
		args = append(args, name)
	} else {
		args = append(args, "--all")
	}
	if namespace != "" {
		args = append(args, "-n", namespace)
	}
	args = append(args, "--timeout=60s", "--ignore-not-found=true")
	_, _ = utils.Run(exec.Command("kubectl", args...))
}

var _ = Describe("OLM Upgrade", Ordered, func() {
	var baseCSV string
	var originalModel v1alpha1.ModelValidation

	const (
		catalogName       = "model-validation-test-catalog"
		operatorGroupName = "model-validation-og"
		subscriptionName  = "model-validation-operator"
		testModelName     = "olm-upgrade-test-model"
	)

	BeforeAll(func() {

		_, _ = fmt.Fprintf(GinkgoWriter, "OLM Upgrade Test Configuration:\n")
		_, _ = fmt.Fprintf(GinkgoWriter, "  Base catalog: %s\n", config.baseCatalog)
		_, _ = fmt.Fprintf(GinkgoWriter, "  Target catalog: %s\n", config.targetCatalog)
		_, _ = fmt.Fprintf(GinkgoWriter, "  Channel: %s\n", config.channel)
		_, _ = fmt.Fprintf(GinkgoWriter, "  Package: %s\n", config.packageName)

		DeferCleanup(func() {
			By("cleaning up OLM test resources")
			cleanupResource("pods", "", webhookTestNamespace)
			cleanupResource("modelvalidations", "", webhookTestNamespace)
			cleanupResource("subscription", subscriptionName, operatorNamespace)
			cleanupResource("csv", "", operatorNamespace)
			cleanupResource("operatorgroup", operatorGroupName, operatorNamespace)
			cleanupResource("catalogsource", catalogName, olmNamespace)
		})
	})

	It("should install the initial version via OLM", func() {
		By("creating a CatalogSource with the base catalog image")
		err := utils.KubectlApply(catalogSourceTemplate, utils.CatalogSourceTemplateData{
			Name:      catalogName,
			Namespace: olmNamespace,
			Image:     config.baseCatalog,
		})
		Expect(err).NotTo(HaveOccurred(), "Failed to create CatalogSource")

		By("waiting for CatalogSource to be ready")
		Eventually(func(g Gomega) {
			status, err := utils.KubectlGet("catalogsource", catalogName, olmNamespace,
				"jsonpath={.status.connectionState.lastObservedState}")
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(status).To(Equal("READY"))
		}, 5*time.Minute, 5*time.Second).Should(Succeed(), "CatalogSource should become READY")

		By("creating an OperatorGroup for AllNamespaces mode")
		err = utils.KubectlApply(operatorGroupTemplate, utils.OperatorGroupTemplateData{
			Name:      operatorGroupName,
			Namespace: operatorNamespace,
		})
		Expect(err).NotTo(HaveOccurred(), "Failed to create OperatorGroup")

		By("creating a Subscription to trigger installation")
		err = utils.KubectlApply(subscriptionTemplate, utils.SubscriptionTemplateData{
			Name:                   subscriptionName,
			Namespace:              operatorNamespace,
			Channel:                config.channel,
			PackageName:            config.packageName,
			CatalogSourceName:      catalogName,
			CatalogSourceNamespace: olmNamespace,
		})
		Expect(err).NotTo(HaveOccurred(), "Failed to create Subscription")

		By("waiting for subscription to report an installed CSV")
		Eventually(func(g Gomega) {
			csv, err := utils.KubectlGet("subscription", subscriptionName, operatorNamespace,
				"jsonpath={.status.installedCSV}")
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(csv).NotTo(BeEmpty())
			if config.expectedBaseCSV != "" {
				g.Expect(csv).To(Equal(config.expectedBaseCSV))
			}
			baseCSV = csv
		}, 5*time.Minute, 5*time.Second).Should(Succeed(),
			"Subscription should report an installed CSV")

		_, _ = fmt.Fprintf(GinkgoWriter, "  Installed base CSV: %s\n", baseCSV)

		By(fmt.Sprintf("waiting for base CSV %s to reach Succeeded phase", baseCSV))
		Eventually(func(g Gomega) {
			phase, err := utils.KubectlGet("csv", baseCSV, operatorNamespace,
				"jsonpath={.status.phase}")
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(phase).To(Equal("Succeeded"))
		}, 5*time.Minute, 5*time.Second).Should(Succeed(),
			"Base CSV should reach Succeeded phase")

		By("waiting for the installed controller revision and Pods to be ready")
		Eventually(func() error { return controllerRolledOut(config.expectedBaseImage) },
			3*time.Minute, 5*time.Second).Should(Succeed())
	})

	It("should have a functioning webhook after OLM installation", func() {
		By("deploying a ModelValidation CR")
		err := utils.KubectlApply(modelValidationTemplate, utils.CRTemplateData{
			ModelName: testModelName,
			Namespace: webhookTestNamespace,
		})
		Expect(err).NotTo(HaveOccurred(), "Failed to apply ModelValidation CR")
		Expect(utils.KubectlGetJSON("modelvalidation", testModelName, webhookTestNamespace, &originalModel)).To(Succeed())
		Expect(originalModel.UID).NotTo(BeEmpty())

		By("deploying a test pod with model validation label")
		err = utils.KubectlApply(podTemplate, utils.DefaultPodData(
			"olm-test-pod-initial", webhookTestNamespace, testModelName, "olm-upgrade"))
		Expect(err).NotTo(HaveOccurred(), "Failed to deploy test pod")

		By("verifying the init container is injected by the webhook")
		Eventually(func(g Gomega) {
			var pod corev1.Pod
			err := utils.KubectlGetJSON("pod", "olm-test-pod-initial", webhookTestNamespace, &pod)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(pod.Spec.InitContainers).ToNot(BeEmpty(),
				"Pod should have init containers after webhook injection")
			g.Expect(pod.Spec.InitContainers[0].Name).To(
				Equal(constants.ModelValidationInitContainerName))
		}, 1*time.Minute, 5*time.Second).Should(Succeed(),
			"Init container should be injected")
	})

	It("should upgrade when the catalog is updated", func() {
		By("capturing the pre-upgrade controller image")
		var preUpgradeImage string
		Eventually(func(g Gomega) {
			img, err := utils.KubectlGet("deployment", "model-validation-controller-manager",
				operatorNamespace,
				"jsonpath={.spec.template.spec.containers[?(@.name==\"manager\")].image}")
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(img).NotTo(BeEmpty())
			if config.expectedBaseImage != "" {
				g.Expect(img).To(Equal(config.expectedBaseImage))
			}
			preUpgradeImage = img
		}, 1*time.Minute, 5*time.Second).Should(Succeed())
		_, _ = fmt.Fprintf(GinkgoWriter, "  Pre-upgrade image: %s\n", preUpgradeImage)

		By(fmt.Sprintf("patching CatalogSource to use target catalog: %s", config.targetCatalog))
		err := utils.KubectlPatch("catalogsource", catalogName, olmNamespace, "merge",
			fmt.Sprintf(`{"spec":{"image":"%s"}}`, config.targetCatalog))
		Expect(err).NotTo(HaveOccurred(), "Failed to patch CatalogSource")

		By("waiting for CatalogSource to reconnect with updated catalog")
		Eventually(func(g Gomega) {
			status, err := utils.KubectlGet("catalogsource", catalogName, olmNamespace,
				"jsonpath={.status.connectionState.lastObservedState}")
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(status).To(Equal("READY"))
		}, 3*time.Minute, 5*time.Second).Should(Succeed(),
			"CatalogSource should reconnect after update")

		By("waiting for subscription to report a new installed CSV")
		Eventually(func(g Gomega) {
			csv, err := utils.KubectlGet("subscription", subscriptionName, operatorNamespace,
				"jsonpath={.status.installedCSV}")
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(csv).NotTo(BeEmpty())
			g.Expect(csv).NotTo(Equal(baseCSV), "installed CSV should change after upgrade")
			if config.expectedTargetCSV != "" {
				g.Expect(csv).To(Equal(config.expectedTargetCSV))
			}
		}, 5*time.Minute, 10*time.Second).Should(Succeed(),
			"Subscription should report a new installed CSV after upgrade")

		var upgradeCSV string
		upgradeCSV, err = utils.KubectlGet("subscription", subscriptionName, operatorNamespace,
			"jsonpath={.status.installedCSV}")
		Expect(err).NotTo(HaveOccurred())
		_, _ = fmt.Fprintf(GinkgoWriter, "  Upgraded CSV: %s (was: %s)\n", upgradeCSV, baseCSV)

		By(fmt.Sprintf("waiting for upgrade CSV %s to reach Succeeded phase", upgradeCSV))
		Eventually(func(g Gomega) {
			phase, err := utils.KubectlGet("csv", upgradeCSV, operatorNamespace,
				"jsonpath={.status.phase}")
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(phase).To(Equal("Succeeded"))
		}, 5*time.Minute, 10*time.Second).Should(Succeed(),
			"Upgrade CSV should reach Succeeded phase")

		var targetImage string
		By("verifying the controller image changed after upgrade")
		Eventually(func(g Gomega) {
			img, err := utils.KubectlGet("deployment", "model-validation-controller-manager",
				operatorNamespace,
				"jsonpath={.spec.template.spec.containers[?(@.name==\"manager\")].image}")
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(img).NotTo(BeEmpty())
			if config.expectedTargetImage != "" {
				g.Expect(img).To(Equal(config.expectedTargetImage))
			}
			g.Expect(img).NotTo(Equal(preUpgradeImage),
				"Controller image should change after upgrade")
			targetImage = img
		}, 2*time.Minute, 5*time.Second).Should(Succeed(),
			"Controller deployment image should be updated")

		By("waiting for the target controller revision and Pods to be ready")
		Eventually(func() error { return controllerRolledOut(targetImage) },
			3*time.Minute, 5*time.Second).Should(Succeed())
	})

	It("should maintain functionality after upgrade", func() {
		By("verifying the same ModelValidation and its complete spec survived the upgrade")
		var preserved v1alpha1.ModelValidation
		Expect(utils.KubectlGetJSON("modelvalidation", testModelName, webhookTestNamespace, &preserved)).To(Succeed())
		Expect(validatePreservedModel(originalModel, preserved)).To(Succeed())

		By("deploying a new test pod to verify webhook still works after upgrade")
		Eventually(func() error {
			return utils.KubectlApply(podTemplate, utils.DefaultPodData(
				"olm-test-pod-upgrade", webhookTestNamespace, testModelName, "olm-upgrade"))
		}, 2*time.Minute, 5*time.Second).Should(Succeed(),
			"Post-upgrade test pod should be accepted by webhook")

		By("verifying the init container is injected on the new pod")
		Eventually(func(g Gomega) {
			var pod corev1.Pod
			err := utils.KubectlGetJSON("pod", "olm-test-pod-upgrade", webhookTestNamespace, &pod)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(pod.Spec.InitContainers).ToNot(BeEmpty(),
				"Post-upgrade pod should have init containers")
			g.Expect(pod.Spec.InitContainers[0].Name).To(
				Equal(constants.ModelValidationInitContainerName))
		}, 1*time.Minute, 5*time.Second).Should(Succeed(),
			"Init container should be injected after upgrade")
	})

	It("should allow CR deletion after upgrade", func() {
		By("deleting the ModelValidation CR")
		cleanupResource("modelvalidation", testModelName, webhookTestNamespace)

		By("verifying the ModelValidation CR is deleted")
		Eventually(func(g Gomega) {
			absent, err := utils.KubectlResourceAbsent("modelvalidation", testModelName, webhookTestNamespace)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(absent).To(BeTrue())
		}, time.Minute, 5*time.Second).Should(Succeed(), "ModelValidation CR should be deleted after upgrade")
	})

})
