package main

import (
	"errors"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// docsSource 优先使用内嵌 docs，否则回退项目根目录下的 docs/。
func docsSource() fs.FS {
	if embedded := embeddedDocs(); embedded != nil {
		return embedded
	}
	return os.DirFS(filepath.Join(projectRoot(), "docs"))
}

// listDocs 返回顶层文件名（忽略子目录），按名称排序。
func listDocs(source fs.FS) ([]string, error) {
	entries, err := fs.ReadDir(source, ".")
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	return names, nil
}

// readDoc 读取单个说明文件；文件名经过 Base 处理，拒绝路径穿越。
func readDoc(source fs.FS, name string) ([]byte, error) {
	clean := path.Base(strings.TrimSpace(name))
	if clean == "." || clean == "" {
		return nil, errors.New("缺少文件名")
	}
	return fs.ReadFile(source, clean)
}
