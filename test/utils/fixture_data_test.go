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

package utils_test_test //nolint:revive

import (
	"reflect"
	"testing"

	corev1 "k8s.io/api/core/v1"

	utils "github.com/sigstore/model-validation-operator/test/utils"
)

func TestDefaultPodDataMountsAllVerificationFixturesReadOnly(t *testing.T) {
	data := utils.DefaultPodData("test-pod", "test-namespace", "test-model", "test-batch")
	if data.PodName != "test-pod" || data.Namespace != "test-namespace" ||
		data.ModelName != "test-model" || data.TestBatch != "test-batch" {
		t.Fatalf("pod identity fields were not preserved: %+v", data)
	}
	wantMounts := []utils.VolumeMount{
		{Name: "model-data", MountPath: "/data", ReadOnly: true},
		{Name: "keys-data", MountPath: "/keys", ReadOnly: true},
		{Name: "fixture-data", MountPath: "/fixtures", ReadOnly: true},
	}
	if !reflect.DeepEqual(data.VolumeMounts, wantMounts) {
		t.Errorf("unexpected mounts: %+v", data.VolumeMounts)
	}
	wantVolumes := []utils.Volume{
		{Name: "model-data", HostPath: &utils.HostPathVolume{
			Path: "/tmp/e2e-model-data", Type: string(corev1.HostPathDirectory),
		}},
		{Name: "keys-data", HostPath: &utils.HostPathVolume{
			Path: "/tmp/e2e-keys-data", Type: string(corev1.HostPathDirectory),
		}},
		{Name: "fixture-data", HostPath: &utils.HostPathVolume{
			Path: "/tmp/e2e-fixture-data", Type: string(corev1.HostPathDirectory),
		}},
	}
	if !reflect.DeepEqual(data.Volumes, wantVolumes) {
		t.Errorf("unexpected hostPath sources: %+v", data.Volumes)
	}
}
