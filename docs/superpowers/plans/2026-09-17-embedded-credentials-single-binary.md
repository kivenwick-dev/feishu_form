# 内置凭据与单文件跨平台打包 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 让打包出的单个可执行文件内置飞书应用凭据（轻量混淆），在 Windows / macOS / Ubuntu 上开箱即用，无需任何配置。

**Architecture:** 新增 `internal/credential` 做 XOR+base64 可逆混淆；probe 读取“参数 > 环境变量 > 内置值”；新增 `cmd/build` 交叉编译命令，把混淆后的凭据用 `-ldflags -X` 注入 probe，再把 probe 与 `docs/` 通过 `go:embed` 打进 `feishu-web`，得到单文件；`feishu-web` 运行时按需把内嵌 probe 释放到临时目录执行。

**Tech Stack:** Go 1.26，标准库 `encoding/base64`、`embed`、`io/fs`；无新增第三方依赖。

参考设计：`docs/superpowers/specs/2026-09-17-embedded-credentials-single-binary-design.md`

---

## 文件结构

| 文件 | 职责 |
| --- | --- |
| `internal/credential/credential.go`（新建） | 凭据混淆/还原 |
| `internal/credential/credential_test.go`（新建） | 混淆往返测试 |
| `cmd/feishu-probe/main.go`（改） | 注入变量 + `resolveCredential` + 优先级 |
| `cmd/feishu-probe/main_test.go`（改） | `resolveCredential` 测试 |
| `cmd/feishu-web/embed_stub.go`（新建） | 无内嵌时的空实现 |
| `cmd/feishu-web/embed_docs.go`（新建） | 内嵌 docs（`embedprobe`） |
| `cmd/feishu-web/embed_darwin.go`（新建） | 内嵌 darwin probe |
| `cmd/feishu-web/embed_linux.go`（新建） | 内嵌 linux probe |
| `cmd/feishu-web/embed_windows.go`（新建） | 内嵌 windows probe |
| `cmd/feishu-web/probe_embed.go`（新建） | 释放内嵌 probe、命令选择 |
| `cmd/feishu-web/docs.go`（新建） | docs 读写与来源选择 |
| `cmd/feishu-web/main.go`（改） | handler 改用 docs.go；`chooseFolder` 加 zenity |
| `cmd/feishu-web/main_test.go`（新建） | 释放 probe / docs 测试 |
| `cmd/build/main.go`（新建） | 跨平台构建命令 |
| `cmd/build/main_test.go`（新建） | 目标解析测试 |
| `windows/start.bat`、`windows/start.ps1`、`windows/build.ps1`（改） | 去凭据交互、转到新构建命令 |
| `.gitignore`（改） | 忽略 embed 暂存与 dist |
| `docs/使用说明.md`、`README.md`、`windows/README.md`（改） | 文档 |

---

## Task 1: 凭据混淆包 `internal/credential`

**Files:**
- Create: `internal/credential/credential.go`
- Test: `internal/credential/credential_test.go`

- [ ] **Step 1: 写失败测试**

创建 `internal/credential/credential_test.go`：

```go
package credential

import (
	"strings"
	"testing"
)

func TestEncodeDecodeRoundTrip(t *testing.T) {
	values := []string{
		"cli_demo_app_id",
		"demo-app-secret-value",
		"含中文/符号!@#",
	}
	for _, want := range values {
		blob := Encode(want)
		if blob == "" {
			t.Fatalf("Encode(%q) 返回空串", want)
		}
		if strings.ContainsAny(blob, "+/=") {
			t.Fatalf("Encode(%q) 含 URL 不安全字符：%q", want, blob)
		}
		got, err := Decode(blob)
		if err != nil {
			t.Fatalf("Decode(%q) 出错：%v", blob, err)
		}
		if got != want {
			t.Fatalf("往返不一致：got %q want %q", got, want)
		}
	}
}

func TestEncodeDecodeEmpty(t *testing.T) {
	if got := Encode(""); got != "" {
		t.Fatalf("Encode(\"\") = %q，want \"\"", got)
	}
	got, err := Decode("")
	if err != nil || got != "" {
		t.Fatalf("Decode(\"\") = %q, %v，want \"\", nil", got, err)
	}
}

func TestDecodeInvalid(t *testing.T) {
	if _, err := Decode("!!!not-base64!!!"); err == nil {
		t.Fatal("非法 base64 应返回错误")
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./internal/credential/ -v`
Expected: FAIL / build failed，提示 `undefined: Encode`

