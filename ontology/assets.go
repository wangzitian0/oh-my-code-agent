// Package ontologyassets embeds the reviewed concept declarations shipped by OMCA.
package ontologyassets

import "embed"

// Files contains the canonical concept JSON under concepts/ at build time.
//
//go:embed concepts/*.json
var Files embed.FS
