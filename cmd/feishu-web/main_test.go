package main

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestWriteProbeToTemp(t *testing.T) {
	content := []byte("#!/bin/sh\necho hi\n")
	path, err := writeProbeToTemp(content, "feishu-probe")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(filepath.Dir(path))

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0111 == 0 {
		t.Fatalf("释放的 probe 没有可执行权限：%v", info.Mode().Perm())
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(content) {
		t.Fatalf("内容不一致：%q", data)
	}
}

func TestExtractEmbeddedProbeWithoutEmbed(t *testing.T) {
	_, err := extractEmbeddedProbe()
	if !errors.Is(err, errNoEmbeddedProbe) {
		t.Fatalf("无内嵌时应返回 errNoEmbeddedProbe，got %v", err)
	}
}

func probeTempDirs(t *testing.T) map[string]bool {
	t.Helper()
	entries, err := os.ReadDir(os.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	dirs := map[string]bool{}
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), "feishu-probe-") {
			dirs[e.Name()] = true
		}
	}
	return dirs
}

func TestWriteProbeToTempCleansUpOnFailure(t *testing.T) {
	before := probeTempDirs(t)
	if _, err := writeProbeToTemp([]byte("x"), filepath.Join("missing-subdir", "feishu-probe")); err == nil {
		t.Fatal("期望写入失败")
	}
	after := probeTempDirs(t)
	if len(after) > len(before) {
		t.Fatalf("失败路径残留临时目录：%v", after)
	}
}
