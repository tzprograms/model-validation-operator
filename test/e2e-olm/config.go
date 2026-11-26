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
	"fmt"
	"strings"
)

const (
	baseCatalogEnv   = "TEST_BASE_CATALOG"
	targetCatalogEnv = "TEST_TARGET_CATALOG"
	baseCSVEnv       = "TEST_EXPECTED_BASE_CSV"
	targetCSVEnv     = "TEST_EXPECTED_TARGET_CSV"
	baseImageEnv     = "TEST_EXPECTED_BASE_IMAGE"
	targetImageEnv   = "TEST_EXPECTED_TARGET_IMAGE"
)

type upgradeConfig struct {
	baseCatalog, targetCatalog             string
	channel, packageName                   string
	expectedBaseCSV, expectedTargetCSV     string
	expectedBaseImage, expectedTargetImage string
}

// Read configuration before contacting the cluster: incomplete inputs should
// fail clearly rather than spend several minutes waiting for an impossible upgrade.
func readUpgradeConfig(getenv func(string) string) (upgradeConfig, error) {
	value := func(key, fallback string) string {
		if v := strings.TrimSpace(getenv(key)); v != "" {
			return v
		}
		return fallback
	}
	config := upgradeConfig{
		baseCatalog: value(baseCatalogEnv, ""), targetCatalog: value(targetCatalogEnv, ""),
		channel: value("OLM_CHANNEL", "tech-preview"), packageName: value("OLM_PACKAGE", "model-validation-operator"),
		expectedBaseCSV: value(baseCSVEnv, ""), expectedTargetCSV: value(targetCSVEnv, ""),
		expectedBaseImage:   value(baseImageEnv, ""),
		expectedTargetImage: value(targetImageEnv, ""),
	}
	if config.baseCatalog == "" || config.targetCatalog == "" {
		return config, fmt.Errorf("TEST_BASE_CATALOG and TEST_TARGET_CATALOG are required")
	}
	if config.baseCatalog == config.targetCatalog {
		return config, fmt.Errorf("base and target catalog references must differ")
	}
	for _, pair := range []struct{ name, base, target string }{
		{"CSV", config.expectedBaseCSV, config.expectedTargetCSV},
		{"operator image", config.expectedBaseImage, config.expectedTargetImage},
	} {
		if (pair.base == "") != (pair.target == "") {
			return config, fmt.Errorf("expected base and target %s must both be set", pair.name)
		}
		if pair.base != "" && pair.base == pair.target {
			return config, fmt.Errorf("expected base and target %s must differ", pair.name)
		}
	}
	return config, nil
}
