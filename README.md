# 报销表拆分工具

本项目将飞书电子表格或本地 Excel 拆分为按员工归档的文件夹，并生成 ZIP。

完整的安装、环境变量、网页启动、Excel 对应规则和故障排查请阅读：[docs/使用说明.md](docs/使用说明.md)。

## 技术栈

- **正式实现：Go**。交付为单一 macOS 可执行文件，不依赖 Python 运行环境。
- **飞书接入：** `net/http` 调用飞书 OpenAPI，采用用户授权或企业自建应用令牌。
- **本地归档：** Go 标准库负责目录、文本、ZIP、并发下载与重试。
- **界面：** 后续使用本地浏览器页面（`127.0.0.1`）作为小工具界面，不制作需要签名的 `.app`。
- **Python 脚本：** 仅保留为既有 Excel 离线模式原型，不作为线上版正式依赖。

## 当前阶段：飞书可行性探测

使用 `cmd/feishu-probe` 验证以下链路：

1. 通过企业自建应用获取 tenant access token。
2. 将 Wiki 链接解析为实际的电子表格 token。
3. 获取电子表格和工作表信息。
4. 读取指定工作表前若干行单元格值。
5. 输出附件单元格是否含有可下载的 file token 或链接。

探测器只读飞书数据，不会改动任何线上文档，不会下载附件。

## 运行

不要把 App Secret 提交到项目中。通过环境变量临时传入：

```bash
FEISHU_APP_ID='cli_xxx' \
FEISHU_APP_SECRET='xxx' \
go run ./cmd/feishu-probe \
  --url 'https://ucn81zkkano6.feishu.cn/wiki/Rbv1wt09tiHbsOkWq6qc5lhinAe?sheet=0EDeTa'
```

探测结果保存为 `outputs/feishu_probe_result.json`。

批量归档验收命令：

```bash
go run ./cmd/feishu-probe --archive --archive-dir outputs \
  --url 'https://你的飞书链接'
```

该命令会直接下载附件原文件，并生成员工目录与 ZIP。正式版会将这部分能力迁移到独立归档命令和本地浏览器界面。

## 本地浏览器界面

双击项目根目录的 `启动线上归档工具.command`。它会从 macOS 钥匙串读取飞书应用凭据，随后自动打开本机页面 `http://127.0.0.1:8765`。粘贴飞书链接、填写输出目录并点击“开始归档”即可。

页面只监听 `127.0.0.1`，不会对局域网开放。

## 附件与链接说明

- 含 `file_token` 的附件会通过 Drive 媒体接口下载原文件并归档。
- 若单元格只是普通网页链接、没有 `file_token`，程序会保留链接信息，但无法直接下载其背后的文件。

链接可以不带 `sheet` 参数，程序会自动选择工作表列表中的第一个工作表。附件单元格可混合图片和 PDF；归档时按附件所在列分类，并依据 MIME/文件头保留或补齐正确扩展名。