- [ ] **Step 3: 写最小实现**

创建 `internal/credential/credential.go`：

```go
// Package credential 对内置的飞书应用凭据做可逆混淆。
// 注意：这不是安全加密，只能避免二进制中出现明文，仅用于内部可信分发。
package credential

import (
	"encoding/base64"
	"fmt"
)

const xorKey = "reimb-cred-2026!"

// Encode 把明文混淆成 URL 安全字符串；空串返回空串。
func Encode(plain string) string {
	if plain == "" {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(xor([]byte(plain)))
}

// Decode 还原 Encode 的结果；空串返回空串，非法输入返回错误。
func Decode(blob string) (string, error) {
	if blob == "" {
		return "", nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(blob)
	if err != nil {
		return "", fmt.Errorf("凭据混淆串无法解码：%w", err)
	}
	return string(xor(raw)), nil
}

func xor(data []byte) []byte {
	out := make([]byte, len(data))
	for i, b := range data {
		out[i] = b ^ xorKey[i%len(xorKey)]
	}
	return out
}
```

- [ ] **Step 4: 运行测试确认通过**

Run: `go test ./internal/credential/ -v`
Expected: PASS（3 个测试）

- [ ] **Step 5: 提交**

```bash
git add internal/credential
git commit -m "feat: add reversible credential obfuscation package"
```

---

## Task 2: probe 凭据解析优先级

**Files:**
- Modify: `cmd/feishu-probe/main.go`
- Test: `cmd/feishu-probe/main_test.go`

- [ ] **Step 1: 写失败测试**

在 `cmd/feishu-probe/main_test.go` 末尾追加（并把 `"reimbursement-archiver/internal/credential"` 加进 import 块）：

```go
func TestResolveCredential(t *testing.T) {
	blob := credential.Encode("embedded-id")
	if got := resolveCredential("env-id", blob); got != "env-id" {
		t.Fatalf("环境变量应优先：got %q", got)
	}
	if got := resolveCredential("", blob); got != "embedded-id" {
		t.Fatalf("应回退内置值：got %q", got)
	}
	if got := resolveCredential("   ", blob); got != "embedded-id" {
		t.Fatalf("空白环境变量应回退内置值：got %q", got)
	}
	if got := resolveCredential("", ""); got != "" {
		t.Fatalf("都为空应返回空：got %q", got)
	}
	if got := resolveCredential("", "!!!bad!!!"); got != "" {
		t.Fatalf("非法混淆串应返回空：got %q", got)
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./cmd/feishu-probe/ -run TestResolveCredential -v`
Expected: FAIL / build failed，提示 `undefined: resolveCredential`

- [ ] **Step 3: 添加注入变量与解析函数**

在 `cmd/feishu-probe/main.go` 的 import 块中，`"regexp"` 之后新增一行：

```go
	"reimbursement-archiver/internal/credential"
```

在 `const screenshotFolder = "截图"`（约 82 行）之后新增：

```go
// 构建时通过 -ldflags "-X main.embeddedAppIDBlob=..." 注入的混淆凭据。
var (
	embeddedAppIDBlob     string
	embeddedAppSecretBlob string
)

// resolveCredential 按“环境变量优先、内置混淆值兜底”的顺序解析应用凭据。
func resolveCredential(envValue, blob string) string {
	if v := strings.TrimSpace(envValue); v != "" {
		return v
	}
	decoded, err := credential.Decode(blob)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(decoded)
}
```

- [ ] **Step 4: 接入 flag 默认值与错误文案**

修改 `main()`（约 1100-1114 行）：

```go
	appID := flag.String("app-id", resolveCredential(os.Getenv("FEISHU_APP_ID"), embeddedAppIDBlob), "飞书 App ID")
	appSecret := flag.String("app-secret", resolveCredential(os.Getenv("FEISHU_APP_SECRET"), embeddedAppSecretBlob), "飞书 App Secret")
```

以及：

```go
	if *urlFlag == "" || *appID == "" || *appSecret == "" {
		fmt.Fprintln(os.Stderr, "请提供表格链接，并通过内置凭据、FEISHU_APP_ID / FEISHU_APP_SECRET 环境变量或 --app-id / --app-secret 参数提供应用凭据。")
		os.Exit(2)
	}
```

