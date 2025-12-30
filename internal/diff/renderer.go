package diff

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/fatih/color"
	"github.com/sergi/go-diff/diffmatchpatch"
)

type Renderer struct {
	out      io.Writer
	noColor  bool
	addColor *color.Color
	delColor *color.Color
	hdrColor *color.Color
	ctxColor *color.Color
}

func NewRenderer(out io.Writer, noColor bool) *Renderer {
	r := &Renderer{
		out:      out,
		noColor:  noColor,
		addColor: color.New(color.FgGreen),
		delColor: color.New(color.FgRed),
		hdrColor: color.New(color.FgCyan, color.Bold),
		ctxColor: color.New(color.FgWhite),
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
	Type            DiffType
	Name            string
	Resource        string
	Current         interface{}
	Desired         interface{}
	Details         string
	RecreateReasons []string // For RECREATE: reasons why recreation is needed
}

func (r *Renderer) RenderDiff(entries []DiffEntry) {
	if len(entries) == 0 {
		fmt.Fprintln(r.out, "No changes detected.")
		return
	}

	grouped := make(map[string][]DiffEntry)
	for _, e := range entries {
		grouped[e.Resource] = append(grouped[e.Resource], e)
	}

	resourceOrder := []string{"TaskDefinition", "Service", "ScheduledTask"}
	for _, resource := range resourceOrder {
		if entries, ok := grouped[resource]; ok {
			r.renderResourceGroup(resource, entries)
		}
	}
}

func (r *Renderer) renderResourceGroup(resource string, entries []DiffEntry) {
	r.hdrColor.Fprintf(r.out, "\n%s:\n", resource+"s")

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name < entries[j].Name
	})

	for _, entry := range entries {
		r.renderEntry(entry)
	}
}

func (r *Renderer) renderEntry(entry DiffEntry) {
	recreateColor := color.New(color.FgYellow)

	switch entry.Type {
	case DiffTypeCreate:
		r.addColor.Fprintf(r.out, "  + %s\n", entry.Name)
		if entry.Details != "" {
			r.renderDetails(entry.Details, "    ")
		} else if entry.Desired != nil {
			r.renderJSON(entry.Desired, "    ", true)
		}

	case DiffTypeUpdate:
		r.hdrColor.Fprintf(r.out, "  ~ %s\n", entry.Name)
		if entry.Current != nil && entry.Desired != nil {
			r.renderUnifiedDiff(entry.Current, entry.Desired, "    ")
		} else if entry.Details != "" {
			r.renderDetails(entry.Details, "    ")
		}

	case DiffTypeRecreate:
		recreateColor.Fprintf(r.out, "  -/+ %s (must be recreated)\n", entry.Name)
		if len(entry.RecreateReasons) > 0 {
			recreateColor.Fprintf(r.out, "    # Reasons for recreation:\n")
			for _, reason := range entry.RecreateReasons {
				recreateColor.Fprintf(r.out, "    #   - %s\n", reason)
			}
		}
		if entry.Current != nil && entry.Desired != nil {
			r.renderUnifiedDiff(entry.Current, entry.Desired, "    ")
		}

	case DiffTypeDelete:
		r.delColor.Fprintf(r.out, "  - %s\n", entry.Name)
		if entry.Details != "" {
			r.renderDetails(entry.Details, "    ")
		} else if entry.Current != nil {
			r.renderJSON(entry.Current, "    ", false)
		}

	case DiffTypeNoop:
		r.ctxColor.Fprintf(r.out, "  = %s (no changes)\n", entry.Name)
	}
}

func (r *Renderer) renderDetails(details, prefix string) {
	for _, line := range strings.Split(details, "\n") {
		if line != "" {
			fmt.Fprintf(r.out, "%s%s\n", prefix, line)
		}
	}
}

func (r *Renderer) renderJSON(v interface{}, prefix string, isAdd bool) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return
	}

	c := r.ctxColor
	if isAdd {
		c = r.addColor
	}

	for _, line := range strings.Split(string(data), "\n") {
		c.Fprintf(r.out, "%s%s\n", prefix, line)
	}
}

func (r *Renderer) renderUnifiedDiff(current, desired interface{}, prefix string) {
	currentJSON, err := json.MarshalIndent(current, "", "  ")
	if err != nil {
		return
	}

	desiredJSON, err := json.MarshalIndent(desired, "", "  ")
	if err != nil {
		return
	}

	dmp := diffmatchpatch.New()
	diffs := dmp.DiffMain(string(currentJSON), string(desiredJSON), false)
	diffs = dmp.DiffCleanupSemantic(diffs)

	lines := r.diffToUnifiedLines(diffs)

	for _, line := range lines {
		if strings.HasPrefix(line, "+") {
			r.addColor.Fprintf(r.out, "%s%s\n", prefix, line)
		} else if strings.HasPrefix(line, "-") {
			r.delColor.Fprintf(r.out, "%s%s\n", prefix, line)
		} else {
			r.ctxColor.Fprintf(r.out, "%s%s\n", prefix, line)
		}
	}
}

func (r *Renderer) diffToUnifiedLines(diffs []diffmatchpatch.Diff) []string {
	var result []string
	var currentLine strings.Builder
	var lineType byte = ' '

	flushLine := func() {
		if currentLine.Len() > 0 {
			result = append(result, string(lineType)+currentLine.String())
			currentLine.Reset()
			lineType = ' '
		}
	}

	for _, d := range diffs {
		lines := strings.Split(d.Text, "\n")
		for i, line := range lines {
			if i > 0 {
				flushLine()
			}

			switch d.Type {
			case diffmatchpatch.DiffInsert:
				if lineType == ' ' {
					flushLine()
				}
				lineType = '+'
				currentLine.WriteString(line)
			case diffmatchpatch.DiffDelete:
				if lineType == ' ' {
					flushLine()
				}
				lineType = '-'
				currentLine.WriteString(line)
			case diffmatchpatch.DiffEqual:
				if lineType != ' ' {
					flushLine()
				}
				currentLine.WriteString(line)
			}
		}
	}

	flushLine()
	return result
}

type DiffSummary struct {
	Creates   int
	Updates   int
	Deletes   int
	Recreates int
	Noops     int
}

func (r *Renderer) RenderSummary(summary DiffSummary) {
	fmt.Fprintln(r.out)
	r.hdrColor.Fprint(r.out, "Summary: ")

	parts := []string{}
	if summary.Creates > 0 {
		parts = append(parts, r.addColor.Sprintf("%d to create", summary.Creates))
	}
	if summary.Updates > 0 {
		parts = append(parts, r.hdrColor.Sprintf("%d to update", summary.Updates))
	}
	if summary.Recreates > 0 {
		parts = append(parts, color.New(color.FgYellow).Sprintf("%d to recreate", summary.Recreates))
	}
	if summary.Deletes > 0 {
		parts = append(parts, r.delColor.Sprintf("%d to delete", summary.Deletes))
	}
	if summary.Noops > 0 {
		parts = append(parts, r.ctxColor.Sprintf("%d unchanged", summary.Noops))
	}

	if len(parts) == 0 {
		fmt.Fprintln(r.out, "No changes.")
	} else {
		fmt.Fprintln(r.out, strings.Join(parts, ", "))
	}
}
