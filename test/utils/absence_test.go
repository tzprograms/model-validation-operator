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
	"os"
	"path/filepath"
	"testing"

	utils "github.com/sigstore/model-validation-operator/test/utils"
)

func TestKubectlResourceAbsentDoesNotHideAPIErrors(t *testing.T) {
	stub := `#!/bin/sh
case $MVO_FAKE_GET in
missing) exit 0;;
found) echo pod/test;;
*) echo Forbidden >&2; exit 1;;
esac
`
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "kubectl"), []byte(stub), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	for _, test := range []struct {
		name   string
		absent bool
		err    bool
	}{
		{name: "missing", absent: true},
		{name: "found"},
		{name: "forbidden", err: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("MVO_FAKE_GET", test.name)
			absent, err := utils.KubectlResourceAbsent("pod", "test", "test-namespace")
			if absent != test.absent || (err != nil) != test.err {
				t.Fatalf("absent = %t, error = %v", absent, err)
			}
		})
	}
}
