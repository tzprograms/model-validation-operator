//go:build ha

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
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	. "github.com/onsi/gomega" //nolint:revive
	coordinationv1 "k8s.io/api/coordination/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/sigstore/model-validation-operator/api/v1alpha1"
	"github.com/sigstore/model-validation-operator/internal/constants"
	utils "github.com/sigstore/model-validation-operator/test/utils"
)

// A completed Deployment rollout can leave the previous Pod's lease identity
// behind until its lease expires. Require a live, Ready manager and re-read the
// lease after looking it up before selecting a failover victim.
func haCurrentReadyLeader(leaseName string) (string, corev1.Pod, error) {
	var lease coordinationv1.Lease
	if err := utils.KubectlGetJSON("lease", leaseName, operatorNamespace, &lease); err != nil {
		return "", corev1.Pod{}, err
	}
	if lease.Spec.HolderIdentity == nil || *lease.Spec.HolderIdentity == "" {
		return "", corev1.Pod{}, fmt.Errorf("leader lease has no holder")
	}
	holder := *lease.Spec.HolderIdentity
	pods, err := haManagerPods()
	if err != nil {
		return "", corev1.Pod{}, err
	}
	for _, pod := range pods {
		if pod.Name != strings.SplitN(holder, "_", 2)[0] || !haPodReady(pod) {
			continue
		}
		var latest coordinationv1.Lease
		if err := utils.KubectlGetJSON("lease", leaseName, operatorNamespace, &latest); err != nil {
			return "", corev1.Pod{}, err
		}
		if latest.UID != lease.UID || latest.Spec.HolderIdentity == nil || *latest.Spec.HolderIdentity != holder {
			return "", corev1.Pod{}, fmt.Errorf("leader changed while selecting the failover victim")
		}
		return holder, pod, nil
	}
	return "", corev1.Pod{}, fmt.Errorf("lease holder %s is not a live Ready manager", holder)
}

func verifyHALeaderFailover() {
	const leaseName = "2b2639c9.sigstore.dev"
	var oldHolder string
	var victim corev1.Pod
	Eventually(func() error {
		var getErr error
		oldHolder, victim, getErr = haCurrentReadyLeader(leaseName)
		return getErr
	}, 90*time.Second, 2*time.Second).Should(Succeed(), "wait for leadership to settle after prior disruptions")

	// Raw DELETE accepts Kubernetes DeleteOptions, including a UID precondition.
	// Never mistake a stale name / NotFound for successfully killing a leader.
	deleteOptions := metav1.DeleteOptions{
		TypeMeta:      metav1.TypeMeta{APIVersion: "v1", Kind: "DeleteOptions"},
		Preconditions: &metav1.Preconditions{UID: &victim.UID},
	}
	body, err := json.Marshal(deleteOptions)
	Expect(err).NotTo(HaveOccurred())
	path := fmt.Sprintf("/api/v1/namespaces/%s/pods/%s", operatorNamespace, victim.Name)
	deleteCmd := exec.Command("kubectl", "delete", "--raw", path, "--request-timeout=15s", "-f", "-")
	deleteCmd.Stdin = bytes.NewReader(body)
	_, err = utils.Run(deleteCmd)
	Expect(err).NotTo(HaveOccurred(), "delete the selected live leader, not a stale lease holder")
	Eventually(func(g Gomega) {
		newHolder, leader, getErr := haCurrentReadyLeader(leaseName)
		g.Expect(getErr).NotTo(HaveOccurred())
		g.Expect(newHolder).NotTo(Equal(oldHolder))
		g.Expect(leader.UID).NotTo(Equal(victim.UID))
	}, 90*time.Second, 2*time.Second).Should(Succeed())

	const podName = "ha-leader-failover"
	Expect(utils.KubectlApply(podTemplate,
		utils.DefaultPodData(podName, webhookTestNamespace, haModelName, "ha"))).To(Succeed())
	var pod corev1.Pod
	Expect(utils.KubectlGetJSON("pod", podName, webhookTestNamespace, &pod)).To(Succeed())
	Expect(pod.Finalizers).To(ContainElement(constants.ModelValidationFinalizer))
	Eventually(func(g Gomega) {
		var mv v1alpha1.ModelValidation
		g.Expect(utils.KubectlGetJSON("modelvalidation", haModelName, webhookTestNamespace, &mv)).To(Succeed())
		g.Expect(mv.Status.InjectedPodCount).To(Equal(int32(1)))
		g.Expect(mv.Status.InjectedPods).To(ContainElement(And(
			HaveField("Name", podName), HaveField("UID", string(pod.UID)))))
	}, time.Minute, 2*time.Second).Should(Succeed())
	_, err = utils.Run(exec.Command("kubectl", "delete", "pod", podName, "-n", webhookTestNamespace,
		"--wait=false", "--request-timeout=15s"))
	Expect(err).NotTo(HaveOccurred())
	Eventually(func(g Gomega) {
		absent, getErr := utils.KubectlResourceAbsent("pod", podName, webhookTestNamespace)
		g.Expect(getErr).NotTo(HaveOccurred())
		g.Expect(absent).To(BeTrue(), "the new leader must remove the tracking finalizer")
		var mv v1alpha1.ModelValidation
		g.Expect(utils.KubectlGetJSON("modelvalidation", haModelName, webhookTestNamespace, &mv)).To(Succeed())
		g.Expect(mv.Status.InjectedPodCount).To(BeZero())
		g.Expect(mv.Status.InjectedPods).To(BeEmpty())
	}, time.Minute, 2*time.Second).Should(Succeed())
	waitForHAReplicas()
}
