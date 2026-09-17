# 内置凭据与单文件跨平台打包设计

日期：2026-09-17
状态：待评审

## 背景

当前工具依赖外部凭据启动：

- `cmd/feishu-probe` 从 `FEISHU_APP_ID` / `FEISHU_APP_SECRET` 环境变量（或 `--app-id` / `--app-secret`）读取应用凭据；
- macOS 通过 `启动线上归档工具.command` 从 Keychain 取值后 export；
- Windows 通过 `windows/start.ps1` 交互式询问。

同事拿到二进制后必须自行配置凭据（Keychain、环境变量或交互输入），而且 Keychain 锁定时会静默传入空值，导致难懂的报错。目标是：打包出的二进制开箱即用，无需任何配置，同时支持 Windows / macOS / Ubuntu 三端，且每个平台只交付一个可执行文件。

## 目标

1. 应用凭据在构建时以轻量混淆形式注入二进制，运行时自动还原，同事无需配置。
2. 保留可覆盖能力：`--app-id` / `--app-secret` 参数 > 环境变量 > 内置值。
3. 单文件交付：`feishu-web` 内嵌 `feishu-probe` 与 `docs/`，一个可执行文件即可运行。
4. 一条跨平台命令产出 Windows / macOS / Ubuntu 三种二进制。
5. 凭据不进入 git 仓库。

## 非目标

- 不做真正的密钥保护（二进制内任何机密都可被逆向提取），本方案只做“轻量混淆 + 不落仓库”。
- 不做服务端代理凭据。
- 不做 macOS 代码签名 / 公证。
- 不做自动更新。
- 不改变归档业务逻辑（附件分类、截图 PDF 等保持现状）。

## 关键决策

| 决策项 | 结论 |
| --- | --- |
| 混淆方式 | 固定密钥 XOR + `base64.RawURLEncoding`（可逆，非哈希） |
| 密钥进入方式 | 构建时注入，凭据不落 git |
| 分发形式 | 单文件（web 内嵌 probe 与 docs） |
| 构建入口 | 一个跨平台 Go 构建命令 `cmd/build` |
| 目标平台 | windows/amd64、darwin/arm64、darwin/amd64、linux/amd64 |
| docs 抽屉 | 一并内嵌 |
| Ubuntu 选目录 | zenity 兜底，未安装则提示手动填路径 |
| 使用对象 | 内部同事，基本可信 |

哈希不可用：飞书 API 需要明文 `app_secret` 换取 `tenant_access_token`，运行时必须能还原原值，而哈希是单向的。

## 架构

### 组件一：`internal/credential`

职责：对内置凭据做可逆混淆。

接口：

```go
func Encode(plain string) string
func Decode(blob string) (string, error)
```

规则：
- 固定 16 字节 XOR 密钥（代码常量）。
- 输出用 `base64.RawURLEncoding`，只含 `A-Za-z0-9-_`，便于脚本安全传参。
- `Encode("")` 返回 `""`；`Decode("")` 返回 `""`，无错误。
- `Decode` 对非法 base64 返回错误。

依赖：仅标准库 `encoding/base64`。

### 组件二：`cmd/feishu-probe` 凭据解析

新增注入变量：

```go
var embeddedAppIDBlob string
var embeddedAppSecretBlob string
```

新增可测试的解析函数：

```go
func resolveCredential(envValue, blob string) string
```

逻辑：`envValue`（去空格）非空则用它，否则用 `Decode(blob)` 的结果（解码失败返回空）。

`main()` 中：

- `appID` 默认值 = `resolveCredential(os.Getenv("FEISHU_APP_ID"), embeddedAppIDBlob)`
- `appSecret` 默认值同理
- 参数 `--app-id` / `--app-secret` 显式传入时覆盖默认值
- 校验失败时的错误文案更新为：“请提供表格链接，并通过内置凭据、`FEISHU_APP_ID` / `FEISHU_APP_SECRET` 环境变量或 `--app-id` / `--app-secret` 参数提供应用凭据。”

### 组件三：`cmd/build` 跨平台构建命令

职责：编码凭据、按目标平台编译 probe、暂存内嵌资源、编译 web、输出到 `dist/`。

流程：

1. 读取 `FEISHU_APP_ID` / `FEISHU_APP_SECRET`，用 `credential.Encode` 得到混淆串。
   - 若缺失：打印警告“未提供凭据，产出的二进制需要环境变量覆盖”，继续构建。
2. 暂存 `docs/` 到 `cmd/feishu-web/embed/docs/`（所有平台共用）。
3. 对每个目标 `{GOOS, GOARCH}`：
   1. `go build -ldflags "-X main.embeddedAppIDBlob=… -X main.embeddedAppSecretBlob=…" -o cmd/feishu-web/embed/<goos>/feishu-probe[.exe] ./cmd/feishu-probe`；
   2. `go build -tags embedprobe -o dist/<goos>-<goarch>/feishu-web[.exe] ./cmd/feishu-web`。
4. 处理多架构同 GOOS 的暂存覆盖：darwin/arm64 与 darwin/amd64 依次“暂存→编译 web”，避免互相覆盖。

参数（保持最小）：
- `-out`：输出目录，默认 `dist`
- `-targets`：逗号分隔的 `goos/goarch` 列表（例如 `windows/amd64,linux/amd64`），覆盖默认目标集合

约束：
- `CGO_ENABLED=0`。
- 暂存目录写入 `.gitignore`。

### 组件四：`cmd/feishu-web` 内嵌层

按 build tag 分离：

