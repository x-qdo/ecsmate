package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"cuelang.org/go/cue"
)

func TestStrictSchemaValidation(t *testing.T) {
	// Get project root for schema import path resolution
	projectRoot, err := findProjectRoot()
	if err != nil {
		t.Fatalf("failed to find project root: %v", err)
	}

	tests := []struct {
		name    string
		cueFile string
		wantErr bool
		errMsg  string
	}{
		{
			name: "manifest without schema import",
			cueFile: `package test
manifest: {
	name: "test"
}`,
			wantErr: true,
			errMsg:  "must import schema package",
		},
		{
			name: "valid manifest with schema",
			cueFile: `package test
import "github.com/x-qdo/ecsmate/pkg/cue:schema"
manifest: schema.#Manifest & {
	name: "test"
}`,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temp directory within project to access schema
			tmpDir := t.TempDir()

			// Write test CUE file
			testFile := filepath.Join(tmpDir, "test.cue")
			if err := os.WriteFile(testFile, []byte(tt.cueFile), 0644); err != nil {
				t.Fatal(err)
			}

			// Create a symlink to the project's cue.mod so schema imports work
			cueModLink := filepath.Join(tmpDir, "cue.mod")
			projectCueMod := filepath.Join(projectRoot, "cue.mod")
			if err := os.Symlink(projectCueMod, cueModLink); err != nil {
				t.Fatalf("failed to symlink cue.mod: %v", err)
			}

			// Also need access to pkg/cue for the schema
			pkgDir := filepath.Join(tmpDir, "pkg")
			if err := os.MkdirAll(pkgDir, 0755); err != nil {
				t.Fatal(err)
			}
			pkgCueLink := filepath.Join(pkgDir, "cue")
			projectPkgCue := filepath.Join(projectRoot, "pkg", "cue")
			if err := os.Symlink(projectPkgCue, pkgCueLink); err != nil {
				t.Fatalf("failed to symlink pkg/cue: %v", err)
			}

			// Test validation
			loader := NewCUELoader()
			_, err := loader.LoadManifest(tmpDir, nil, nil)

			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error containing %q, got nil", tt.errMsg)
				} else if !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("expected error containing %q, got %v", tt.errMsg, err)
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

func findProjectRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}

	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	return "", os.ErrNotExist
}

func TestApplySetValues_OverridesDefaultValues(t *testing.T) {
	loader := NewCUELoader()

	// Use CUE default syntax (string | *"value") which allows override
	base := loader.ctx.CompileString(`
		images: {
			tag: string | *"original"
			registry: string | *"ecr.aws"
		}
	`)
	if base.Err() != nil {
		t.Fatalf("failed to compile base: %v", base.Err())
	}

	result, err := loader.applySetValues(base, []string{"images.tag=overridden"})
	if err != nil {
		t.Fatalf("applySetValues failed: %v", err)
	}

	tag, err := ExtractString(result, "images.tag")
	if err != nil {
		t.Fatalf("failed to extract tag: %v", err)
	}
	if tag != "overridden" {
		t.Errorf("expected tag 'overridden', got '%s'", tag)
	}
}

func TestApplySetValues_HiddenFields(t *testing.T) {
	loader := NewCUELoader()

	// Use CUE default syntax for hidden fields
	base := loader.ctx.CompileString(`
		_values: {
			namespace: string | *"original"
		}
	`)
	if base.Err() != nil {
		t.Fatalf("failed to compile base: %v", base.Err())
	}

	result, err := loader.applySetValues(base, []string{"_values.namespace=overridden"})
	if err != nil {
		t.Fatalf("applySetValues failed: %v", err)
	}

	nsPath := cue.MakePath(cue.Hid("_values", "_"), cue.Str("namespace"))
	nsVal := result.LookupPath(nsPath)
	if nsVal.Err() != nil {
		t.Fatalf("failed to lookup _values.namespace: %v", nsVal.Err())
	}
	ns, err := nsVal.String()
	if err != nil {
		t.Fatalf("failed to get string value: %v", err)
	}
	if ns != "overridden" {
		t.Errorf("expected namespace 'overridden', got '%s'", ns)
	}
}

