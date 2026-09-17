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