- [ ] **Step 5: 运行测试确认通过**

Run: `go test ./cmd/feishu-probe/ -v`
Expected: PASS（含新增 `TestResolveCredential`，原有测试不回归）

- [ ] **Step 6: 提交**

```bash
git add cmd/feishu-probe/main.go cmd/feishu-probe/main_test.go
git commit -m "feat: resolve probe credentials from embedded value with env override"
```

---

## Task 3: web 内嵌 probe 与命令选择

**Files:**
- Create: `cmd/feishu-web/embed_stub.go`
- Create: `cmd/feishu-web/embed_darwin.go`, `cmd/feishu-web/embed_linux.go`, `cmd/feishu-web/embed_windows.go`
- Create: `cmd/feishu-web/probe_embed.go`
- Modify: `cmd/feishu-web/main.go`（`archiveCommand`）
- Test: `cmd/feishu-web/main_test.go`

- [ ] **Step 1: 写失败测试**

创建 `cmd/feishu-web/main_test.go`：

```go
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
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./cmd/feishu-web/ -run TestWriteProbeToTemp -v`
Expected: FAIL / build failed，提示 `undefined: writeProbeToTemp`

- [ ] **Step 3: 新建空实现 stub**

创建 `cmd/feishu-web/embed_stub.go`：

```go
//go:build !embedprobe

package main

import "io/fs"

// 未使用 embedprobe 构建标签时（开发用 go run），不内嵌任何资源。
func embeddedProbe() ([]byte, string) { return nil, "" }

func embeddedDocs() fs.FS { return nil }
```

- [ ] **Step 4: 新建按平台的 embed 文件**

创建 `cmd/feishu-web/embed_darwin.go`：

```go
//go:build darwin && embedprobe

package main

import _ "embed"

//go:embed embed/darwin/feishu-probe
var probeDarwin []byte

func embeddedProbe() ([]byte, string) { return probeDarwin, "feishu-probe" }
```

创建 `cmd/feishu-web/embed_linux.go`：

```go
//go:build linux && embedprobe

package main

import _ "embed"

//go:embed embed/linux/feishu-probe
var probeLinux []byte

func embeddedProbe() ([]byte, string) { return probeLinux, "feishu-probe" }
```

创建 `cmd/feishu-web/embed_windows.go`：

```go
//go:build windows && embedprobe

package main

import _ "embed"

//go:embed embed/windows/feishu-probe.exe
var probeWindows []byte

func embeddedProbe() ([]byte, string) { return probeWindows, "feishu-probe.exe" }
```

- [ ] **Step 5: 新建释放与命令选择逻辑**

创建 `cmd/feishu-web/probe_embed.go`：

```go
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
```

- [ ] **Step 6: 改造 archiveCommand**

修改 `cmd/feishu-web/main.go` 的 `archiveCommand`（文件末尾）：

```go
func archiveCommand(args []string) *exec.Cmd {
	if executable, err := os.Executable(); err == nil {
		dir := filepath.Dir(executable)
		names := []string{"feishu-probe"}
		if runtime.GOOS == "windows" {
			names = append(names, "feishu-probe.exe")
		}
		for _, name := range names {
			path := filepath.Join(dir, name)
			if info, statErr := os.Stat(path); statErr == nil && !info.IsDir() {
				return exec.Command(path, args...)
			}
		}
	}
	if path, err := extractEmbeddedProbe(); err == nil {
		return exec.Command(path, args...)
	}
	goArgs := append([]string{"run", "./cmd/feishu-probe"}, args...)
	return exec.Command("go", goArgs...)
}
```

- [ ] **Step 7: 运行测试确认通过**

Run: `go test ./cmd/feishu-web/ -v`
Expected: PASS（`TestWriteProbeToTemp`）

- [ ] **Step 8: 提交**

```bash
git add cmd/feishu-web/embed_stub.go cmd/feishu-web/embed_darwin.go cmd/feishu-web/embed_linux.go cmd/feishu-web/embed_windows.go cmd/feishu-web/probe_embed.go cmd/feishu-web/main.go cmd/feishu-web/main_test.go
git commit -m "feat: embed probe binary and extract it at runtime"
```

---

