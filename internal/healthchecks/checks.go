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

// Package healthchecks registers operator liveness and webhook readiness checks.
package healthchecks

import (
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/healthz"
)

type healthCheckManager interface {
	AddHealthzCheck(string, healthz.Checker) error
	AddReadyzCheck(string, healthz.Checker) error
}

type webhookHealth interface {
	StartedChecker() healthz.Checker
}

// Setup registers liveness and readiness checks.
// Liveness stays independent of webhook startup so a slow certificate mount does
// not cause restarts. Readiness must wait until the webhook can accept requests.
func Setup(mgr healthCheckManager, server webhookHealth) error {
	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		return fmt.Errorf("register liveness: %w", err)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		return fmt.Errorf("register readiness: %w", err)
	}
	if server != nil {
		if err := mgr.AddReadyzCheck("webhook", server.StartedChecker()); err != nil {
			return fmt.Errorf("register webhook readiness: %w", err)
		}
	}
	return nil
}
