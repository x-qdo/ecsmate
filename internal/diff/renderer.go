//nolint:errcheck // Diff rendering writes best-effort CLI output; write failures are not actionable here.
package diff

import (
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"sort"
	"strings"

	"github.com/fatih/color"
	"github.com/x-qdo/ecsmate/internal/config"
)

// Box-drawing characters for visual grouping
const (
	boxTop    = "┌"
	boxMid    = "│"
	boxBottom = "└"
	boxHoriz  = "─"
)

// ecsAssignedDefaults lists fields that ECS assigns automatically when not specified.
// These should be ignored in diffs when they exist in current (remote) but not in desired (local).
var ecsAssignedDefaults = map[string]bool{
	"hostPort": true, // ECS assigns hostPort if not specified (usually 0 or same as containerPort)
}

type Renderer struct {
	out         io.Writer
	noColor     bool
	addColor    *color.Color
	delColor    *color.Color
	hdrColor    *color.Color
	ctxColor    *color.Color
	actionColor *color.Color
	dimColor    *color.Color
}

func NewRenderer(out io.Writer, noColor bool) *Renderer {
	r := &Renderer{
		out:         out,
		noColor:     noColor,
		addColor:    color.New(color.FgGreen),
		delColor:    color.New(color.FgRed),
		hdrColor:    color.New(color.FgCyan, color.Bold),
		ctxColor:    color.New(color.FgWhite),
		actionColor: color.New(color.FgYellow, color.Bold),
		dimColor:    color.New(color.FgHiBlack),
	}

	if noColor {
		color.NoColor = true
	}

	return r
}

type DiffType string

const (
	DiffTypeCreate   DiffType = "CREATE"
	DiffTypeUpdate   DiffType = "UPDATE"
	DiffTypeDelete   DiffType = "DELETE"
	DiffTypeRecreate DiffType = "RECREATE"
	DiffTypeNoop     DiffType = "NOOP"
)

type DiffEntry struct {
	Type              DiffType
	Name              string
	Resource          string
	Current           interface{}
	Desired           interface{}
	Details           string
	RecreateReasons   []string
	PropagationReason string   // Why this change was propagated from another resource
	Hooks             []string // Hook commands that will run during deployment
}

// RenderHeader displays the planning header with manifest name
func (r *Renderer) RenderHeader(manifestName string) {
	r.hdrColor.Fprintf(r.out, "Planning ")
	r.actionColor.Fprintf(r.out, "apply")
	r.hdrColor.Fprintf(r.out, " %q\n", manifestName)
}

func (r *Renderer) RenderDiff(entries []DiffEntry) {
	if len(entries) == 0 {
		fmt.Fprintln(r.out, "No changes detected.")
		return
	}

	// Filter out noops for cleaner output
	var changedEntries []DiffEntry
	for _, e := range entries {
		if e.Type != DiffTypeNoop {
			changedEntries = append(changedEntries, e)
		}
	}

	if len(changedEntries) == 0 {
		r.ctxColor.Fprintln(r.out, "No changes detected.")
		return
	}

	// Sort entries by execution phase order
	sortByExecutionPhase(changedEntries)

	fmt.Fprintln(r.out)
	for _, entry := range changedEntries {
		r.renderEntryBoxed(entry)
	}
}

// sortByExecutionPhase sorts entries according to the execution order:
// Phase 1: Dependencies (TargetGroup)
// Phase 2: Core Resources (TaskDefinition)
// Phase 3: Routing (ListenerRule)
// Phase 4: Deployments (Service, ScheduledTask)
func sortByExecutionPhase(entries []DiffEntry) {
	phaseOrder := map[string]int{
		"SSMParameter":   0,
		"TargetGroup":    1,
		"TaskDefinition": 2,
		"ListenerRule":   3,
		"Service":        4,
		"ScheduledTask":  4,
	}

	sort.SliceStable(entries, func(i, j int) bool {
		pi := phaseOrder[entries[i].Resource]
		pj := phaseOrder[entries[j].Resource]
		if pi != pj {
			return pi < pj
		}
		// Within same phase, sort by name
		return entries[i].Name < entries[j].Name
	})
}