## Task 4: web 内嵌 docs

**Files:**
- Create: `cmd/feishu-web/embed_docs.go`
- Create: `cmd/feishu-web/docs.go`
- Modify: `cmd/feishu-web/main.go`（删除原 `docsHandler`/`docReadHandler`，改用 docs.go；`/docs/` 路由改用 `docsSource()`）
- Test: `cmd/feishu-web/main_test.go`

- [ ] **Step 1: 写失败测试**

在 `cmd/feishu-web/main_test.go` 末尾追加（import 增加 `"testing/fstest"`）：

```go
func TestListDocs(t *testing.T) {
	source := fstest.MapFS{
		"使用说明.md":      &fstest.MapFile{Data: []byte("a")},
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
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./cmd/feishu-web/ -run 'TestListDocs|TestReadDoc' -v`
Expected: FAIL / build failed，提示 `undefined: listDocs`

- [ ] **Step 3: 新建 docs 来源与读写**

创建 `cmd/feishu-web/docs.go`：

```go
package main

import (
	"fmt"
	"io/fs"
	"os"
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
	clean := filepath.Base(strings.TrimSpace(name))
	if clean == "." || clean == "" {
		return nil, fmt.Errorf("缺少文件名")
	}
	return fs.ReadFile(source, clean)
}
```

- [ ] **Step 4: 新建内嵌 docs 文件**

创建 `cmd/feishu-web/embed_docs.go`：

```go
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
```

- [ ] **Step 5: 替换 main.go 中的 docs handler**

删除 `cmd/feishu-web/main.go` 中原有的 `docsHandler` 与 `docReadHandler`（约 68-96 行），替换为：

```go
func docsHandler(w http.ResponseWriter, r *http.Request) {
	names, err := listDocs(docsSource())
	if err != nil || names == nil {
		names = []string{}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(names)
}

func docReadHandler(w http.ResponseWriter, r *http.Request) {
	b, err := readDoc(docsSource(), r.URL.Query().Get("name"))
	if err != nil {
		http.Error(w, "文件不存在", 404)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write(b)
}
```

把 main 中的静态路由（约 46 行）：

```go
	http.Handle("/docs/", http.StripPrefix("/docs/", http.FileServer(http.Dir(filepath.Join(root, "docs")))))
```

改为：

```go
	http.Handle("/docs/", http.StripPrefix("/docs/", http.FileServer(http.FS(docsSource()))))
```

同时删除 `main()` 开头已不再使用的：

```go
	root := projectRoot()
```

（`projectRoot()` 仍被 `docsSource()` 回退分支使用，函数本身保留。）

- [ ] **Step 6: 运行测试确认通过**

Run: `go test ./cmd/feishu-web/ -v`
Expected: PASS（`TestWriteProbeToTemp`、`TestListDocs`、`TestReadDoc`）

- [ ] **Step 7: 提交**

```bash
git add cmd/feishu-web/docs.go cmd/feishu-web/embed_docs.go cmd/feishu-web/main.go cmd/feishu-web/main_test.go
git commit -m "feat: serve docs from embedded filesystem with disk fallback"
```

---

## Task 5: Ubuntu 目录选择（zenity 兜底）

**Files:**
- Modify: `cmd/feishu-web/main.go`（`chooseFolder`）

- [ ] **Step 1: 修改 chooseFolder 的 default 分支**

把 `cmd/feishu-web/main.go` 中 `chooseFolder()` 的 `default:` 分支：

```go
	default:
		out, err := exec.Command("zenity", "--file-selection", "--directory", "--title=选择归档输出目录").Output()
		if err != nil {
			return nil, fmt.Errorf("当前系统不支持目录选择，请直接填写输出路径")
		}
		if strings.TrimSpace(string(out)) == "" {
			return nil, fmt.Errorf("已取消选择目录")
		}
		return out, nil
```

- [ ] **Step 2: 编译验证**

Run: `go build ./... && go vet ./...`
Expected: 无输出、退出码 0

- [ ] **Step 3: 提交**

```bash
git add cmd/feishu-web/main.go
git commit -m "feat: use zenity for folder selection on Linux"
```

---

## Task 6: 跨平台构建命令 `cmd/build`

**Files:**
- Create: `cmd/build/main.go`
- Test: `cmd/build/main_test.go`

