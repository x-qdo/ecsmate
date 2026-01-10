package config

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/cue/load"

	"github.com/qdo/ecsmate/internal/log"
)

// Schema files are loaded from pkg/cue at runtime

type CUELoader struct {
	ctx *cue.Context
}

func NewCUELoader() *CUELoader {
	return &CUELoader{
		ctx: cuecontext.New(),
	}
}

// LoadManifest loads and evaluates a manifest directory with values
func (l *CUELoader) LoadManifest(manifestPath string, valueFiles []string, setValues []string) (cue.Value, error) {
	log.Debug("loading manifest", "path", manifestPath, "valueFiles", valueFiles, "setValues", setValues)

	// Build load configuration
	cfg := &load.Config{
		Dir: manifestPath,
	}
	if moduleRoot, modulePath, err := findModuleRoot(manifestPath); err != nil {
		return cue.Value{}, err
	} else if moduleRoot != "" && modulePath != "" {
		cfg.ModuleRoot = moduleRoot
		cfg.Module = modulePath
	}

	// Determine which files to load
	var files []string

	// Find all CUE files in the manifest directory
	entries, err := os.ReadDir(manifestPath)
	if err != nil {
		return cue.Value{}, fmt.Errorf("failed to read manifest directory: %w", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".cue") {
			files = append(files, entry.Name())
		}
	}

	// Add taskdefs directory if exists
	taskdefsPath := filepath.Join(manifestPath, "taskdefs")
	if stat, err := os.Stat(taskdefsPath); err == nil && stat.IsDir() {
		taskdefEntries, err := os.ReadDir(taskdefsPath)
		if err == nil {
			for _, entry := range taskdefEntries {
				if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".cue") {
					files = append(files, filepath.Join("taskdefs", entry.Name()))
				}
			}
		}
	}

	// Add values directory files (shared defaults and SSM config)
	valuesPath := filepath.Join(manifestPath, "values")
	if stat, err := os.Stat(valuesPath); err == nil && stat.IsDir() {
		valuesEntries, err := os.ReadDir(valuesPath)
		if err == nil {
			for _, entry := range valuesEntries {
				if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".cue") {
					files = append(files, filepath.Join("values", entry.Name()))
				}
			}
		}
	}

	// Load value files
	for _, vf := range valueFiles {
		absPath, err := filepath.Abs(vf)
		if err != nil {
			return cue.Value{}, fmt.Errorf("failed to resolve value file path %s: %w", vf, err)
		}
		// If the value file is within the manifest directory, use relative path
		if relPath, err := filepath.Rel(manifestPath, absPath); err == nil && !strings.HasPrefix(relPath, "..") {
			files = append(files, relPath)
		} else {
			// Otherwise, use absolute path
			files = append(files, absPath)
		}
	}

	log.Debug("loading CUE files", "files", files)

	// Load the instances
	instances := load.Instances(files, cfg)
	if len(instances) == 0 {
		return cue.Value{}, fmt.Errorf("no CUE instances found in %s", manifestPath)
	}

	inst := instances[0]
	if inst.Err != nil {
		return cue.Value{}, fmt.Errorf("failed to load CUE instance: %w", inst.Err)
	}

	// Build the value
	value := l.ctx.BuildInstance(inst)
	if value.Err() != nil {
		return cue.Value{}, fmt.Errorf("failed to build CUE value: %w", value.Err())
	}

	// Apply --set overrides
	if len(setValues) > 0 {
		value, err = l.applySetValues(value, setValues)
		if err != nil {
			return cue.Value{}, fmt.Errorf("failed to apply set values: %w", err)
		}
	}

	// Validate against schema
	if err := value.Validate(); err != nil {
		return cue.Value{}, fmt.Errorf("CUE validation failed: %w", err)
	}

	return value, nil
}

