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

// Package main provides the entry point for the model validation operator
package main

import (
	"context"
	"flag"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestPreStopCommand(t *testing.T) {
	if delay := os.Getenv("MVO_TEST_PRE_STOP"); delay != "" {
		flag.CommandLine = flag.NewFlagSet("manager", flag.ExitOnError)
		os.Args = []string{"manager", "--pre-stop-delay=" + delay}
		main()
		return
	}
	for _, tc := range []struct {
		name, delay string
		wantCode    int
	}{
		{name: "without Kubernetes or certificates", delay: "10ms"},
		{name: "reject negative delay", delay: "-1s", wantCode: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestPreStopCommand$")
			cmd.Env = append(os.Environ(), "MVO_TEST_PRE_STOP="+tc.delay, "KUBECONFIG=/nonexistent")
			output, err := cmd.CombinedOutput()
			if ctx.Err() != nil {
				t.Fatalf("pre-stop tried to start the manager: %v", ctx.Err())
			}
			code := 0
			if err != nil {
				if exit, ok := err.(*exec.ExitError); ok {
					code = exit.ExitCode()
				} else {
					t.Fatal(err)
				}
			}
			if code != tc.wantCode {
				t.Fatalf("exit %d, want %d: %s", code, tc.wantCode, output)
			}
			if tc.wantCode != 0 && !strings.Contains(string(output), "pre-stop-delay must not be negative") {
				t.Fatalf("missing invalid-delay diagnostic: %s", output)
			}
		})
	}
}
