//go:build !embedprobe

package main

import "io/fs"

// 未使用 embedprobe 构建标签时（开发用 go run），不内嵌任何资源。
func embeddedProbe() ([]byte, string) { return nil, "" }

func embeddedDocs() fs.FS { return nil }
