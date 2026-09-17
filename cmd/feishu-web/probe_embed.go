package main

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"sync"
)

var errNoEmbeddedProbe = errors.New("未内嵌 probe 程序")

var (
	embeddedProbeOnce sync.Once
	embeddedProbePath string
	embeddedProbeErr  error
)

// writeProbeToTemp 把 probe 内容写到临时目录并赋予可执行权限。
func writeProbeToTemp(data []byte, name string) (string, error) {
	dir, err := os.MkdirTemp("", "feishu-probe-*")
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, data, 0755); err != nil {
		return "", err
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(path, 0755); err != nil {
			return "", err
		}
	}
	return path, nil
}

// extractEmbeddedProbe 首次调用时释放内嵌 probe，并缓存路径。
func extractEmbeddedProbe() (string, error) {
	embeddedProbeOnce.Do(func() {
		data, name := embeddedProbe()
		if len(data) == 0 {
			embeddedProbeErr = errNoEmbeddedProbe
			return
		}
		embeddedProbePath, embeddedProbeErr = writeProbeToTemp(data, name)
	})
	return embeddedProbePath, embeddedProbeErr
}
