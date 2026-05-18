package config

import (
	"fmt"
	"io"
	"strings"
)

// ManifestError represents a manifest loading or validation error
// with user-friendly formatting for CLI output.
type ManifestError struct {
	Phase   string // e.g., "load", "build", "validate"
	Summary string // single-line summary
	Details []string
	Hint    string
}

func (e *ManifestError) Error() string {
	return e.Summary
}

// Print writes a formatted error message to the given writer.
// If useColor is true, it uses ANSI colors.
func (e *ManifestError) Print(w io.Writer, useColor bool) {
	red := ""
	yellow := ""
	cyan := ""
	dim := ""
	reset := ""
	bold := ""

	if useColor {
		red = "\033[31m"
		yellow = "\033[33m"
		cyan = "\033[36m"
		dim = "\033[2m"
		reset = "\033[0m"
		bold = "\033[1m"
	}

	_, _ = fmt.Fprintf(w, "\n%s%sManifest Error%s%s\n", red, bold, reset, reset)
	_, _ = fmt.Fprintf(w, "%s%s%s\n\n", dim, strings.Repeat("─", 50), reset)

	_, _ = fmt.Fprintf(w, "%s%s%s\n", red, e.Summary, reset)

	if len(e.Details) > 0 {
		_, _ = fmt.Fprintf(w, "\n%sDetails:%s\n", yellow, reset)
		for _, detail := range e.Details {
			// Indent each line of the detail
			lines := strings.Split(detail, "\n")
			for _, line := range lines {
				if line != "" {
					_, _ = fmt.Fprintf(w, "  %s%s%s\n", cyan, line, reset)
				}
			}
		}
	}

	if e.Hint != "" {
		_, _ = fmt.Fprintf(w, "\n%sHint:%s %s\n", yellow, reset, e.Hint)
	}

	_, _ = fmt.Fprintln(w)
}

// NewCUEBuildError creates a ManifestError for CUE build failures.
func NewCUEBuildError(summary string, details []string) *ManifestError {
	return &ManifestError{
		Phase:   "build",
		Summary: summary,
		Details: details,
		Hint:    "Check that all required fields are set and types match the schema.",
	}
}

// NewCUEValidationError creates a ManifestError for CUE validation failures.
func NewCUEValidationError(summary string, details []string) *ManifestError {
	return &ManifestError{
		Phase:   "validate",
		Summary: summary,
		Details: details,
		Hint:    "Ensure your manifest conforms to the schema.#Manifest type.",
	}
}

// NewCUELoadError creates a ManifestError for CUE file loading failures.
func NewCUELoadError(summary string, details []string) *ManifestError {
	return &ManifestError{
		Phase:   "load",
		Summary: summary,
		Details: details,
		Hint:    "Check CUE syntax and file paths.",
	}
}
