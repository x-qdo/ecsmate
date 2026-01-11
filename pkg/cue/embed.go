package schema

import "embed"

//go:embed *.cue
var EmbeddedSchema embed.FS