func TestApplySetValues_NumericValues(t *testing.T) {
	loader := NewCUELoader()

	// Use CUE default syntax for numeric and boolean values
	base := loader.ctx.CompileString(`
		config: {
			count: int | *1
			enabled: bool | *true
		}
	`)
	if base.Err() != nil {
		t.Fatalf("failed to compile base: %v", base.Err())
	}

	result, err := loader.applySetValues(base, []string{"config.count=42", "config.enabled=false"})
	if err != nil {
		t.Fatalf("applySetValues failed: %v", err)
	}

	count, err := ExtractInt(result, "config.count")
	if err != nil {
		t.Fatalf("failed to extract count: %v", err)
	}
	if count != 42 {
		t.Errorf("expected count 42, got %d", count)
	}

	enabled, err := ExtractBool(result, "config.enabled")
	if err != nil {
		t.Fatalf("failed to extract enabled: %v", err)
	}
	if enabled != false {
		t.Errorf("expected enabled false, got %v", enabled)
	}
}

func TestApplySetValues_InvalidFormat(t *testing.T) {
	loader := NewCUELoader()

	base := loader.ctx.CompileString(`foo: "bar"`)
	if base.Err() != nil {
		t.Fatalf("failed to compile base: %v", base.Err())
	}

	_, err := loader.applySetValues(base, []string{"invalid-no-equals"})
	if err == nil {
		t.Error("expected error for invalid format")
	} else if !strings.Contains(err.Error(), "expected key=value") {
		t.Errorf("expected 'expected key=value' in error, got: %v", err)
	}
}

func TestApplySetValues_NonExistentField(t *testing.T) {
	loader := NewCUELoader()

	base := loader.ctx.CompileString(`foo: "bar"`)
	if base.Err() != nil {
		t.Fatalf("failed to compile base: %v", base.Err())
	}

	_, err := loader.applySetValues(base, []string{"nonexistent.field=value"})
	if err == nil {
		t.Error("expected error for non-existent field")
	} else if !strings.Contains(err.Error(), "field does not exist") {
		t.Errorf("expected 'field does not exist' in error, got: %v", err)
	}
}

