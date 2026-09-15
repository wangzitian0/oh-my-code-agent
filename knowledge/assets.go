// Package knowledgeassets embeds the reviewed host packs shipped by OMCA.
package knowledgeassets

import "embed"

// Files contains the canonical pack JSON under hosts/ at build time.
//
//go:embed hosts/*/*/*/manifest.json
var Files embed.FS