- [ ] **Step 1: 写失败测试**

创建 `cmd/build/main_test.go`：

```go
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
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./cmd/build/ -v`
Expected: FAIL / build failed，提示 `undefined: parseTargets`

- [ ] **Step 3: 写实现**

创建 `cmd/build/main.go`：

```go
// Command build 交叉编译 Windows / macOS / Ubuntu 发行版：把飞书应用凭据以混淆
// 形式注入 feishu-probe，再把 probe 与 docs 打进 feishu-web，得到单文件。
package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"reimbursement-archiver/internal/credential"
)

type target struct {
	GOOS   string
	GOARCH string
}

func (t target) String() string { return t.GOOS + "-" + t.GOARCH }

func (t target) probeName() string {
	if t.GOOS == "windows" {
		return "feishu-probe.exe"
	}
	return "feishu-probe"
}

func (t target) webName() string {
	if t.GOOS == "windows" {
		return "feishu-web.exe"
	}
	return "feishu-web"
}

var defaultTargets = []target{
	{"windows", "amd64"},
	{"darwin", "arm64"},
	{"darwin", "amd64"},
	{"linux", "amd64"},
}

func parseTargets(spec string) ([]target, error) {
	var targets []target
	for _, part := range strings.Split(spec, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		goos, goarch, ok := strings.Cut(part, "/")
		goos, goarch = strings.TrimSpace(goos), strings.TrimSpace(goarch)
		if !ok || goos == "" || goarch == "" {
			return nil, fmt.Errorf("无效目标 %q，应为 goos/goarch", part)
		}
		targets = append(targets, target{goos, goarch})
	}
	if len(targets) == 0 {
		return nil, fmt.Errorf("目标列表为空")
	}
	return targets, nil
}

func main() {
	outDir := flag.String("out", "dist", "输出目录")
	targetSpec := flag.String("targets", "", "逗号分隔的 goos/goarch 列表，默认构建全部目标")
	flag.Parse()

	root, err := moduleRoot()
	if err != nil {
		fail(err)
	}
	targets := defaultTargets
	if strings.TrimSpace(*targetSpec) != "" {
		if targets, err = parseTargets(*targetSpec); err != nil {
			fail(err)
		}
	}

	idBlob := credential.Encode(strings.TrimSpace(os.Getenv("FEISHU_APP_ID")))
	secretBlob := credential.Encode(strings.TrimSpace(os.Getenv("FEISHU_APP_SECRET")))
	if idBlob == "" || secretBlob == "" {
		fmt.Println("警告：未提供 FEISHU_APP_ID / FEISHU_APP_SECRET，产出的二进制需要环境变量覆盖")
	}

	if err := stageDocs(root); err != nil {
		fail(fmt.Errorf("暂存 docs 失败：%w", err))
	}
	for _, t := range targets {
		if err := buildTarget(root, *outDir, t, idBlob, secretBlob); err != nil {
			fail(err)
		}
	}
}

func buildTarget(root, outDir string, t target, idBlob, secretBlob string) error {
	embedDir := filepath.Join(root, "cmd", "feishu-web", "embed", t.GOOS)
	if err := os.MkdirAll(embedDir, 0755); err != nil {
		return err
	}
	probePath := filepath.Join(embedDir, t.probeName())
	probeLDFlags := fmt.Sprintf("-s -w -X main.embeddedAppIDBlob=%s -X main.embeddedAppSecretBlob=%s", idBlob, secretBlob)
	if err := goBuild(root, t, probeLDFlags, false, probePath, "./cmd/feishu-probe"); err != nil {
		return fmt.Errorf("构建 %s probe 失败：%w", t, err)
	}

	webPath := filepath.Join(root, outDir, t.String(), t.webName())
	if err := os.MkdirAll(filepath.Dir(webPath), 0755); err != nil {
		return err
	}
	if err := goBuild(root, t, "-s -w", true, webPath, "./cmd/feishu-web"); err != nil {
		return fmt.Errorf("构建 %s web 失败：%w", t, err)
	}
	fmt.Printf("已生成 %s\n", webPath)
	return nil
}

func goBuild(root string, t target, ldflags string, embedProbe bool, output, pkg string) error {
	args := []string{"build", "-trimpath", "-ldflags", ldflags}
	if embedProbe {
		args = append(args, "-tags", "embedprobe")
	}
	args = append(args, "-o", output, pkg)
	cmd := exec.Command("go", args...)
	cmd.Dir = root
	cmd.Env = buildEnv(t)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	return cmd.Run()
}

func buildEnv(t target) []string {
	replaced := map[string]bool{"CGO_ENABLED": true, "GOOS": true, "GOARCH": true}
	out := make([]string, 0, len(os.Environ())+3)
	for _, kv := range os.Environ() {
		key, _, _ := strings.Cut(kv, "=")
		if replaced[key] {
			continue
		}
		out = append(out, kv)
	}
	return append(out, "CGO_ENABLED=0", "GOOS="+t.GOOS, "GOARCH="+t.GOARCH)
}

func stageDocs(root string) error {
	src := filepath.Join(root, "docs")
	dst := filepath.Join(root, "cmd", "feishu-web", "embed", "docs")
	if err := os.RemoveAll(dst); err != nil {
		return err
	}
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if info.IsDir() {
			if rel == "superpowers" {
				return filepath.SkipDir
			}
			return os.MkdirAll(filepath.Join(dst, rel), 0755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dst, rel), data, 0644)
	})
}

func moduleRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("未找到 go.mod")
		}
		dir = parent
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "构建失败："+err.Error())
	os.Exit(1)
}
```

