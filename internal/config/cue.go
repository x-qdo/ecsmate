package config

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/build"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/load"

	"github.com/x-qdo/ecsmate/internal/log"
	schema "github.com/x-qdo/ecsmate/pkg/cue"
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

	manifestAbsPath, err := filepath.Abs(manifestPath)
	if err != nil {
		return cue.Value{}, fmt.Errorf("failed to resolve manifest path %s: %w", manifestPath, err)
	}

	// Build load configuration
	cfg := &load.Config{
		Dir: manifestAbsPath,
	}
	moduleRoot, modulePath, err := findModuleRoot(manifestAbsPath)
	if err != nil {
		return cue.Value{}, err
	}

	// Use virtual module root if no real one found
	if moduleRoot == "" || modulePath == "" {
		moduleRoot = "/virtual/ecsmate"
		modulePath = "github.com/x-qdo/ecsmate"
	}
	cfg.ModuleRoot = moduleRoot
	cfg.Module = modulePath

	// Build overlay with embedded schema files
	cfg.Overlay = buildSchemaOverlay(moduleRoot)

	// Determine which files to load
	var files []string

	// Find all CUE files in the manifest directory
	entries, err := os.ReadDir(manifestAbsPath)
	if err != nil {
		return cue.Value{}, fmt.Errorf("failed to read manifest directory: %w", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".cue") {
			files = append(files, entry.Name())
		}
	}

	// Add taskdefs directory if exists
	taskdefsPath := filepath.Join(manifestAbsPath, "taskdefs")
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

	// Add values directory files directly in values/ (not subdirectories).
	// Subdirectory files (envs/*.cue, tenants/*.cue) must be explicitly specified via -f flag.
	valuesPath := filepath.Join(manifestAbsPath, "values")
	if stat, err := os.Stat(valuesPath); err == nil && stat.IsDir() {
		valuesEntries, err := os.ReadDir(valuesPath)
		if err == nil {
			for _, entry := range valuesEntries {
				// Only load .cue files directly in values/, skip subdirectories
				if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".cue") {
					files = append(files, filepath.Join("values", entry.Name()))
				}
			}
		}
	}

	// Load value files
	for _, vf := range valueFiles {
		absPath, err := resolveValueFilePath(manifestAbsPath, vf)
		if err != nil {
			return cue.Value{}, fmt.Errorf("failed to resolve value file path %s: %w", vf, err)
		}
		// If the value file is within the manifest directory, use relative path
		if relPath, ok := relativePathInDir(manifestAbsPath, absPath); ok {
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
		return cue.Value{}, NewCUELoadError(inst.Err.Error(), nil)
	}

	value := l.ctx.BuildInstance(inst)
	if value.Err() != nil {
		return cue.Value{}, NewCUEBuildError(value.Err().Error(), collectCUEErrors(value.Err()))
	}

	// Apply --set overrides after building using FillPath
	if len(setValues) > 0 {
		var err error
		value, err = l.applySetValues(value, setValues)
		if err != nil {
			return cue.Value{}, err
		}
	}

	if !HasSchemaImport(inst) {
		err := &ManifestError{
			Phase:   "load",
			Summary: "manifest must import schema package",
			Hint:    "Add to your CUE file:\n  import \"github.com/x-qdo/ecsmate/pkg/cue:schema\"\n  manifest: schema.#Manifest & { ... }",
		}
		return cue.Value{}, err
	}

	if err := value.Validate(); err != nil {
		return cue.Value{}, NewCUEValidationError(err.Error(), collectCUEErrors(err))
	}

	if !IsManifestConstrained(value) {
		err := &ManifestError{
			Phase:   "validate",
			Summary: "manifest must be constrained by schema.#Manifest",
			Hint:    "Use: manifest: schema.#Manifest & { ... }",
		}
		return cue.Value{}, err
	}

	return value, nil
}

