# Windows 版使用说明

这个目录为 Windows 用户提供启动和打包入口。业务代码仍然位于项目根目录的 `cmd/`、`docs/` 和 `go.mod`，Windows 目录只负责启动和生成 Windows 可执行文件。

## 直接启动

双击 `start.bat` 或 `feishu-web.exe` 即可。打包后的 `feishu-web.exe` 已内置飞书凭据，无需配置环境变量。

如果还没有 `feishu-web.exe`，请在装有 Go 的机器上运行 `windows\build.ps1`；它会调用 `go run ./cmd/build -targets windows/amd64` 生成内置凭据的单文件程序，并复制到 `windows/feishu-web.exe`。开发者在装有 Go 时也可以用 `start.ps1` 直接走源码。

## 生成 Windows 可执行文件

先设置凭据环境变量，再从项目根目录运行：

```powershell
$env:FEISHU_APP_ID = "cli_xxxxxxxxxxxxx"
$env:FEISHU_APP_SECRET = "xxxxxxxxxxxxxxxx"
.\windows\build.ps1
```

脚本会调用 `go run .\cmd\build -targets windows/amd64`，生成单个可执行文件：

```text
dist\windows-amd64\feishu-web.exe
```

并自动复制一份到 `windows\feishu-web.exe`，方便直接双击。该文件已内嵌 `feishu-probe` 与 `docs/`，同事拿到后不需要 Go，也不需要配置凭据。

如需其它平台或架构，可直接调用构建命令，例如只生成 Windows ARM64（此时不会复制到 `windows\`，请从 `dist\windows-arm64\` 取用）：

```powershell
go run .\cmd\build -targets windows/arm64
```

> 注意：未设置 `FEISHU_APP_ID` / `FEISHU_APP_SECRET` 时构建仍会成功，但产物需要环境变量覆盖才能使用。
> 产物内已内嵌（混淆后的）凭据，不要上传到公开 Release 或对外发送。

## 命令行归档

网页界面之外，也可以直接运行源码版：

```powershell
$env:FEISHU_APP_ID = "cli_xxxxxxxxxxxxx"
$env:FEISHU_APP_SECRET = "xxxxxxxxxxxxxxxx"
go run .\cmd\feishu-probe `
  --archive `
  --archive-dir outputs `
  --url "https://你的飞书链接" `
  --excel "C:\Users\你的用户名\Documents\同一张报销表.xlsx"
```

单文件打包只产出 `feishu-web`；如需独立的 probe 可执行文件，可用 `go build -o feishu-probe.exe .\cmd\feishu-probe` 自行构建。

## 路径和安全

- 输出目录可以使用 Windows 绝对路径，例如 `D:\报销归档`。
- 网页中的“选择目录”按钮会调用 Windows 文件夹选择框；也可以直接在输入框中填写路径。
- 程序只监听 `127.0.0.1`，不会对局域网开放。
- 不要把 App Secret 写入项目文件、脚本或 Git。