// renderEntryBoxed renders a single entry with visual box grouping (nelm-style)
func (r *Renderer) renderEntryBoxed(entry DiffEntry) {
	action, actionColor := r.getActionInfo(entry.Type)
	resourcePath := fmt.Sprintf("%s/%s", entry.Resource, entry.Name)

	// Top line: ┌ Action Resource/Name
	r.dimColor.Fprint(r.out, boxTop+" ")
	actionColor.Fprint(r.out, action)
	fmt.Fprint(r.out, " ")
	r.ctxColor.Fprintln(r.out, resourcePath)

	// Content with box continuation
	switch entry.Type {
	case DiffTypeCreate:
		r.renderBoxedCreate(entry)
	case DiffTypeUpdate:
		r.renderBoxedUpdate(entry)
	case DiffTypeRecreate:
		r.renderBoxedRecreate(entry)
	case DiffTypeDelete:
		r.renderBoxedDelete(entry)
	}

	// Show hooks that will run during deployment
	if len(entry.Hooks) > 0 {
		r.dimColor.Fprint(r.out, boxMid)
		r.ctxColor.Fprintln(r.out, " hooks:")
		for _, hook := range entry.Hooks {
			r.dimColor.Fprint(r.out, boxMid)
			r.actionColor.Fprintf(r.out, "   → %s\n", hook)
		}
	}

	// Bottom line: └ Action Resource/Name
	r.dimColor.Fprint(r.out, boxBottom+" ")
	actionColor.Fprint(r.out, action)
	fmt.Fprint(r.out, " ")
	r.ctxColor.Fprintln(r.out, resourcePath)
	fmt.Fprintln(r.out)
}

func (r *Renderer) getActionInfo(diffType DiffType) (string, *color.Color) {
	switch diffType {
	case DiffTypeCreate:
		return "Create", r.addColor
	case DiffTypeUpdate:
		return "Update", r.actionColor
	case DiffTypeDelete:
		return "Delete", r.delColor
	case DiffTypeRecreate:
		return "Recreate", r.actionColor
	default:
		return "Unknown", r.ctxColor
	}
}

func (r *Renderer) renderBoxedCreate(entry DiffEntry) {
	if entry.Desired == nil {
		return
	}
	lines := r.formatAsLines(entry.Desired)
	for _, line := range lines {
		r.dimColor.Fprint(r.out, boxMid)
		r.addColor.Fprintln(r.out, " + "+line)
	}
}

func (r *Renderer) renderBoxedDelete(entry DiffEntry) {
	if entry.Current == nil {
		return
	}
	lines := r.formatAsLines(entry.Current)
	for _, line := range lines {
		r.dimColor.Fprint(r.out, boxMid)
		r.delColor.Fprintln(r.out, " - "+line)
	}
}

func (r *Renderer) renderBoxedUpdate(entry DiffEntry) {
	if entry.Current == nil || entry.Desired == nil {
		return
	}

	currentMap := toMap(entry.Current)
	desiredMap := toMap(entry.Desired)

	if currentMap == nil || desiredMap == nil {
		return
	}

	r.renderBoxedMapDiff(currentMap, desiredMap, "")
}

func (r *Renderer) renderBoxedRecreate(entry DiffEntry) {
	if len(entry.RecreateReasons) > 0 {
		r.dimColor.Fprint(r.out, boxMid)
		r.actionColor.Fprintln(r.out, "   # Reasons for recreation:")
		for _, reason := range entry.RecreateReasons {
			r.dimColor.Fprint(r.out, boxMid)
			r.actionColor.Fprintf(r.out, "   #   - %s\n", reason)
		}
	}
	r.renderBoxedUpdate(entry)
}

