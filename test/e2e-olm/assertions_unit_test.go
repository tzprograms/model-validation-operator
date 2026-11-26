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
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/sigstore/model-validation-operator/api/v1alpha1"
)

const targetManagerImage = "example.com/manager:v2"
const statusUpdateCase = "status update"

func TestControllerRolloutRejectsStaleOrUnreadyPods(t *testing.T) {
	for _, name := range []string{"complete", "stale generation", "old replicas", "not updated", "not ready",
		"not available", "unavailable", "missing desired count", "no manager", "wrong desired image",
		"no pods", "extra pod", "missing pod UID", "terminating pod", "old pod image", "Running but not Ready"} {
		t.Run(name, func(t *testing.T) {
			replicas := int32(1)
			spec := corev1.PodSpec{Containers: []corev1.Container{{Name: "manager", Image: targetManagerImage}}}
			deployment := appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Generation: 2},
				Spec: appsv1.DeploymentSpec{Replicas: &replicas, Template: corev1.PodTemplateSpec{Spec: spec}},
				Status: appsv1.DeploymentStatus{ObservedGeneration: 2, Replicas: 1, UpdatedReplicas: 1,
					ReadyReplicas: 1, AvailableReplicas: 1}}
			pods := []corev1.Pod{{ObjectMeta: metav1.ObjectMeta{Name: "controller", UID: "pod-uid"}, Spec: *spec.DeepCopy(),
				Status: corev1.PodStatus{Phase: corev1.PodRunning, Conditions: []corev1.PodCondition{
					{Type: corev1.PodReady, Status: corev1.ConditionTrue}}}}}
			switch name {
			case "stale generation":
				deployment.Status.ObservedGeneration--
			case "old replicas":
				deployment.Status.Replicas++
			case "not updated":
				deployment.Status.UpdatedReplicas = 0
			case "not ready":
				deployment.Status.ReadyReplicas = 0
			case "not available":
				deployment.Status.AvailableReplicas = 0
			case "unavailable":
				deployment.Status.UnavailableReplicas = 1
			case "missing desired count":
				deployment.Spec.Replicas = nil
			case "no manager":
				deployment.Spec.Template.Spec.Containers = nil
			case "wrong desired image":
				deployment.Spec.Template.Spec.Containers[0].Image = "unrelated:image"
			case "no pods":
				pods = nil
			case "extra pod":
				pods = []corev1.Pod{pods[0], *pods[0].DeepCopy()}
			case "missing pod UID":
				pods[0].UID = ""
			case "terminating pod":
				now := metav1.Now()
				pods[0].DeletionTimestamp = &now
			case "old pod image":
				pods[0].Spec.Containers[0].Image = "old:image"
			case "Running but not Ready":
				pods[0].Status.Conditions[0].Status = corev1.ConditionFalse
			}
			err := validateControllerRollout(deployment, pods, targetManagerImage)
			if (err == nil) != (name == "complete") {
				t.Fatalf("unexpected rollout result: %v", err)
			}
		})
	}
}

func TestUpgradePreservesModelIdentityAndSpec(t *testing.T) {
	for _, name := range []string{
		"unchanged", "recreated", "missing original UID", "different name", "different namespace",
		"different spec", "deleted", statusUpdateCase} {
		t.Run(name, func(t *testing.T) {
			before := v1alpha1.ModelValidation{ObjectMeta: metav1.ObjectMeta{Name: "model", Namespace: "test", UID: "cr-uid"},
				Spec: v1alpha1.ModelValidationSpec{Model: v1alpha1.Model{Path: "/model"}}}
			after := before.DeepCopy()
			switch name {
			case "recreated":
				after.UID = "replacement-uid"
			case "missing original UID":
				before.UID = ""
			case "different name":
				after.Name = "replacement"
			case "different namespace":
				after.Namespace = "other"
			case "different spec":
				after.Spec.Model.Path = "/changed-model"
			case "deleted":
				now := metav1.Now()
				after.DeletionTimestamp = &now
			case statusUpdateCase:
				after.Status.InjectedPodCount = 1
			}
			err := validatePreservedModel(before, *after)
			wantSuccess := name == "unchanged" || name == statusUpdateCase
			if (err == nil) != wantSuccess {
				t.Fatalf("unexpected preservation result: %v", err)
			}
		})
	}
}
