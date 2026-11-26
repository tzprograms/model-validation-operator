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
	"strings"
	"testing"
)

const testBaseCatalog = "base:v1"

func TestUpgradeConfig(t *testing.T) {
	for _, tc := range []struct {
		name, key, value, wantError string
	}{
		{name: "generic upgrade"},
		{name: "missing base", key: baseCatalogEnv, value: " ", wantError: "are required"},
		{name: "missing target", key: targetCatalogEnv, value: "", wantError: "are required"},
		{name: "same catalog", key: targetCatalogEnv, value: testBaseCatalog, wantError: "must differ"},
		{name: "incomplete CSV", key: baseCSVEnv, value: "incomplete.v1", wantError: "both be set"},
		{name: "incomplete image", key: targetImageEnv, value: "incomplete:v2", wantError: "both be set"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := map[string]string{baseCatalogEnv: testBaseCatalog, targetCatalogEnv: "target:v2"}
			if tc.key != "" {
				env[tc.key] = tc.value
			}
			config, err := readUpgradeConfig(func(key string) string { return env[key] })
			if tc.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantError) {
					t.Fatalf("want %q, got %v", tc.wantError, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if config.channel != "tech-preview" || config.packageName != "model-validation-operator" {
				t.Fatalf("wrong defaults: %+v", config)
			}
		})
	}
}

func TestUpgradeIdentityConfiguration(t *testing.T) {
	env := map[string]string{
		baseCatalogEnv: " base:v1 ", targetCatalogEnv: " target:v2 ",
		"OLM_CHANNEL": "stable", "OLM_PACKAGE": "custom-package",
		baseCSVEnv: "mvo.v1", targetCSVEnv: "mvo.v2",
		baseImageEnv: "mvo:v1", targetImageEnv: "mvo:v2",
	}
	config, err := readUpgradeConfig(func(key string) string { return env[key] })
	if err != nil {
		t.Fatal(err)
	}
	if config.baseCatalog != testBaseCatalog || config.targetCatalog != "target:v2" ||
		config.channel != "stable" || config.packageName != "custom-package" ||
		config.expectedBaseCSV != "mvo.v1" || config.expectedTargetCSV != "mvo.v2" ||
		config.expectedBaseImage != "mvo:v1" || config.expectedTargetImage != "mvo:v2" {
		t.Fatalf("lost configuration: %+v", config)
	}
	for _, pair := range []struct{ base, target string }{
		{baseCSVEnv, targetCSVEnv},
		{baseImageEnv, targetImageEnv},
	} {
		original := env[pair.target]
		env[pair.target] = env[pair.base]
		if _, err := readUpgradeConfig(func(key string) string { return env[key] }); err == nil {
			t.Fatalf("accepted same identity: %s", pair.target)
		}
		env[pair.target] = original
	}
}
