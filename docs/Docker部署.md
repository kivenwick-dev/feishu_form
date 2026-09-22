# Docker 部署说明

本文用于把飞书报销表归档工具部署到 Ubuntu 云服务器。线上页面完成归档后会直接提供 ZIP 下载，不需要在浏览器里选择服务器路径。

## 1. 准备服务器

安装 Docker 和 Compose 插件：

```bash
sudo apt update
sudo apt install -y docker.io docker-compose-plugin
sudo systemctl enable --now docker
```

如果希望当前用户直接运行 Docker：

```bash
sudo usermod -aG docker "$USER"
```

退出 SSH 后重新登录，让用户组生效。

## 2. 上传或拉取项目

在服务器上拉取 GitHub 的 `test` 分支：

```bash
git clone -b test https://github.com/kivenwick-dev/feishu_form.git
cd feishu_form
```

如果服务器不能使用 SSH Key，也可以用 HTTPS：

```bash
git clone -b test https://github.com/kivenwick-dev/feishu_form.git
cd feishu_form
```

如果项目目录已经存在，进入目录后更新到 `test` 分支最新代码：

```bash
cd feishu_form
git checkout test
git pull origin test
```

## 3. 配置 .env

复制模板：

```bash
cp .env.example .env
```

编辑 `.env`：

```bash
nano .env
```

至少需要填写：

```dotenv
FEISHU_APP_ID=cli_xxxxxxxxxxxxx
FEISHU_APP_SECRET=xxxxxxxxxxxxxxxx
REIMBURSEMENT_PUBLIC_HOST=0.0.0.0
REIMBURSEMENT_PORT=8765
```

常用配置说明：

| 配置 | 作用 |
| --- | --- |
| `FEISHU_APP_ID` | 飞书企业自建应用 App ID |
| `FEISHU_APP_SECRET` | 飞书企业自建应用 App Secret |
| `REIMBURSEMENT_PUBLIC_HOST` | 宿主机监听 IP。公网访问用 `0.0.0.0`，只给 Nginx 反代用 `127.0.0.1` |
| `REIMBURSEMENT_PORT` | 服务端口，默认 `8765` |
| `REIMBURSEMENT_OUTPUT_DIR` | 容器内输出目录，默认 `/data/outputs` |
| `REIMBURSEMENT_HOST_OUTPUT_DIR` | 宿主机保存输出文件的位置，默认 `./outputs` |
| `REIMBURSEMENT_OUTPUT_RETENTION` | outputs 自动清理保留时长，默认 `10m`；设为 `0` 可关闭 |
| `REIMBURSEMENT_OUTPUT_CLEANUP_INTERVAL` | outputs 自动清理扫描间隔，默认 `1m` |
| `REIMBURSEMENT_DISABLE_FOLDER_PICKER` | 是否隐藏桌面选择目录按钮。Docker 中建议保持 `1` |

`.env` 内含密钥，已经被 `.gitignore` 忽略，不要提交到 Git。

## 4. 构建并启动

```bash
docker compose up -d --build
```

查看日志：

```bash
docker compose logs -f
```

看到类似下面的日志即启动成功：

```text
飞书报销归档工具已启动：http://0.0.0.0:8765
```

浏览器访问：

```text
http://服务器公网IP:8765
```

如果服务器有防火墙或云厂商安全组，需要放行 `.env` 中的 `REIMBURSEMENT_PORT`。

## 5. 使用方式

1. 打开页面。
2. 粘贴飞书 Wiki/电子表格链接。
3. 上传与线上表对应的 Excel 文件。
4. 输出目录已锁定为服务端配置的 `/data/outputs`，无需也不能在网页中修改。
5. 点击“开始归档”。
6. 完成后点击页面里的“下载 ZIP”。

服务只把结果临时写到 `REIMBURSEMENT_OUTPUT_DIR` 供浏览器下载；ZIP 成功下载后，会立即删除服务器上的 ZIP 和同名结果目录。自动清理仍会作为兜底运行，默认删除超过 10 分钟的残留输出。页面运行日志也会在任务结束 10 分钟后自动清空；如需临时延长未下载结果的保留窗口，可在 `.env` 中调大 `REIMBURSEMENT_OUTPUT_RETENTION` 后重启容器。

ZIP 文件名和内部顶层目录会沿用飞书表格标题，例如：

```text
8月份-模型团队 - AI提效工具 - 部门报销表.zip
```

ZIP 内部仍是原来的打包形式：

```text
8月份-模型团队 - AI提效工具 - 部门报销表/
├── 员工A/
├── 员工B/
├── 汇总.txt
└── 失败清单.txt
```

宿主机也能在 `REIMBURSEMENT_HOST_OUTPUT_DIR` 对应目录看到生成结果。

## 6. 修改 IP 或端口

修改 `.env`：

```dotenv
REIMBURSEMENT_PUBLIC_HOST=0.0.0.0
REIMBURSEMENT_PORT=9000
```

重启容器：

```bash
docker compose up -d
```

然后访问：

```text
http://服务器公网IP:9000
```

## 7. 常用维护命令

停止服务：

```bash
docker compose down
```

重启服务：

```bash
docker compose restart
```

更新代码后重新构建：

```bash
docker compose up -d --build
```

查看容器状态：

```bash
docker compose ps
```

进入容器排查：

```bash
docker compose exec reimbursement-archiver sh
```

## 8. 反向代理建议

如果使用 Nginx 和域名，建议在 `.env` 中只监听本机：

```dotenv
REIMBURSEMENT_PUBLIC_HOST=127.0.0.1
REIMBURSEMENT_PORT=8765
```

Nginx 反代到：

```text
http://127.0.0.1:8765
```

这样服务不会直接暴露在公网端口上，再由 Nginx 负责 HTTPS、访问控制和日志。