func (r *Renderer) renderBoxedMapDiff(current, desired map[string]interface{}, indent string) {
	allKeys := make(map[string]bool)
	for k := range current {
		allKeys[k] = true
	}
	for k := range desired {
		allKeys[k] = true
	}

	keys := make([]string, 0, len(allKeys))
	for k := range allKeys {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, key := range keys {
		currentVal, currentExists := current[key]
		desiredVal, desiredExists := desired[key]

		if !currentExists {
			// New field - expand complex values
			r.renderBoxedNewField(key, desiredVal, indent)
		} else if !desiredExists {
			// Field exists in current but not in desired
			// Skip if it's an ECS-assigned default (e.g., hostPort assigned by ECS when not specified)
			if ecsAssignedDefaults[key] {
				continue
			}
			// Removed field - expand complex values
			r.renderBoxedRemovedField(key, currentVal, indent)
		} else if !deepEqual(currentVal, desiredVal) {
			// Changed field - check if it's a nested structure
			currentMap, currentIsMap := currentVal.(map[string]interface{})
			desiredMap, desiredIsMap := desiredVal.(map[string]interface{})
			currentArr, currentIsArr := currentVal.([]interface{})
			desiredArr, desiredIsArr := desiredVal.([]interface{})

			if currentIsMap && desiredIsMap {
				// Nested object - show key as context, then recurse
				r.dimColor.Fprint(r.out, boxMid)
				r.ctxColor.Fprintf(r.out, "   %s%s:\n", indent, key)
				r.renderBoxedMapDiff(currentMap, desiredMap, indent+"  ")
			} else if currentIsArr && desiredIsArr {
				// Array - show changes
				r.dimColor.Fprint(r.out, boxMid)
				r.ctxColor.Fprintf(r.out, "   %s%s:\n", indent, key)
				r.renderBoxedArrayDiff(currentArr, desiredArr, indent+"  ")
			} else {
				// Value change - expand complex values
				r.renderBoxedRemovedField(key, currentVal, indent)
				r.renderBoxedNewField(key, desiredVal, indent)
			}
		} else {
			// Unchanged - show as context (dimmed), expand complex values
			if key == "environment" {
				continue
			}
			r.renderBoxedContextField(key, currentVal, indent)
		}
	}
}

func (r *Renderer) renderBoxedNewField(key string, val interface{}, indent string) {
	switch v := val.(type) {
	case map[string]interface{}:
		r.dimColor.Fprint(r.out, boxMid)
		r.addColor.Fprintf(r.out, " + %s%s:\n", indent, key)
		r.renderBoxedNewMap(v, indent+"  ")
	case []interface{}:
		r.dimColor.Fprint(r.out, boxMid)
		r.addColor.Fprintf(r.out, " + %s%s:\n", indent, key)
		r.renderBoxedNewArray(v, indent+"  ")
	default:
		r.dimColor.Fprint(r.out, boxMid)
		r.addColor.Fprintf(r.out, " + %s%s: %s\n", indent, key, formatSimpleValue(val))
	}
}

func (r *Renderer) renderBoxedRemovedField(key string, val interface{}, indent string) {
	switch v := val.(type) {
	case map[string]interface{}:
		r.dimColor.Fprint(r.out, boxMid)
		r.delColor.Fprintf(r.out, " - %s%s:\n", indent, key)
		r.renderBoxedRemovedMap(v, indent+"  ")
	case []interface{}:
		r.dimColor.Fprint(r.out, boxMid)
		r.delColor.Fprintf(r.out, " - %s%s:\n", indent, key)
		r.renderBoxedRemovedArray(v, indent+"  ")
	default:
		r.dimColor.Fprint(r.out, boxMid)
		r.delColor.Fprintf(r.out, " - %s%s: %s\n", indent, key, formatSimpleValue(val))
	}
}

func (r *Renderer) renderBoxedContextField(key string, val interface{}, indent string) {
	switch v := val.(type) {
	case map[string]interface{}:
		r.dimColor.Fprint(r.out, boxMid)
		r.dimColor.Fprintf(r.out, "   %s%s:\n", indent, key)
		r.renderBoxedContextMap(v, indent+"  ")
	case []interface{}:
		r.dimColor.Fprint(r.out, boxMid)
		r.dimColor.Fprintf(r.out, "   %s%s:\n", indent, key)
		r.renderBoxedContextArray(v, indent+"  ")
	default:
		r.dimColor.Fprint(r.out, boxMid)
		r.dimColor.Fprintf(r.out, "   %s%s: %s\n", indent, key, formatSimpleValue(val))
	}
}

func (r *Renderer) renderBoxedRemovedMap(m map[string]interface{}, indent string) {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, key := range keys {
		r.renderBoxedRemovedField(key, m[key], indent)
	}
}

func (r *Renderer) renderBoxedRemovedArray(arr []interface{}, indent string) {
	for i, item := range arr {
		switch v := item.(type) {
		case map[string]interface{}:
			if name, ok := v["name"].(string); ok {
				r.dimColor.Fprint(r.out, boxMid)
				r.delColor.Fprintf(r.out, " - %s[%s]:\n", indent, name)
				r.renderBoxedRemovedMap(v, indent+"  ")
			} else {
				r.dimColor.Fprint(r.out, boxMid)
				r.delColor.Fprintf(r.out, " - %s[%d]:\n", indent, i)
				r.renderBoxedRemovedMap(v, indent+"  ")
			}
		default:
			r.dimColor.Fprint(r.out, boxMid)
			r.delColor.Fprintf(r.out, " - %s[%d]: %s\n", indent, i, formatSimpleValue(item))
		}
	}
}

func (r *Renderer) renderBoxedContextMap(m map[string]interface{}, indent string) {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, key := range keys {
		r.renderBoxedContextField(key, m[key], indent)
	}
}

func (r *Renderer) renderBoxedContextArray(arr []interface{}, indent string) {
	for i, item := range arr {
		switch v := item.(type) {
		case map[string]interface{}:
			if name, ok := v["name"].(string); ok {
				r.dimColor.Fprint(r.out, boxMid)
				r.dimColor.Fprintf(r.out, "   %s[%s]:\n", indent, name)
				r.renderBoxedContextMap(v, indent+"  ")
			} else {
				r.dimColor.Fprint(r.out, boxMid)
				r.dimColor.Fprintf(r.out, "   %s[%d]:\n", indent, i)
				r.renderBoxedContextMap(v, indent+"  ")
			}
		default:
			r.dimColor.Fprint(r.out, boxMid)
			r.dimColor.Fprintf(r.out, "   %s[%d]: %s\n", indent, i, formatSimpleValue(item))
		}
	}
}

func (r *Renderer) renderBoxedNewArray(arr []interface{}, indent string) {
	for i, item := range arr {
		switch v := item.(type) {
		case map[string]interface{}:
			if name, ok := v["name"].(string); ok {
				r.dimColor.Fprint(r.out, boxMid)
				r.addColor.Fprintf(r.out, " + %s[%s]:\n", indent, name)
				r.renderBoxedNewMap(v, indent+"  ")
			} else {
				r.dimColor.Fprint(r.out, boxMid)
				r.addColor.Fprintf(r.out, " + %s[%d]:\n", indent, i)
				r.renderBoxedNewMap(v, indent+"  ")
			}
		default:
			r.dimColor.Fprint(r.out, boxMid)
			r.addColor.Fprintf(r.out, " + %s[%d]: %s\n", indent, i, formatSimpleValue(item))
		}
	}
}

func (r *Renderer) renderBoxedArrayDiff(current, desired []interface{}, indent string) {
	// Try to match by name field
	currentByName := indexByName(current)
	desiredByName := indexByName(desired)

	if len(currentByName) > 0 || len(desiredByName) > 0 {
		allNames := make(map[string]bool)
		for name := range currentByName {
			allNames[name] = true
		}
		for name := range desiredByName {
			allNames[name] = true
		}

		names := make([]string, 0, len(allNames))
		for name := range allNames {
			names = append(names, name)
		}
		sort.Strings(names)

		for _, name := range names {
			currentItem, currentExists := currentByName[name]
			desiredItem, desiredExists := desiredByName[name]

			if !currentExists {
				r.dimColor.Fprint(r.out, boxMid)
				r.addColor.Fprintf(r.out, " + %s[%s]: (new)\n", indent, name)
				if m, ok := desiredItem.(map[string]interface{}); ok {
					r.renderBoxedNewMap(m, indent+"  ")
				}
			} else if !desiredExists {
				r.dimColor.Fprint(r.out, boxMid)
				r.delColor.Fprintf(r.out, " - %s[%s]: (removed)\n", indent, name)
			} else if !deepEqual(currentItem, desiredItem) {
				r.dimColor.Fprint(r.out, boxMid)
				r.ctxColor.Fprintf(r.out, "   %s[%s]:\n", indent, name)
				if currentMap, ok := currentItem.(map[string]interface{}); ok {
					if desiredMap, ok := desiredItem.(map[string]interface{}); ok {
						r.renderBoxedMapDiff(currentMap, desiredMap, indent+"  ")
					}
				}
			}
		}
		return
	}

	// Fall back to index-based comparison
	maxLen := len(current)
	if len(desired) > maxLen {
		maxLen = len(desired)
	}

	for i := 0; i < maxLen; i++ {
		if i >= len(current) {
			// New item - expand if complex
			r.renderBoxedArrayItemNew(i, desired[i], indent)
		} else if i >= len(desired) {
			// Removed item - expand if complex
			r.renderBoxedArrayItemRemoved(i, current[i], indent)
		} else if !deepEqual(current[i], desired[i]) {
			// Changed item - check if both are maps for structural diff
			currentMap, currentIsMap := current[i].(map[string]interface{})
			desiredMap, desiredIsMap := desired[i].(map[string]interface{})

			if currentIsMap && desiredIsMap {
				r.dimColor.Fprint(r.out, boxMid)
				r.ctxColor.Fprintf(r.out, "   %s[%d]:\n", indent, i)
				r.renderBoxedMapDiff(currentMap, desiredMap, indent+"  ")
			} else {
				r.renderBoxedArrayItemRemoved(i, current[i], indent)
				r.renderBoxedArrayItemNew(i, desired[i], indent)
			}
		}
	}
}

func (r *Renderer) renderBoxedArrayItemNew(idx int, item interface{}, indent string) {
	switch v := item.(type) {
	case map[string]interface{}:
		r.dimColor.Fprint(r.out, boxMid)
		r.addColor.Fprintf(r.out, " + %s[%d]:\n", indent, idx)
		r.renderBoxedNewMap(v, indent+"  ")
	case []interface{}:
		r.dimColor.Fprint(r.out, boxMid)
		r.addColor.Fprintf(r.out, " + %s[%d]:\n", indent, idx)
		r.renderBoxedNewArray(v, indent+"  ")
	default:
		r.dimColor.Fprint(r.out, boxMid)
		r.addColor.Fprintf(r.out, " + %s[%d]: %s\n", indent, idx, formatSimpleValue(item))
	}
}

func (r *Renderer) renderBoxedArrayItemRemoved(idx int, item interface{}, indent string) {
	switch v := item.(type) {
	case map[string]interface{}:
		r.dimColor.Fprint(r.out, boxMid)
		r.delColor.Fprintf(r.out, " - %s[%d]:\n", indent, idx)
		r.renderBoxedRemovedMap(v, indent+"  ")
	case []interface{}:
		r.dimColor.Fprint(r.out, boxMid)
		r.delColor.Fprintf(r.out, " - %s[%d]:\n", indent, idx)
		r.renderBoxedRemovedArray(v, indent+"  ")
	default:
		r.dimColor.Fprint(r.out, boxMid)
		r.delColor.Fprintf(r.out, " - %s[%d]: %s\n", indent, idx, formatSimpleValue(item))
	}
}

func (r *Renderer) renderBoxedNewMap(m map[string]interface{}, indent string) {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, key := range keys {
		r.renderBoxedNewField(key, m[key], indent)
	}
}

func (r *Renderer) formatAsLines(v interface{}) []string {
	data, err := json.MarshalIndent(redactResolvedSSMValues(v), "", "  ")
	if err != nil {
		return nil
	}
	return strings.Split(string(data), "\n")
}

func toMap(v interface{}) map[string]interface{} {
	v = redactResolvedSSMValues(v)

	// If already a map, return it
	if m, ok := v.(map[string]interface{}); ok {
		return m
	}

	// Convert via JSON
	data, err := json.Marshal(v)
	if err != nil {
		return nil
	}

	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		return nil
	}

	return m
}

