package bash_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	bash "github.com/adrianliechti/go-bash"
)

// These are reviewed, fixed excerpts of GNU Bash's own tests. Only these
// checked-in scripts are sent to the optional host oracle, in temporary dirs.
// The pinned outputs remain authoritative when Bash 5.3 is not installed.
func TestBash53Upstream(t *testing.T) {
	const dir = "testdata/bash-5.3"
	var cases []struct {
		Name         string   `json:"name"`
		Source       string   `json:"source"`
		Lines        string   `json:"lines"`
		ExitCode     int      `json:"exit_code"`
		Stderr       string   `json:"stderr"`
		KnownFailure string   `json:"known_failure,omitempty"`
		SupportFiles []string `json:"support_files,omitempty"`
	}
	data, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) == 0 {
		t.Fatal("no upstream fixtures")
	}
	oracle := bash53Reference(t)
	strict := os.Getenv("BASH_TEST_STRICT") == "1"
	b := newShell(t, bash.Options{Env: map[string]string{
		"HOME": "/work", "LC_ALL": "C", "TZ": "UTC", "TMPDIR": ".",
	}})
	seen := make(map[string]bool)
	for _, tc := range cases {
		if tc.Name == "" || strings.Trim(tc.Name, "abcdefghijklmnopqrstuvwxyz0123456789-") != "" || seen[tc.Name] {
			t.Fatalf("invalid or duplicate fixture name %q", tc.Name)
		}
		seen[tc.Name] = true
		t.Run(tc.Name, func(t *testing.T) {
			t.Logf("GNU Bash 5.3 tests/%s, %s", tc.Source, tc.Lines)
			// Some upstream drivers source or invoke fixed helper scripts. Copy
			// only manifest-listed, reviewed files into each isolated directory.
			files := make(map[string][]byte)
			var setup strings.Builder
			setup.WriteString("THIS_SH=bash\n")
			for _, name := range tc.SupportFiles {
				if !fs.ValidPath(name) || strings.Contains(name, "/") {
					t.Fatalf("invalid support file %q", name)
				}
				data, err := os.ReadFile(filepath.Join(dir, "support", name))
				if err != nil {
					t.Fatal(err)
				}
				files[name] = data
				setup.WriteString("printf '%s' '" + strings.ReplaceAll(string(data), "'", "'\\''") + "' > '" + strings.ReplaceAll(name, "'", "'\\''") + "'\n")
			}
			script, err := os.ReadFile(filepath.Join(dir, tc.Name+".bash"))
			if err != nil {
				t.Fatal(err)
			}
			want, err := os.ReadFile(filepath.Join(dir, tc.Name+".stdout"))
			if err != nil {
				t.Fatal(err)
			}
			if oracle != "" {
				ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
				defer cancel()
				cmd := exec.CommandContext(ctx, oracle, "--noprofile", "--norc", "-c", "(\n"+string(script)+"\n)")
				cmd.Dir = t.TempDir()
				cmd.Env = bash53ReferenceEnv()
				cmd.Env = append(cmd.Env, "THIS_SH="+oracle)
				for name, data := range files {
					if err := os.WriteFile(filepath.Join(cmd.Dir, name), data, 0600); err != nil {
						t.Fatal(err)
					}
				}
				var stdout, stderr bytes.Buffer
				cmd.Stdout, cmd.Stderr = &stdout, &stderr
				code := 0
				if err := cmd.Run(); err != nil {
					var exit *exec.ExitError
					if !errors.As(err, &exit) {
						t.Fatal(err)
					}
					code = exit.ExitCode()
				}
				if stdout.String() != string(want) || stderr.String() != tc.Stderr || code != tc.ExitCode {
					t.Fatalf("Bash 5.3 disagrees with golden: stdout %q, stderr %q, exit %d; want %q, %q, %d", stdout.String(), stderr.String(), code, want, tc.Stderr, tc.ExitCode)
				}
			}
			// Share compiled WASM modules, but give each fixture its own directory
			// and subshell so files, variables, functions, and options stay isolated.
			r, err := b.Exec(t.Context(), "(\nmkdir "+tc.Name+" && cd "+tc.Name+" || exit 1\n"+setup.String()+string(script)+"\n)")
			if err != nil || r.Stdout != string(want) || r.Stderr != tc.Stderr || r.ExitCode != tc.ExitCode || r.Exited {
				if tc.KnownFailure != "" && !strict {
					t.Logf("stdout %q, stderr %q, exit %d, error %v; want %q, %q, %d", r.Stdout, r.Stderr, r.ExitCode, err, want, tc.Stderr, tc.ExitCode)
					t.Skipf("known Bash 5.3 gap: %s Set BASH_TEST_STRICT=1 to fail on this gap.", tc.KnownFailure)
				}
				t.Fatalf("go-bash: stdout %q, stderr %q, exit %d, exited %v, error %v; want %q, %q, %d", r.Stdout, r.Stderr, r.ExitCode, r.Exited, err, want, tc.Stderr, tc.ExitCode)
			}
			if tc.KnownFailure != "" {
				t.Log("Known gap now passes; remove known_failure from the manifest once the fix is confirmed")
			}
		})
	}
}

func bash53ReferenceEnv() []string {
	return []string{"PATH=/usr/bin:/bin", "HOME=/work", "LC_ALL=C", "TZ=UTC", "TMPDIR=."}
}

func bash53Reference(t *testing.T) string {
	t.Helper()
	oracle := os.Getenv("BASH_TEST_BINARY")
	explicit := oracle != ""
	if !explicit {
		oracle, _ = exec.LookPath("bash")
		if oracle == "" {
			t.Log("Bash unavailable; checking pinned Bash 5.3 outputs")
			return ""
		}
	}
	// An explicit relative path must still work after cmd.Dir changes.
	oracle, err := filepath.Abs(oracle)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, oracle, "--noprofile", "--norc", "-c", `printf '%s' "$BASH_VERSION"`)
	cmd.Dir = t.TempDir()
	cmd.Env = bash53ReferenceEnv()
	version, err := cmd.Output()
	if err != nil {
		t.Fatalf("Bash reference version: %v", err)
	}
	if !strings.HasPrefix(string(version), "5.3.") {
		if explicit {
			t.Fatalf("BASH_TEST_BINARY must select Bash 5.3 for upstream fixtures; got %q", version)
		}
		t.Logf("Host Bash %s is not 5.3; checking pinned Bash 5.3 outputs", version)
		return ""
	}
	t.Logf("Also checking goldens against GNU Bash %s", version)
	return oracle
}
