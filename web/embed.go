// Package web embeds the user interface and the print templates into the executable.
package web

import "embed"

// Static holds index.html, CSS, JavaScript modules and images.
//
//go:embed all:static
var Static embed.FS

// Templates holds the server-side print templates.
//
//go:embed templates/*.html
var Templates embed.FS