func redactResolvedSSMValues(v interface{}) interface{} {
	switch val := v.(type) {
	case string:
		return config.RedactResolvedSSMValue(val)
	case map[string]interface{}:
		redacted := make(map[string]interface{}, len(val))
		for k, item := range val {
			redacted[k] = redactResolvedSSMValues(item)
		}
		return redacted
	case []interface{}:
		redacted := make([]interface{}, len(val))
		for i, item := range val {
			redacted[i] = redactResolvedSSMValues(item)
		}
		return redacted
	case nil, bool, float64:
		return val
	default:
		data, err := json.Marshal(val)
		if err != nil {
			return val
		}

		var decoded interface{}
		if err := json.Unmarshal(data, &decoded); err != nil {
			return val
		}
		return redactResolvedSSMValues(decoded)
	}
}

func indexByName(arr []interface{}) map[string]interface{} {
	result := make(map[string]interface{})
	for _, item := range arr {
		if m, ok := item.(map[string]interface{}); ok {
			if name, ok := m["name"].(string); ok {
				result[name] = item
			}
		}
	}
	return result
}

// formatSimpleValue formats only simple scalar values, returning empty for complex types
func formatSimpleValue(v interface{}) string {
	switch val := v.(type) {
	case string:
		return fmt.Sprintf("%q", val)
	case float64:
		if val == float64(int64(val)) {
			return fmt.Sprintf("%d", int64(val))
		}
		return fmt.Sprintf("%g", val)
	case bool:
		return fmt.Sprintf("%t", val)
	case nil:
		return "null"
	case []interface{}:
		if len(val) == 0 {
			return "[]"
		}
		// Small simple arrays can be inline
		if len(val) <= 5 && isSimpleArray(val) {
			items := make([]string, len(val))
			for i, item := range val {
				items[i] = formatSimpleValue(item)
			}
			return "[" + strings.Join(items, ", ") + "]"
		}
		return ""
	case map[string]interface{}:
		return ""
	default:
		data, _ := json.Marshal(v)
		return string(data)
	}
}