- [ ] **Step 4: 运行测试确认通过**

Run: `go test ./cmd/build/ -v`
Expected: PASS（`TestParseTargets`、`TestTargetNames`）

- [ ] **Step 5: 提交**

```bash
git add cmd/build
git commit -m "feat: add cross-platform build command with credential injection"
```

---

## Task 7: 启动脚本、忽略规则与文档

**Files:**
- Modify: `.gitignore`
- Modify: `windows/start.bat`, `windows/start.ps1`, `windows/build.ps1`
- Modify: `docs/使用说明.md`, `README.md`, `windows/README.md`

- [ ] **Step 1: 更新 .gitignore**

在 `.gitignore` 末尾追加：

```
/cmd/feishu-web/embed/
/dist/
```

- [ ] **Step 2: 简化 Windows 启动脚本**

替换 `windows/start.bat` 为：

```bat
@echo off
setlocal
cd /d "%~dp0"
if not exist "feishu-web.exe" (
  echo 未找到 feishu-web.exe。请先运行 build.ps1 生成，或在项目根目录运行 go run ./cmd/feishu-web。
  pause
  exit /b 1
)
"feishu-web.exe"
if errorlevel 1 pause
endlocal
```

替换 `windows/start.ps1` 为：

```powershell
$ErrorActionPreference = "Stop"

$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$projectRoot = Split-Path -Parent $scriptDir
Set-Location $projectRoot

$bundledWeb = Join-Path $scriptDir "feishu-web.exe"
if (Test-Path $bundledWeb) {
    & $bundledWeb
    exit $LASTEXITCODE
}

if (-not (Get-Command go.exe -ErrorAction SilentlyContinue)) {
    throw "未找到 feishu-web.exe，且未安装 Go。请在有 Go 的机器上运行 windows\build.ps1 生成可执行文件。"
}

& go run .\cmd\feishu-web
exit $LASTEXITCODE
```

替换 `windows/build.ps1` 为：

```powershell
$ErrorActionPreference = "Stop"

$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$projectRoot = Split-Path -Parent $scriptDir
Set-Location $projectRoot

if (-not (Get-Command go.exe -ErrorAction SilentlyContinue)) {
    throw "需要安装 Go 才能构建。"
}

go run .\cmd\build @args
```

- [ ] **Step 3: 更新 docs/使用说明.md**

在“Windows 启动入口”小节之前新增一节：

```markdown
### 打包与分发（单文件、内置凭据）

在项目根目录用一条命令即可生成三个平台的单文件程序（凭据在构建时注入）：

```bash
FEISHU_APP_ID='cli_xxx' FEISHU_APP_SECRET='xxx' go run ./cmd/build
```

产物位于 `dist/`：

- `dist/windows-amd64/feishu-web.exe`
- `dist/darwin-arm64/feishu-web`（Apple 芯片）
- `dist/darwin-amd64/feishu-web`（Intel 芯片）
- `dist/linux-amd64/feishu-web`

同事拿到对应文件后无需任何配置：

- Windows：双击 `feishu-web.exe`；
- macOS：首次运行执行 `chmod +x feishu-web`，若被拦截则执行 `xattr -dr com.apple.quarantine feishu-web` 后再打开；
- Ubuntu：`chmod +x feishu-web && ./feishu-web`（目录选择需要系统已安装 zenity，否则手动填写输出路径）。

内置凭据可用环境变量或 `--app-id` / `--app-secret` 覆盖。注意：二进制内的凭据只做了轻量混淆，可被逆向提取，仅适用于内部可信分发。
```

