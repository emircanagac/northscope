package ui

import "embed"

// Dist contains the production Vite build. Keeping this file inside ui/ lets
// Go embed the real ui/dist directory while cmd/northscope remains under cmd/.
//
//go:embed dist
var Dist embed.FS