func isSimpleArray(arr []interface{}) bool {
	for _, item := range arr {
		switch item.(type) {
		case map[string]interface{}, []interface{}:
			return false
		}
	}
	return true
}

func deepEqual(a, b interface{}) bool {
	return reflect.DeepEqual(a, b)
}

type DiffSummary struct {
	Creates   int
	Updates   int
	Deletes   int
	Recreates int
	Noops     int
}

type SSMParamChange struct {
	Name    string
	Action  string
	OldHash string
	NewHash string
}

func (r *Renderer) RenderSSMParamChanges(changes []SSMParamChange) {
	if len(changes) == 0 {
		return
	}

	fmt.Fprintln(r.out)
	r.hdrColor.Fprintln(r.out, "SSM Parameters:")

	for _, c := range changes {
		switch c.Action {
		case "create":
			r.addColor.Fprintf(r.out, "  + %s (hash: %s)\n", c.Name, c.NewHash)
		case "update":
			r.actionColor.Fprintf(r.out, "  ~ %s (hash: %s → %s)\n", c.Name, c.OldHash, c.NewHash)
		case "delete":
			r.delColor.Fprintf(r.out, "  - %s (orphaned)\n", c.Name)
		}
	}
}

func (r *Renderer) RenderSummary(summary DiffSummary, manifestName string) {
	r.hdrColor.Fprintf(r.out, "Planned changes summary")
	if manifestName != "" {
		r.hdrColor.Fprintf(r.out, " for %q", manifestName)
	}
	r.hdrColor.Fprintln(r.out, ":")

	if summary.Creates > 0 {
		r.addColor.Fprintf(r.out, "- create: %d resources\n", summary.Creates)
	}
	if summary.Updates > 0 {
		r.actionColor.Fprintf(r.out, "- update: %d resources\n", summary.Updates)
	}
	if summary.Recreates > 0 {
		r.actionColor.Fprintf(r.out, "- recreate: %d resources\n", summary.Recreates)
	}
	if summary.Deletes > 0 {
		r.delColor.Fprintf(r.out, "- delete: %d resources\n", summary.Deletes)
	}

	total := summary.Creates + summary.Updates + summary.Recreates + summary.Deletes
	if total == 0 {
		r.ctxColor.Fprintln(r.out, "No changes detected.")
	}
}
