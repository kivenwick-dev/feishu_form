package main

import (
	"os"
	"path/filepath"
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
	if info.Mode().Perm()&0111 == 0 {
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
