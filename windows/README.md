# Windows 版使用说明

这个目录为 Windows 用户提供启动和打包入口。业务代码仍然位于项目根目录的 `cmd/`、`docs/` 和 `go.mod`，Windows 目录只负责启动和生成 Windows 可执行文件。

## 直接启动

双击 `start.bat`。脚本会：

1. 切换到项目根目录；
2. 使用当前环境中的 `FEISHU_APP_ID` 和 `FEISHU_APP_SECRET`，如果没有就交互式输入；
3. 优先运行 `windows/feishu-web.exe`；
4. 如果还没有 `.exe`，检查 Go 是否安装，并使用 `go run` 启动源码版本；
5. 自动打开 `http://127.0.0.1:8765`。

App Secret 使用安全输入，不会写入项目文件。也可以提前在 PowerShell 中设置环境变量：

```powershell
$env:FEISHU_APP_ID = "cli_xxxxxxxxxxxxx"
$env:FEISHU_APP_SECRET = "xxxxxxxxxxxxxxxx"
.\windows\start.bat
```

## 生成 Windows 可执行文件

在 Windows PowerShell 中，从项目根目录运行：

```powershell
.\windows\build.ps1
```

脚本会生成：

```text
windows\feishu-web.exe
windows\feishu-probe.exe
```

生成后再次双击 `start.bat` 即可，不需要 Go 参与网页归档过程。程序仍需要网络访问 `open.feishu.cn`，并需要有效的飞书应用凭据。

默认脚本生成 Windows x64 版本。如需 Windows ARM64，可先设置：

```powershell
$env:GOARCH = "arm64"
.\windows\build.ps1
```

## 命令行归档

网页界面之外，也可以直接运行：

```powershell
$env:FEISHU_APP_ID = "cli_xxxxxxxxxxxxx"
$env:FEISHU_APP_SECRET = "xxxxxxxxxxxxxxxx"
.\windows\feishu-probe.exe `
  --archive `
  --archive-dir outputs `
  --url "https://你的飞书链接" `
  --excel "C:\Users\你的用户名\Documents\同一张报销表.xlsx"
```

如果尚未生成 `.exe`，可将命令中的 `.\windows\feishu-probe.exe` 换成：

```powershell
go run .\cmd\feishu-probe ...
```

## 路径和安全

- 输出目录可以使用 Windows 绝对路径，例如 `D:\报销归档`。
- 网页中的“选择目录”按钮会调用 Windows 文件夹选择框；也可以直接在输入框中填写路径。
- 程序只监听 `127.0.0.1`，不会对局域网开放。
- 不要把 App Secret 写入项目文件、脚本或 Git。
