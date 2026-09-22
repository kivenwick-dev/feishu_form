package main

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"testing/fstest"
	"time"
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

func TestArchiveCommandFallsBackToGoRun(t *testing.T) {
	cmd, err := archiveCommand([]string{"--archive"})
	if err != nil {
		t.Fatalf("无内嵌 probe 时应回退 go run，err = %v", err)
	}
	if filepath.Base(cmd.Path) != "go" {
		t.Fatalf("cmd.Path = %q，期望 go", cmd.Path)
	}
	want := []string{"run", "./cmd/feishu-probe", "--archive"}
	if !reflect.DeepEqual(cmd.Args[1:], want) {
		t.Fatalf("cmd.Args = %#v，期望 %#v", cmd.Args[1:], want)
	}
}

func TestListDocs(t *testing.T) {
	source := fstest.MapFS{
		"使用说明.md":        &fstest.MapFile{Data: []byte("a")},
		"sub/ignored.md": &fstest.MapFile{Data: []byte("b")},
	}
	names, err := listDocs(source)
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 1 || names[0] != "使用说明.md" {
		t.Fatalf("names = %#v", names)
	}
}

func TestReadDoc(t *testing.T) {
	source := fstest.MapFS{"使用说明.md": &fstest.MapFile{Data: []byte("hello")}}
	got, err := readDoc(source, "使用说明.md")
	if err != nil || string(got) != "hello" {
		t.Fatalf("readDoc = %q, %v", got, err)
	}
	if _, err := readDoc(source, "missing.md"); err == nil {
		t.Fatal("缺失文件应返回错误")
	}
	if _, err := readDoc(source, ".."); err == nil {
		t.Fatal("非法文件名应返回错误")
	}
}

func TestReadDocSanitizesToBasename(t *testing.T) {
	source := fstest.MapFS{
		"secret": &fstest.MapFile{Data: []byte("inside")},
		"hosts":  &fstest.MapFile{Data: []byte("inner-hosts")},
	}
	// Base("../secret") == "secret"：归一化后读取根内的 secret，而不是外层文件
	got, err := readDoc(source, "../secret")
	if err != nil || string(got) != "inside" {
		t.Fatalf("readDoc(../secret) = %q, %v，want \"inside\"", got, err)
	}
	// Base("/etc/hosts") == "hosts"：同样落在根内
	got, err = readDoc(source, "/etc/hosts")
	if err != nil || string(got) != "inner-hosts" {
		t.Fatalf("readDoc(/etc/hosts) = %q, %v，want \"inner-hosts\"", got, err)
	}
}

func TestListDocsEmptyReturnsNonNil(t *testing.T) {
	source := fstest.MapFS{"sub/only.md": &fstest.MapFile{Data: []byte("x")}}
	names, err := listDocs(source)
	if err != nil {
		t.Fatal(err)
	}
	if names == nil {
		t.Fatal("应返回非 nil 空切片，避免 JSON 输出 null")
	}
	if len(names) != 0 {
		t.Fatalf("names = %#v", names)
	}
}

func TestEnvDuration(t *testing.T) {
	t.Setenv("REIMBURSEMENT_TEST_DURATION", "30m")
	if got := envDuration("REIMBURSEMENT_TEST_DURATION", time.Hour); got != 30*time.Minute {
		t.Fatalf("envDuration = %s，期望 30m", got)
	}

	t.Setenv("REIMBURSEMENT_TEST_DURATION", "bad")
	if got := envDuration("REIMBURSEMENT_TEST_DURATION", time.Hour); got != time.Hour {
		t.Fatalf("非法 duration 应回退默认值，got %s", got)
	}
}

func TestCloudDefaultsKeepOutputForTenMinutes(t *testing.T) {
	t.Setenv("REIMBURSEMENT_OUTPUT_RETENTION", "")
	t.Setenv("REIMBURSEMENT_OUTPUT_CLEANUP_INTERVAL", "")
	if got := outputRetention(); got != 10*time.Minute {
		t.Fatalf("outputRetention = %s，期望 10m", got)
	}
	if got := outputCleanupInterval(); got != time.Minute {
		t.Fatalf("outputCleanupInterval = %s，期望 1m", got)
	}
}

