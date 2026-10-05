// Package packs embeds the fictional demo business packs into the aios binary.
package packs

import "embed"

// FS holds auto.json and homecare.json.
//
//go:embed *.json
var FS embed.FS
