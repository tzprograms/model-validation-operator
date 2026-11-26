//go:build upgrade

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

package e2eolm

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"reflect"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/sigstore/model-validation-operator/api/v1alpha1"
	utils "github.com/sigstore/model-validation-operator/test/utils"
)

const controllerDeployment = "model-validation-controller-manager"

func controllerRolledOut(expectedImage string) error {
	var deployment appsv1.Deployment
	if err := utils.KubectlGetJSON("deployment", controllerDeployment, operatorNamespace, &deployment); err != nil {
		return err
	}
	selector, err := metav1.LabelSelectorAsSelector(deployment.Spec.Selector)
	if err != nil {
		return err
	}
	output, err := utils.Run(exec.Command("kubectl", "get", "pods", "-n", operatorNamespace,
		"-l", selector.String(), "--request-timeout=15s", "-o", "json"))
	if err != nil {
		return err
	}
	var pods corev1.PodList
	if err := json.Unmarshal([]byte(output), &pods); err != nil {
		return err
	}
	return validateControllerRollout(deployment, pods.Items, expectedImage)
}

// Available can remain true while an old Pod serves a new Deployment revision.
// Require the observed revision and every replacement Pod to be fully ready.
func validateControllerRollout(deployment appsv1.Deployment, pods []corev1.Pod, expectedImage string) error {
	if deployment.Spec.Replicas == nil || *deployment.Spec.Replicas < 1 {
		return fmt.Errorf("no desired controller replicas")
	}
	want, status := *deployment.Spec.Replicas, deployment.Status
	if status.ObservedGeneration < deployment.Generation || status.Replicas != want ||
		status.UpdatedReplicas != want || status.ReadyReplicas != want || status.AvailableReplicas != want ||
		status.UnavailableReplicas != 0 {
		return fmt.Errorf("controller rollout is incomplete: %+v", status)
	}
	image := managerImage(deployment.Spec.Template.Spec)
	if image == "" || (expectedImage != "" && image != expectedImage) {
		return fmt.Errorf("controller manager image %q, expected %q", image, expectedImage)
	}
	if len(pods) != int(want) {
		return fmt.Errorf("expected %d controller Pods, found %d", want, len(pods))
	}
	for _, pod := range pods {
		if pod.UID == "" || pod.DeletionTimestamp != nil || managerImage(pod.Spec) != image {
			return fmt.Errorf("controller Pod %s is terminating, unidentified or running the old image", pod.Name)
		}
		ready := false
		for _, condition := range pod.Status.Conditions {
			if condition.Type == corev1.PodReady {
				ready = condition.Status == corev1.ConditionTrue
			}
		}
		if !ready {
			return fmt.Errorf("controller Pod %s is not Ready", pod.Name)
		}
	}
	return nil
}

func managerImage(spec corev1.PodSpec) string {
	for _, container := range spec.Containers {
		if container.Name == "manager" {
			return container.Image
		}
	}
	return ""
}

func validatePreservedModel(before, after v1alpha1.ModelValidation) error {
	if before.UID == "" || before.UID != after.UID || before.Name != after.Name || before.Namespace != after.Namespace {
		return fmt.Errorf("ModelValidation identity changed across the upgrade")
	}
	if after.DeletionTimestamp != nil || !reflect.DeepEqual(before.Spec, after.Spec) {
		return fmt.Errorf("ModelValidation spec changed or the resource is being deleted")
	}
	return nil
}
