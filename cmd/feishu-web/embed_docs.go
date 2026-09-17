//go:build embedprobe

package main

import (
	"embed"
	"io/fs"
)

//go:embed all:embed/docs
var docsFS embed.FS

func embeddedDocs() fs.FS {
	sub, err := fs.Sub(docsFS, "embed/docs")
	if err != nil {
		return nil
	}
	return sub
}