- [ ] **Step 4: 更新 README.md 与 windows/README.md**

`README.md` 技术栈行下方追加一段：

```markdown
打包分发：`go run ./cmd/build` 可生成 Windows / macOS / Ubuntu 单文件程序，构建时注入飞书凭据，同事无需配置即可启动。详见 [docs/使用说明.md](docs/使用说明.md)。
```

`windows/README.md` 的“直接启动”部分改为：

```markdown
## 直接启动

双击 `start.bat` 或 `feishu-web.exe` 即可。打包后的 `feishu-web.exe` 已内置飞书凭据，无需配置环境变量。

如果还没有 `feishu-web.exe`，请在装有 Go 的机器上运行 `windows\build.ps1`；它会调用 `go run ./cmd/build` 生成内置凭据的单文件程序。
```

- [ ] **Step 5: 提交**

```bash
git add .gitignore windows/start.bat windows/start.ps1 windows/build.ps1 docs/使用说明.md README.md windows/README.md
git commit -m "docs: document single-file build and simplify Windows launchers"
```

---

## Task 8: 端到端验证

**Files:** 无（仅验证）

- [ ] **Step 1: 全量静态检查与测试**

Run:
```bash
gofmt -l cmd internal
go vet ./...
go test ./...
```
Expected: `gofmt -l` 无输出；vet 与 test 退出码 0，全部 PASS

- [ ] **Step 2: 构建单个平台并确认产物**

把真实凭据放进当前终端的环境变量（切勿写进任何文件或提交）：

```bash
export FEISHU_APP_ID='真实 App ID'
export FEISHU_APP_SECRET='真实 App Secret'
go run ./cmd/build -targets darwin/arm64
ls -l dist/darwin-arm64/feishu-web
```
Expected: 打印“已生成 .../dist/darwin-arm64/feishu-web”，文件存在且可执行

- [ ] **Step 3: 确认二进制内无明文凭据**

Run:
```bash
strings dist/darwin-arm64/feishu-web | grep -c "$FEISHU_APP_ID"
strings dist/darwin-arm64/feishu-web | grep -c "$FEISHU_APP_SECRET"
```
Expected: 两条都输出 `0`

- [ ] **Step 4: 启动打包产物并验证服务与内嵌 docs**

Run:
```bash
NO_BROWSER_OPEN=1 ./dist/darwin-arm64/feishu-web &
sleep 2
curl -s http://127.0.0.1:8765/api/docs
kill %1
```
Expected: `/api/docs` 返回包含 `使用说明.md` 的 JSON 数组

- [ ] **Step 5: 确认开发流程未受影响**

Run:
```bash
NO_BROWSER_OPEN=1 go run ./cmd/feishu-web &
sleep 4
curl -s http://127.0.0.1:8765/ | head -c 60
kill %1
```
Expected: 返回页面 HTML（`<!doctype html>` 开头），说明无 embedprobe 标签的开发构建正常

- [ ] **Step 6: 提交验证结果（如有微调）**

```bash
git status
```
Expected: 工作区仅剩 `dist/`、`cmd/feishu-web/embed/`（均被忽略），无未跟踪源码变更

---

## 验收对照

| 设计验收标准 | 对应任务 |
| --- | --- |
| 无环境变量即可启动打包产物 | Task 6 + Task 8 Step 4 |
| 单文件含 web + probe + docs | Task 3 + Task 4 + Task 8 Step 2/4 |
| 参数/环境变量可覆盖内置值 | Task 2 |
| 三端产物可启动，Ubuntu 目录选择 | Task 5 + Task 6 + Task 8 |
| 仓库不含真实凭据 | Task 1/2（仅注入）+ Task 8 Step 3 |
| 开发流程不受影响 | Task 3（stub）+ Task 8 Step 5 |
