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
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	coordinationv1 "k8s.io/api/coordination/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestHAPodReadiness(t *testing.T) {
	for _, tc := range []struct {
		name              string
		status            corev1.ConditionStatus
		terminating, want bool
	}{
		{name: "ready", status: corev1.ConditionTrue, want: true},
		{name: "running but unready", status: corev1.ConditionFalse},
		{name: "unknown", status: corev1.ConditionUnknown},
		{name: "missing condition"},
		{name: "terminating", status: corev1.ConditionTrue, terminating: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pod := corev1.Pod{}
			if tc.status != "" {
				pod.Status.Conditions = []corev1.PodCondition{{Type: corev1.PodReady, Status: tc.status}}
			}
			if tc.terminating {
				now := metav1.Now()
				pod.DeletionTimestamp = &now
			}
			if got := haPodReady(pod); got != tc.want {
				t.Fatalf("ready=%v, want %v", got, tc.want)
			}
		})
	}
}

// Exercise the real kubectl-backed selection, including the second lease read.
func TestHALeaderSelectionRejectsStaleEvidence(t *testing.T) {
	for _, name := range []string{
		"selected ready holder", "missing holder", "unready", "deleting holder", "changed holder", "changed lease UID",
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			holder := "manager-1_identity"
			lease := coordinationv1.Lease{ObjectMeta: metav1.ObjectMeta{UID: "lease-uid"},
				Spec: coordinationv1.LeaseSpec{HolderIdentity: &holder}}
			pod := corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "manager-1", UID: "pod-uid"},
				Status: corev1.PodStatus{Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}}}}
			latest := lease.DeepCopy()
			switch name {
			case "missing holder":
				lease.Spec.HolderIdentity = nil
			case "unready":
				pod.Status.Conditions[0].Status = corev1.ConditionFalse
			case "deleting holder":
				now := metav1.Now()
				pod.DeletionTimestamp = &now
			case "changed holder":
				next := "manager-2_identity"
				latest.Spec.HolderIdentity = &next
			case "changed lease UID":
				latest.UID = "different-lease"
			}
			deployment := appsv1.Deployment{Spec: appsv1.DeploymentSpec{Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"control-plane": "controller-manager"}}}}
			for file, value := range map[string]any{"lease": lease, "latest": latest,
				"deployment": deployment, "pods": corev1.PodList{Items: []corev1.Pod{pod}}} {
				data, err := json.Marshal(value)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, file), data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			script := `#!/bin/sh
case "$2" in
lease)
  if [ -f "$MVO_FAKE_DIR/read" ]; then cat "$MVO_FAKE_DIR/latest"
  else touch "$MVO_FAKE_DIR/read"; cat "$MVO_FAKE_DIR/lease"; fi ;;
deployment|pods) cat "$MVO_FAKE_DIR/$2" ;;
*) echo "unexpected mock command" >&2; exit 1 ;;
esac
`
			if err := os.WriteFile(filepath.Join(dir, "kubectl"), []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
			t.Setenv("MVO_FAKE_DIR", dir)
			gotHolder, gotPod, err := haCurrentReadyLeader("test-lease")
			if name != "selected ready holder" {
				if err == nil {
					t.Fatal("accepted stale or unready lease holder")
				}
				return
			}
			if err != nil || gotHolder != holder || gotPod.UID != pod.UID {
				t.Fatalf("holder=%s pod=%s error=%v", gotHolder, gotPod.UID, err)
			}
		})
	}
}