- `embed_stub.go`（`//go:build !embedprobe`）：`embeddedProbe()` 返回 `nil`，`embeddedDocs()` 返回 `nil`。开发用 `go run` 走这条，行为同现在。
- `embed_darwin.go`（`//go:build darwin && embedprobe`）
- `embed_linux.go`（`//go:build linux && embedprobe`）
- `embed_windows.go`（`//go:build windows && embedprobe`）
  分别 `//go:embed embed/<goos>/feishu-probe[.exe]`。
- `embed_docs.go`（`//go:build embedprobe`）：`//go:embed all:embed/docs`。

对外函数：

```go
func embeddedProbe() (data []byte, name string)
func embeddedDocs() fs.FS // 无内嵌时为 nil
```

probe 调用优先级（改造 `archiveCommand`）：

1. 可执行文件同目录存在 `feishu-probe`（开发/旧方式）→ 直接用；
2. 有内嵌 probe → 首次解压到临时目录（`os.MkdirTemp`），`chmod 0755`，用 `sync.Once` 缓存路径 → 调用；
3. 都没有 → 回退 `go run ./cmd/feishu-probe`。

probe 进程工作目录 `cmd.Dir`：仍设为项目根（`projectRoot()`）以便相对输出路径落到用户预期位置；打包模式下 `projectRoot()` 无 `docs/`、`cmd/` 时回退当前工作目录。

docs 抽屉：

- `docsHandler` / `docReadHandler` / `/docs/` 静态服务：有内嵌 docs 时用 `fs.FS` 读取；否则回退项目根 `docs/` 目录。
- 保持现有接口 `/api/docs`、`/api/docs/read`、`/docs/` 不变。

`chooseFolder()`：

- Windows：现有 PowerShell 方案不变。
- macOS：现有 osascript 方案不变。
- Linux：先尝试 `zenity --file-selection --directory`；命令不存在或失败则返回“请直接填写输出路径”。

### 组件五：启动入口与文档

- `windows/start.bat`：简化为直接运行同目录 `feishu-web.exe`（不再询问凭据）；exe 不存在时打印提示。
- `windows/start.ps1`：删除其中的凭据交互逻辑，仅保留“无 exe 时用 `go run` 兜底”的能力，供开发者在 Windows 上调试。
- `windows/build.ps1`：改为调用 `go run ./cmd/build` 的薄封装。
- 在 `docs/使用说明.md` 新增“打包与分发”章节，包含三端产物获取、macOS Gatekeeper 处理（右键打开或 `xattr -dr com.apple.quarantine`）、Ubuntu `chmod +x`、带凭据注入的构建命令。
- `README.md`、`windows/README.md` 同步更新：默认二进制无需配置。

## 凭据优先级（最终）

```
--app-id / --app-secret 参数
  > FEISHU_APP_ID / FEISHU_APP_SECRET 环境变量
    > 二进制内置值（XOR+base64 混淆）
      > 空 → 报错并提示三种来源
```

## 错误处理

- 构建时无凭据：警告并继续，产物可用环境变量覆盖。
- 混淆串解码失败：视为无内置值，继续按环境变量逻辑；不 panic。
- 内嵌 probe 解压失败：`finish(err)` 反馈到网页日志，文案说明“内置程序释放失败”。
- Linux 无 zenity：目录选择接口返回可读错误，页面提示手动填路径。
- 单文件启动时找不到项目根：`projectRoot()` 回退当前工作目录，docs 走内嵌。

## 测试

单元测试（`go test ./...`）：

- `internal/credential`：往返一致性、空串、非法 base64、含中文/特殊字符。
- `cmd/feishu-probe`：`resolveCredential` 的优先级（env 优先于内置、内置兜底、都空返回空）。
- `cmd/feishu-web`：`archiveCommand` 在有内嵌 probe 时能解压出可执行文件（用临时数据）；无内嵌时不 panic。
- `cmd/build`：目标列表解析、暂存路径计算等纯函数；不实际交叉编译。

验证命令：

- `go build ./...`、`go vet ./...`、`gofmt -l`、`go test ./...`
- 手工：`FEISHU_APP_ID=… FEISHU_APP_SECRET=… go run ./cmd/build` 产出四个目标；
- 手工：macOS 上以 `NO_BROWSER_OPEN=1` 启动打包产物，页面正常、docs 抽屉可打开、能完成一次归档。

## 安全说明

- 内置凭据可被逆向提取，仅适用于内部可信分发。
- 构建注入的明文不得写入仓库、日志或提交信息。
- 设计文档与代码中不出现真实密钥。
- 建议：若分发范围扩大，改用服务端代理。

## 验收标准

1. 不带任何环境变量，运行打包后的单文件二进制即可启动并完成归档。
2. 一个文件即包含 web + probe + docs，无需携带源码或 `go`。
3. `--app-id` / `--app-secret` 与环境变量仍可覆盖内置值。
4. Windows / macOS / Ubuntu 三端产物均可启动，Ubuntu 目录选择在装有 zenity 时可用。
5. 仓库中不包含真实凭据。
6. 现有 `go run ./cmd/feishu-web` 开发流程不受影响。

## 风险与权衡

- **逆向提取**：轻量混淆只能挡顺手查看，无法防逆向。已知并可接受。
- **固定密钥**：看懂代码的人可自解；如需更强可改为每次构建随机密钥（本次不做）。
- **临时目录残留**：解压出的 probe 在系统临时目录，进程退出后由操作系统清理策略处理；可接受。
- **未签名 macOS 产物**：需要文档说明 Gatekeeper 绕过步骤，否则同事打不开。