func TestCleanupExpiredLogs(t *testing.T) {
	job.Lock()
	job.Running = false
	job.Logs = []string{"完成"}
	job.logsExpireAt = time.Now().Add(-time.Second)
	job.Unlock()

	cleanupExpiredLogs(time.Now())

	job.Lock()
	defer job.Unlock()
	if job.Logs != nil {
		t.Fatalf("过期日志应被清空，got %#v", job.Logs)
	}
	if !job.logsExpireAt.IsZero() {
		t.Fatalf("过期时间应被清空，got %s", job.logsExpireAt)
	}
}

func TestPageLocksCloudOutputDirectory(t *testing.T) {
	if !strings.Contains(page, `id="out" value="{{.DefaultOut}}" readonly`) {
		t.Fatal("输出目录输入框应锁定")
	}
	if strings.Contains(page, `onclick="chooseOut()"`) {
		t.Fatal("云服务页面不应提供输出目录选择")
	}
	if !strings.Contains(page, "云服务存档") {
		t.Fatal("页面应标明云服务存档")
	}
}

func TestCleanupOutputDirOnce(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	oldFile := filepath.Join(dir, "old.zip")
	freshFile := filepath.Join(dir, "fresh.zip")
	oldDir := filepath.Join(dir, "old-result")
	freshDir := filepath.Join(dir, "fresh-result")

	for _, path := range []string{oldDir, freshDir} {
		if err := os.Mkdir(path, 0755); err != nil {
			t.Fatal(err)
		}
	}
	for _, path := range []string{oldFile, freshFile, filepath.Join(oldDir, "data.txt"), filepath.Join(freshDir, "data.txt")} {
		if err := os.WriteFile(path, []byte("x"), 0600); err != nil {
			t.Fatal(err)
		}
	}

	oldTime := now.Add(-25 * time.Hour)
	freshTime := now.Add(-2 * time.Hour)
	if err := os.Chtimes(oldFile, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(oldDir, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(freshFile, freshTime, freshTime); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(freshDir, freshTime, freshTime); err != nil {
		t.Fatal(err)
	}

	cleanupOutputDirOnce(dir, 24*time.Hour, now)

	if _, err := os.Stat(oldFile); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("过期文件应被删除，stat err = %v", err)
	}
	if _, err := os.Stat(oldDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("过期目录应被删除，stat err = %v", err)
	}
	if _, err := os.Stat(freshFile); err != nil {
		t.Fatalf("未过期文件不应删除：%v", err)
	}
	if _, err := os.Stat(freshDir); err != nil {
		t.Fatalf("未过期目录不应删除：%v", err)
	}
}

func TestCleanupDownloadedArchive(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "result.zip")
	root := filepath.Join(dir, "result")
	if err := os.WriteFile(archive, []byte("zip"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(root, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "data.txt"), []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}

	job.Lock()
	job.ArchivePath = archive
	job.DownloadURL = "/api/download"
	job.Logs = []string{"完成，可下载 ZIP：result.zip"}
	job.Unlock()

	cleanupDownloadedArchive(archive)

	if _, err := os.Stat(archive); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("下载后的 ZIP 应被删除，stat err = %v", err)
	}
	if _, err := os.Stat(root); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("下载后的结果目录应被删除，stat err = %v", err)
	}
	job.Lock()
	defer job.Unlock()
	if job.ArchivePath != "" || job.DownloadURL != "" {
		t.Fatalf("下载后应清空任务下载状态：ArchivePath=%q DownloadURL=%q", job.ArchivePath, job.DownloadURL)
	}
	if got := job.Logs[len(job.Logs)-1]; got != "ZIP 已下载，服务器临时文件已清理" {
		t.Fatalf("最后一条日志 = %q", got)
	}
}
