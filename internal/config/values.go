package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/cue/load"

	"github.com/qdo/ecsmate/internal/log"
)

// ValuesLoader handles loading and merging of CUE value files
type ValuesLoader struct {
	ctx *cue.Context
}

// NewValuesLoader creates a new ValuesLoader
func NewValuesLoader() *ValuesLoader {
	return &ValuesLoader{
		ctx: cuecontext.New(),
	}
}

// LoadValues loads and merges multiple value files in order
// Later files override earlier ones
func (l *ValuesLoader) LoadValues(manifestPath string, valueFiles []string) (cue.Value, error) {
	log.Debug("loading values", "manifestPath", manifestPath, "valueFiles", valueFiles)

	// Start with default values from the manifest's values directory
	defaultValuesPath := filepath.Join(manifestPath, "values", "default.cue")
	var allFiles []string

	if _, err := os.Stat(defaultValuesPath); err == nil {
		allFiles = append(allFiles, defaultValuesPath)
	}

	// Add explicitly specified value files
	for _, vf := range valueFiles {
		absPath := vf
		if !filepath.IsAbs(vf) {
			// Try relative to manifest path first
			relToManifest := filepath.Join(manifestPath, vf)
			if _, err := os.Stat(relToManifest); err == nil {
				absPath = relToManifest
			} else {
				// Try relative to current directory
				var err error
				absPath, err = filepath.Abs(vf)
				if err != nil {
					return cue.Value{}, fmt.Errorf("failed to resolve value file path %s: %w", vf, err)
				}
			}
		}
		allFiles = append(allFiles, absPath)
	}

	if len(allFiles) == 0 {
		// Return empty value if no value files
		return l.ctx.CompileString("{}"), nil
	}

	log.Debug("loading value files", "files", allFiles)

	// Load all value files
	cfg := &load.Config{}
	instances := load.Instances(allFiles, cfg)

	if len(instances) == 0 {
		return cue.Value{}, fmt.Errorf("no CUE instances found for values")
	}

	// Unify all instances
	var result cue.Value
	for i, inst := range instances {
		if inst.Err != nil {
			return cue.Value{}, fmt.Errorf("failed to load value file %s: %w", allFiles[i], inst.Err)
		}

		value := l.ctx.BuildInstance(inst)
		if value.Err() != nil {
			return cue.Value{}, fmt.Errorf("failed to build value file %s: %w", allFiles[i], value.Err())
		}

		if i == 0 {
			result = value
		} else {
			// Unify values (later values override)
			result = result.Unify(value)
			if result.Err() != nil {
				return cue.Value{}, fmt.Errorf("failed to merge value files: %w", result.Err())
			}
		}
	}

	return result, nil
}

// ApplySetOverrides applies --set key=value overrides to a values object
func (l *ValuesLoader) ApplySetOverrides(values cue.Value, setValues []string) (cue.Value, error) {
	for _, sv := range setValues {
		parts := strings.SplitN(sv, "=", 2)
		if len(parts) != 2 {
			return cue.Value{}, fmt.Errorf("invalid set value format: %s (expected key=value)", sv)
		}
		key, val := parts[0], parts[1]

		log.Debug("applying set override", "key", key, "value", val)

		// Convert key path like "image.tag" to CUE path
		pathParts := strings.Split(key, ".")

		// Try to compile as string first, then as raw value
		override := l.ctx.CompileString(fmt.Sprintf("%q", val))
		if override.Err() != nil {
			override = l.ctx.CompileString(val)
			if override.Err() != nil {
				return cue.Value{}, fmt.Errorf("failed to compile set value %s=%s: %w", key, val, override.Err())
			}
		}

		// Apply using FillPath
		selectors := make([]cue.Selector, len(pathParts))
		for i, p := range pathParts {
			selectors[i] = cue.Str(p)
		}
		values = values.FillPath(cue.MakePath(selectors...), override)
	}

	return values, nil
}

// MergeValuesIntoManifest combines loaded values with the manifest
func MergeValuesIntoManifest(manifest, values cue.Value) (cue.Value, error) {
	// Create a new value with values nested under "values" key
	ctx := cuecontext.New()

	// Build expression that puts values under the "values" field
	combined := ctx.CompileString(`{
		values: _values
		_values: {}
	}`)

	// Fill in the actual values
	combined = combined.FillPath(cue.ParsePath("_values"), values)

	// Unify with manifest
	result := manifest.Unify(combined)
	if result.Err() != nil {
		return cue.Value{}, fmt.Errorf("failed to merge values into manifest: %w", result.Err())
	}

	return result, nil
}

// ExtractValues extracts the values section from a combined manifest
func ExtractValues(combined cue.Value) (cue.Value, error) {
	values := combined.LookupPath(cue.ParsePath("values"))
	if !values.Exists() {
		return cuecontext.New().CompileString("{}"), nil
	}
	return values, nil
}
