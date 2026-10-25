// Package web contains the embedded administration UI and built-in locales.
package web

import "embed"

//go:embed static locales
var Files embed.FS