func findModuleRoot(start string) (string, string, error) {
	dir, err := filepath.Abs(start)
	if err != nil {
		return "", "", fmt.Errorf("failed to resolve manifest path: %w", err)
	}

	for {
		cueModule := filepath.Join(dir, "cue.mod", "module.cue")
		if _, err := os.Stat(cueModule); err == nil {
			modulePath, err := readCueModule(cueModule)
			if err != nil {
				return "", "", err
			}
			return dir, modulePath, nil
		}

		goModule := filepath.Join(dir, "go.mod")
		if _, err := os.Stat(goModule); err == nil {
			modulePath, err := readGoModule(goModule)
			if err != nil {
				return "", "", err
			}
			return dir, modulePath, nil
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	return "", "", nil
}

func readGoModule(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("failed to open go.mod: %w", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "module ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "module ")), nil
		}
	}

	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("failed to read go.mod: %w", err)
	}

	return "", nil
}

func readCueModule(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("failed to open cue module file: %w", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "module:") {
			value := strings.TrimSpace(strings.TrimPrefix(line, "module:"))
			value = strings.Trim(value, "\"")
			return value, nil
		}
	}

	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("failed to read cue module file: %w", err)
	}

	return "", nil
}

// applySetValues applies --set key=value overrides
func (l *CUELoader) applySetValues(value cue.Value, setValues []string) (cue.Value, error) {
	for _, sv := range setValues {
		parts := strings.SplitN(sv, "=", 2)
		if len(parts) != 2 {
			return cue.Value{}, fmt.Errorf("invalid set value format: %s (expected key=value)", sv)
		}
		key, val := parts[0], parts[1]

		// Build a CUE expression for the override
		// Convert key path like "image.tag" to CUE path
		pathParts := strings.Split(key, ".")

		// Create override value
		override := l.ctx.CompileString(fmt.Sprintf("%q", val))
		if override.Err() != nil {
			// Try as raw value (for numbers, bools)
			override = l.ctx.CompileString(val)
			if override.Err() != nil {
				return cue.Value{}, fmt.Errorf("failed to compile set value %s=%s: %w", key, val, override.Err())
			}
		}

		// Apply the override using FillPath
		selectors := make([]cue.Selector, len(pathParts))
		for i, p := range pathParts {
			selectors[i] = cue.Str(p)
		}
		value = value.FillPath(cue.MakePath(selectors...), override)
	}

	return value, nil
}

// GetManifest extracts the manifest from a CUE value
func (l *CUELoader) GetManifest(value cue.Value) (cue.Value, error) {
	manifest := value.LookupPath(cue.ParsePath("manifest"))
	if manifest.Err() != nil {
		// Try looking at root level
		return value, nil
	}
	return manifest, nil
}

// ExtractString extracts a string value at the given path
func ExtractString(v cue.Value, path string) (string, error) {
	val := v.LookupPath(cue.ParsePath(path))
	if val.Err() != nil {
		return "", val.Err()
	}
	return val.String()
}

// ExtractInt extracts an int value at the given path
func ExtractInt(v cue.Value, path string) (int64, error) {
	val := v.LookupPath(cue.ParsePath(path))
	if val.Err() != nil {
		return 0, val.Err()
	}
	return val.Int64()
}

// ExtractBool extracts a bool value at the given path
func ExtractBool(v cue.Value, path string) (bool, error) {
	val := v.LookupPath(cue.ParsePath(path))
	if val.Err() != nil {
		return false, val.Err()
	}
	return val.Bool()
}

// ExtractStringSlice extracts a string slice at the given path
func ExtractStringSlice(v cue.Value, path string) ([]string, error) {
	val := v.LookupPath(cue.ParsePath(path))
	if val.Err() != nil {
		return nil, val.Err()
	}

	iter, err := val.List()
	if err != nil {
		return nil, err
	}

	var result []string
	for iter.Next() {
		s, err := iter.Value().String()
		if err != nil {
			return nil, err
		}
		result = append(result, s)
	}

	return result, nil
}