func TestBuildCUEOverrideExpr(t *testing.T) {
	tests := []struct {
		path     string
		value    string
		expected string
	}{
		{path: "tag", value: "v1", expected: `tag: "v1"`},
		{path: "images.tag", value: "v1", expected: `images: tag: "v1"`},
		{path: "a.b.c", value: "value", expected: `a: b: c: "value"`},
		{path: "_values.namespace", value: "cal", expected: `_values: namespace: "cal"`},
		{path: "count", value: "42", expected: `count: 42`},
		{path: "enabled", value: "true", expected: `enabled: true`},
	}

	for _, tt := range tests {
		t.Run(tt.path+"="+tt.value, func(t *testing.T) {
			result := buildCUEOverrideExpr(tt.path, tt.value)
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestCUEStdlibListConcat(t *testing.T) {
	projectRoot, err := findProjectRoot()
	if err != nil {
		t.Fatalf("failed to find project root: %v", err)
	}

	cueFile := `package test

import (
	"list"
	"strings"

	"github.com/x-qdo/ecsmate/pkg/cue:schema"
)

_tasks: [{
	command: "artisan queue:work"
}, {
	command: "artisan report:daily --verbose"
}]

manifest: schema.#Manifest & {
	name: "test"
	scheduledTasks: {
		for idx, task in _tasks {
			"task-\(idx)": {
				taskDefinition: "cron"
				cluster:        "test-cluster"
				schedule: {
					type:       "cron"
					expression: "0 * * * ? *"
				}
				overrides: {
					containerOverrides: [{
						name:    "command"
						command: list.Concat([["php", "/var/www/console"], strings.Split(task.command, " ")])
					}]
				}
			}
		}
	}
}
`

	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.cue")
	if err := os.WriteFile(testFile, []byte(cueFile), 0644); err != nil {
		t.Fatal(err)
	}

	cueModLink := filepath.Join(tmpDir, "cue.mod")
	projectCueMod := filepath.Join(projectRoot, "cue.mod")
	if err := os.Symlink(projectCueMod, cueModLink); err != nil {
		t.Fatalf("failed to symlink cue.mod: %v", err)
	}

	pkgDir := filepath.Join(tmpDir, "pkg")
	if err := os.MkdirAll(pkgDir, 0755); err != nil {
		t.Fatal(err)
	}
	pkgCueLink := filepath.Join(pkgDir, "cue")
	projectPkgCue := filepath.Join(projectRoot, "pkg", "cue")
	if err := os.Symlink(projectPkgCue, pkgCueLink); err != nil {
		t.Fatalf("failed to symlink pkg/cue: %v", err)
	}

	loader := NewCUELoader()
	value, err := loader.LoadManifest(tmpDir, nil, nil)
	if err != nil {
		t.Fatalf("LoadManifest failed: %v", err)
	}

	path0 := cue.MakePath(
		cue.Str("manifest"),
		cue.Str("scheduledTasks"),
		cue.Str("task-0"),
		cue.Str("overrides"),
		cue.Str("containerOverrides"),
		cue.Index(0),
		cue.Str("command"),
	)
	cmd0Val := value.LookupPath(path0)
	if cmd0Val.Err() != nil {
		t.Fatalf("failed to lookup task-0 command: %v", cmd0Val.Err())
	}
	cmd0 := extractList(t, cmd0Val)
	expected0 := []string{"php", "/var/www/console", "artisan", "queue:work"}
	if len(cmd0) != len(expected0) {
		t.Errorf("task-0 command length: got %d, want %d", len(cmd0), len(expected0))
	}
	for i, v := range expected0 {
		if i < len(cmd0) && cmd0[i] != v {
			t.Errorf("task-0 command[%d]: got %q, want %q", i, cmd0[i], v)
		}
	}

	path1 := cue.MakePath(
		cue.Str("manifest"),
		cue.Str("scheduledTasks"),
		cue.Str("task-1"),
		cue.Str("overrides"),
		cue.Str("containerOverrides"),
		cue.Index(0),
		cue.Str("command"),
	)
	cmd1Val := value.LookupPath(path1)
	if cmd1Val.Err() != nil {
		t.Fatalf("failed to lookup task-1 command: %v", cmd1Val.Err())
	}
	cmd1 := extractList(t, cmd1Val)
	expected1 := []string{"php", "/var/www/console", "artisan", "report:daily", "--verbose"}
	if len(cmd1) != len(expected1) {
		t.Errorf("task-1 command length: got %d, want %d", len(cmd1), len(expected1))
	}
	for i, v := range expected1 {
		if i < len(cmd1) && cmd1[i] != v {
			t.Errorf("task-1 command[%d]: got %q, want %q", i, cmd1[i], v)
		}
	}
}

func extractList(t *testing.T, v cue.Value) []string {
	t.Helper()
	iter, err := v.List()
	if err != nil {
		t.Fatalf("failed to get list: %v", err)
	}
	var result []string
	for iter.Next() {
		s, err := iter.Value().String()
		if err != nil {
			t.Fatalf("failed to get string: %v", err)
		}
		result = append(result, s)
	}
	return result
}

func TestCUEStdlibStringsFunctions(t *testing.T) {
	projectRoot, err := findProjectRoot()
	if err != nil {
		t.Fatalf("failed to find project root: %v", err)
	}

	cueFile := `package test

import (
	"strings"

	"github.com/x-qdo/ecsmate/pkg/cue:schema"
)

_input: "hello-world-test"

manifest: schema.#Manifest & {
	name: strings.Replace(_input, "-", "_", -1)
}
`

	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.cue")
	if err := os.WriteFile(testFile, []byte(cueFile), 0644); err != nil {
		t.Fatal(err)
	}

	cueModLink := filepath.Join(tmpDir, "cue.mod")
	projectCueMod := filepath.Join(projectRoot, "cue.mod")
	if err := os.Symlink(projectCueMod, cueModLink); err != nil {
		t.Fatalf("failed to symlink cue.mod: %v", err)
	}

	pkgDir := filepath.Join(tmpDir, "pkg")
	if err := os.MkdirAll(pkgDir, 0755); err != nil {
		t.Fatal(err)
	}
	pkgCueLink := filepath.Join(pkgDir, "cue")
	projectPkgCue := filepath.Join(projectRoot, "pkg", "cue")
	if err := os.Symlink(projectPkgCue, pkgCueLink); err != nil {
		t.Fatalf("failed to symlink pkg/cue: %v", err)
	}

	loader := NewCUELoader()
	value, err := loader.LoadManifest(tmpDir, nil, nil)
	if err != nil {
		t.Fatalf("LoadManifest failed: %v", err)
	}

	name, err := ExtractString(value, "manifest.name")
	if err != nil {
		t.Fatalf("failed to extract manifest name: %v", err)
	}
	if name != "hello_world_test" {
		t.Errorf("manifest.name: got %q, want %q", name, "hello_world_test")
	}
}

func TestCUEStdlibListFlatten(t *testing.T) {
	projectRoot, err := findProjectRoot()
	if err != nil {
		t.Fatalf("failed to find project root: %v", err)
	}

	cueFile := `package test

import (
	"list"

	"github.com/x-qdo/ecsmate/pkg/cue:schema"
)

_subnets: [
	["subnet-a1", "subnet-a2"],
	["subnet-b1"],
]

manifest: schema.#Manifest & {
	name: "test"
	services: web: {
		cluster:        "test"
		taskDefinition: "web"
		desiredCount:   1
		launchType:     "FARGATE"
		networkConfiguration: awsvpcConfiguration: {
			subnets:        list.FlattenN(_subnets, 1)
			securityGroups: ["sg-123"]
			assignPublicIp: "DISABLED"
		}
	}
}
`

	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.cue")
	if err := os.WriteFile(testFile, []byte(cueFile), 0644); err != nil {
		t.Fatal(err)
	}

	cueModLink := filepath.Join(tmpDir, "cue.mod")
	projectCueMod := filepath.Join(projectRoot, "cue.mod")
	if err := os.Symlink(projectCueMod, cueModLink); err != nil {
		t.Fatalf("failed to symlink cue.mod: %v", err)
	}

	pkgDir := filepath.Join(tmpDir, "pkg")
	if err := os.MkdirAll(pkgDir, 0755); err != nil {
		t.Fatal(err)
	}
	pkgCueLink := filepath.Join(pkgDir, "cue")
	projectPkgCue := filepath.Join(projectRoot, "pkg", "cue")
	if err := os.Symlink(projectPkgCue, pkgCueLink); err != nil {
		t.Fatalf("failed to symlink pkg/cue: %v", err)
	}

	loader := NewCUELoader()
	value, err := loader.LoadManifest(tmpDir, nil, nil)
	if err != nil {
		t.Fatalf("LoadManifest failed: %v", err)
	}

	subnets, err := ExtractStringSlice(value, "manifest.services.web.networkConfiguration.awsvpcConfiguration.subnets")
	if err != nil {
		t.Fatalf("failed to extract subnets: %v", err)
	}
	expected := []string{"subnet-a1", "subnet-a2", "subnet-b1"}
	if len(subnets) != len(expected) {
		t.Errorf("subnets length: got %d, want %d", len(subnets), len(expected))
	}
	for i, v := range expected {
		if i < len(subnets) && subnets[i] != v {
			t.Errorf("subnets[%d]: got %q, want %q", i, subnets[i], v)
		}
	}
}
