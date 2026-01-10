package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
import "github.com/qdo/ecsmate/pkg/cue:schema"
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
