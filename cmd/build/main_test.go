package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestParseTargets(t *testing.T) {
	targets, err := parseTargets("windows/amd64, linux/arm64 ,darwin/arm64")
	if err != nil {
		t.Fatal(err)
	}
	want := []target{{"windows", "amd64"}, {"linux", "arm64"}, {"darwin", "arm64"}}
	if !reflect.DeepEqual(targets, want) {
		t.Fatalf("targets = %#v", targets)
	}
	if _, err := parseTargets("windows"); err == nil {
		t.Fatal("缺少 goarch 应报错")
	}
	if _, err := parseTargets("   "); err == nil {
		t.Fatal("空列表应报错")
	}
	if _, err := parseTargets("windows/amd64/extra"); err == nil {
		t.Fatal("goarch 含多余 / 应报错")
	}
}

func TestTargetNames(t *testing.T) {
	win := target{"windows", "amd64"}
	if win.probeName() != "feishu-probe.exe" || win.webName() != "feishu-web.exe" || win.String() != "windows-amd64" {
		t.Fatalf("windows 命名错误：%#v", win)
	}
	linux := target{"linux", "amd64"}
	if linux.probeName() != "feishu-probe" || linux.webName() != "feishu-web" || linux.String() != "linux-amd64" {
		t.Fatalf("linux 命名错误：%#v", linux)
	}
}

func TestBuildEnv(t *testing.T) {
	t.Setenv("GOOS", "plan9")
	t.Setenv("GOARCH", "mips")
	t.Setenv("CGO_ENABLED", "1")
	t.Setenv("BUILD_TEST_KEEP", "yes")

	env := buildEnv(target{"linux", "arm64"})
	counts := map[string]int{}
	values := map[string]string{}
	for _, kv := range env {
		key, value, ok := strings.Cut(kv, "=")
		if !ok {
			continue
		}
		counts[key]++
		values[key] = value
	}
	for key, want := range map[string]string{
		"GOOS":            "linux",
		"GOARCH":          "arm64",
		"CGO_ENABLED":     "0",
		"BUILD_TEST_KEEP": "yes",
	} {
		if values[key] != want {
			t.Errorf("%s = %q, 期望 %q", key, values[key], want)
		}
		if counts[key] != 1 {
			t.Errorf("%s 出现 %d 次，期望 1 次", key, counts[key])
		}
	}
}

func TestStageDocs(t *testing.T) {
	root := t.TempDir()
	docs := filepath.Join(root, "docs")
	for _, rel := range []string{"使用说明.md", ".DS_Store", filepath.Join("superpowers", "spec.md")} {
		full := filepath.Join(docs, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	if err := stageDocs(root); err != nil {
		t.Fatal(err)
	}

	embedded := filepath.Join(root, "cmd", "feishu-web", "embed", "docs")
	if _, err := os.Stat(filepath.Join(embedded, "使用说明.md")); err != nil {
		t.Errorf("使用说明.md 应被暂存：%v", err)
	}
	if _, err := os.Stat(filepath.Join(embedded, ".DS_Store")); !os.IsNotExist(err) {
		t.Errorf(".DS_Store 不应被暂存，err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(embedded, "superpowers")); !os.IsNotExist(err) {
		t.Errorf("superpowers 目录不应被暂存，err = %v", err)
	}
}
