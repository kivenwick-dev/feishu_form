package main

import (
	"reflect"
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