func resolveValueFilePath(manifestAbsPath, valueFile string) (string, error) {
	absPath, err := filepath.Abs(valueFile)
	if err != nil {
		return "", err
	}
	if filepath.IsAbs(valueFile) {
		return absPath, nil
	}

	if _, err := os.Stat(absPath); err == nil || !os.IsNotExist(err) {
		return absPath, nil
	}

	manifestValuePath, err := filepath.Abs(filepath.Join(manifestAbsPath, valueFile))
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(manifestValuePath); err == nil {
		return manifestValuePath, nil
	}

	return absPath, nil
}

func relativePathInDir(baseDir, targetPath string) (string, bool) {
	relPath, err := filepath.Rel(baseDir, targetPath)
	if err != nil {
		return "", false
	}
	if relPath == "." {
		return relPath, true
	}
	if relPath == ".." || strings.HasPrefix(relPath, ".."+string(os.PathSeparator)) {
		return "", false
	}
	return relPath, true
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
	defer func() { _ = file.Close() }()

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
	defer func() { _ = file.Close() }()

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

func collectCUEErrors(err error) []string {
	if err == nil {
		return nil
	}

	seen := make(map[string]bool)
	var details []string

	for _, e := range errors.Errors(err) {
		pathParts := e.Path()
		path := strings.Join(pathParts, ".")
		if path == "" {
			path = "<root>"
		}

		pos := e.Position()
		var location string
		if pos.IsValid() {
			location = fmt.Sprintf(" (%s:%d:%d)", pos.Filename(), pos.Line(), pos.Column())
		}

		msg := e.Error()
		if strings.HasPrefix(msg, path+":") {
			msg = strings.TrimPrefix(msg, path+":")
			msg = strings.TrimSpace(msg)
		}

		detail := fmt.Sprintf("%s: %s%s", path, msg, location)
		if !seen[detail] {
			seen[detail] = true
			details = append(details, detail)
		}
	}

	return details
}

// applySetValues applies --set key=value overrides using FillPath
func (l *CUELoader) applySetValues(value cue.Value, setValues []string) (cue.Value, error) {
	if len(setValues) == 0 {
		return value, nil
	}

	result := value
	for _, sv := range setValues {
		parts := strings.SplitN(sv, "=", 2)
		if len(parts) != 2 {
			return cue.Value{}, fmt.Errorf(
				"invalid --set format %q: expected key=value (e.g., --set images.tag=v1.0.0)", sv,
			)
		}
		key, val := parts[0], parts[1]

		log.Debug("applying set override", "key", key, "value", val)

		// Resolve the path against the existing value. Hidden fields are
		// package-qualified in CUE, so reuse the selectors from the value.
		path, ok := findExistingCUEPath(result, key)
		if !ok {
			return cue.Value{}, fmt.Errorf(
				"--set %s: field does not exist\n  Available top-level fields: %s", key, listTopLevelFields(result),
			)
		}

		target := result.LookupPath(path)
		// Parse the value to proper CUE type
		expr := formatSetValueForTarget(target, val)
		cueVal := l.ctx.CompileString(expr)
		if cueVal.Err() != nil {
			return cue.Value{}, fmt.Errorf("--set %s: invalid value %q: %w", key, val, cueVal.Err())
		}

		// Use FillPath to override the value
		result = result.FillPath(path, cueVal)
		if result.Err() != nil {
			return cue.Value{}, fmt.Errorf("--set %s=%s: %w", key, val, result.Err())
		}
	}

	return result, nil
}

// findExistingCUEPath resolves a dot-separated path using selectors from the value.
func findExistingCUEPath(value cue.Value, path string) (cue.Path, bool) {
	parts := strings.Split(path, ".")
	selectors := make([]cue.Selector, 0, len(parts))
	current := value

	for _, part := range parts {
		iter, err := current.Fields(cue.All())
		if err != nil {
			return cue.Path{}, false
		}

		found := false
		for iter.Next() {
			selector := iter.Selector()
			if selectorName(selector) != part {
				continue
			}

			selectors = append(selectors, selector)
			current = iter.Value()
			found = true
			break
		}

		if !found {
			return cue.Path{}, false
		}
	}

	return cue.MakePath(selectors...), true
}

func selectorName(selector cue.Selector) string {
	name := selector.String()
	if unquoted, err := strconv.Unquote(name); err == nil {
		return unquoted
	}
	return name
}

// listTopLevelFields returns a comma-separated list of top-level field names for error messages
func listTopLevelFields(v cue.Value) string {
	var fields []string
	iter, _ := v.Fields(cue.All())
	for iter.Next() {
		fields = append(fields, iter.Selector().String())
	}
	if len(fields) == 0 {
		return "(none)"
	}
	return strings.Join(fields, ", ")
}

// buildCUEOverrideExpr builds a CUE struct expression for a dotted path.
func buildCUEOverrideExpr(path, value string) string {
	parts := strings.Split(path, ".")
	quotedValue := formatCUEValue(value)

	var sb strings.Builder
	for i, part := range parts {
		if i > 0 {
			sb.WriteString(" ")
		}
		sb.WriteString(part)
		sb.WriteString(":")
	}
	sb.WriteString(" ")
	sb.WriteString(quotedValue)

	return sb.String()
}

// formatSetValueForTarget formats a CLI --set value using the CUE kind of the
// target field.
func formatSetValueForTarget(target cue.Value, val string) string {
	if shouldKeepSetValueAsString(target) {
		return strconv.Quote(val)
	}

	return formatCUEValue(val)
}

// shouldKeepSetValueAsString returns true for fields that accept strings, but
// not the scalar types inferred by formatCUEValue. For those fields, values like
// 90681564 are data, not CUE numbers.
func shouldKeepSetValueAsString(target cue.Value) bool {
	if target.Err() != nil {
		return false
	}

	targetKind := target.IncompleteKind()
	acceptsString := targetKind&cue.StringKind != 0
	inferredScalarKinds := cue.BoolKind | cue.IntKind | cue.FloatKind | cue.NumberKind
	acceptsInferredScalar := targetKind&inferredScalarKinds != 0

	return acceptsString && !acceptsInferredScalar
}

// formatCUEValue formats a value for a CUE expression.
func formatCUEValue(val string) string {
	if _, err := strconv.ParseInt(val, 10, 64); err == nil {
		return val
	}
	if _, err := strconv.ParseFloat(val, 64); err == nil {
		return val
	}
	if val == "true" || val == "false" {
		return val
	}
	return fmt.Sprintf("%q", val)
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

// HasSchemaImport checks if the CUE instance imports the schema package
func HasSchemaImport(inst *build.Instance) bool {
	for _, imp := range inst.Imports {
		if imp.ImportPath == "github.com/x-qdo/ecsmate/pkg/cue:schema" {
			return true
		}
	}
	return false
}

// IsManifestConstrained checks if manifest is properly constrained by schema.
// This validates that the manifest field exists and uses schema definitions.
func IsManifestConstrained(value cue.Value) bool {
	manifest := value.LookupPath(cue.ParsePath("manifest"))
	if !manifest.Exists() {
		return false
	}

	// If schema is imported and manifest validates, check that it has
	// the expected structure from #Manifest (name field is required)
	name := manifest.LookupPath(cue.ParsePath("name"))
	return name.Exists() && name.Err() == nil
}

// buildSchemaOverlay creates a CUE overlay with embedded schema files.
// This allows schema imports to work regardless of where ecsmate is invoked.
func buildSchemaOverlay(moduleRoot string) map[string]load.Source {
	overlay := make(map[string]load.Source)

	// Add module.cue to establish module identity
	moduleCue := `module: "github.com/x-qdo/ecsmate"
language: { version: "v0.11.0" }`
	overlay[filepath.Join(moduleRoot, "cue.mod", "module.cue")] = load.FromString(moduleCue)

	// Add embedded schema files from pkg/cue
	schemaDir := filepath.Join(moduleRoot, "pkg", "cue")
	entries, err := schema.EmbeddedSchema.ReadDir(".")
	if err != nil {
		return overlay
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".cue") {
			continue
		}
		content, err := schema.EmbeddedSchema.ReadFile(entry.Name())
		if err != nil {
			continue
		}
		overlay[filepath.Join(schemaDir, entry.Name())] = load.FromBytes(content)
	}

	return overlay
}
