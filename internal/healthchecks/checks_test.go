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
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

type testHealthManager struct {
	live, ready map[string]healthz.Checker
	fail        string
}

func (m *testHealthManager) AddHealthzCheck(name string, check healthz.Checker) error {
	if m.fail == name {
		return errors.New("registration failed")
	}
	m.live[name] = check
	return nil
}

func (m *testHealthManager) AddReadyzCheck(name string, check healthz.Checker) error {
	if m.fail == name {
		return errors.New("registration failed")
	}
	m.ready[name] = check
	return nil
}

func probe(checks map[string]healthz.Checker) int {
	response := httptest.NewRecorder()
	(&healthz.Handler{Checks: checks}).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))
	return response.Code
}

func TestWebhookReadinessWaitsForServer(t *testing.T) {
	mgr := &testHealthManager{live: map[string]healthz.Checker{}, ready: map[string]healthz.Checker{}}
	if err := Setup(mgr, webhook.NewServer(webhook.Options{})); err != nil {
		t.Fatal(err)
	}
	if got := probe(mgr.ready); got != http.StatusInternalServerError {
		t.Fatalf("unstarted webhook readiness = %d, want 500", got)
	}
	if got := probe(mgr.live); got != http.StatusOK {
		t.Fatalf("liveness = %d, want 200", got)
	}
}

func TestDisabledWebhookIsReady(t *testing.T) {
	mgr := &testHealthManager{live: map[string]healthz.Checker{}, ready: map[string]healthz.Checker{}}
	if err := Setup(mgr, nil); err != nil {
		t.Fatal(err)
	}
	if got := probe(mgr.ready); got != http.StatusOK {
		t.Fatalf("disabled webhook readiness = %d, want 200", got)
	}
}

func TestHealthRegistrationErrors(t *testing.T) {
	for _, name := range []string{"healthz", "readyz", "webhook"} {
		t.Run(name, func(t *testing.T) {
			mgr := &testHealthManager{live: map[string]healthz.Checker{}, ready: map[string]healthz.Checker{}, fail: name}
			if err := Setup(mgr, webhook.NewServer(webhook.Options{})); err == nil {
				t.Fatal("expected registration error")
			}
		})
	}
}
