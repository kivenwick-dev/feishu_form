package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

type jobState struct {
	Running      bool     `json:"running"`
	Done         bool     `json:"done"`
	Error        string   `json:"error,omitempty"`
	Logs         []string `json:"logs"`
	ArchivePath  string   `json:"-"`
	ZipName      string   `json:"zip_name,omitempty"`
	DownloadURL  string   `json:"download_url,omitempty"`
	logsExpireAt time.Time
}

var job = struct {
	sync.Mutex
	jobState
}{jobState: jobState{Logs: []string{"等待开始"}}}

const page = `<!doctype html><html lang="zh-CN"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>飞书报销表归档</title>
<style>body{font-family:-apple-system,BlinkMacSystemFont,"PingFang SC",sans-serif;margin:0;padding:0 34px 48px 300px;color:#172033;background:#f5f7fb}main{max-width:860px;margin:42px auto}.card{background:#fff;border:1px solid #e7eaf0;border-radius:16px;padding:30px 34px;box-shadow:0 10px 30px #20304d0d}h1{font-size:28px;margin:0 0 8px}label{display:block;margin:22px 0 8px;font-weight:650;font-size:14px}input{width:100%;box-sizing:border-box;padding:12px 13px;border:1px solid #d5dbe5;border-radius:9px;font-size:14px;background:#fff}input:focus{outline:3px solid #1677ff22;border-color:#1677ff}.row{display:flex;align-items:center}.row input{flex:1}button{margin-top:24px;padding:12px 24px;border:0;border-radius:9px;background:#1677ff;color:#fff;font-size:15px;font-weight:600;cursor:pointer;transition:.15s}button:hover{background:#0f63d8;transform:translateY(-1px)}button.small{margin:0 0 0 10px;padding:10px 14px;font-size:13px;background:#eef4ff;color:#145dcc}button.small:hover{background:#dceaff}button:disabled{background:#aeb8c8;cursor:not-allowed;transform:none}progress{width:100%;height:12px;margin-top:24px;accent-color:#1677ff}#status{margin-top:18px;padding:13px 15px;border-radius:9px;background:#f6f8fb;white-space:pre-wrap;color:#526078;line-height:1.7;font-size:13px;min-height:22px}.hint{color:#718096;font-size:13px}.drawer{position:fixed;left:0;top:0;width:260px;height:100vh;box-sizing:border-box;padding:28px 18px;background:#fff;border-right:1px solid #e3e7ef;overflow:auto;box-shadow:4px 0 18px #20304d08}.drawer h2{font-size:18px;margin:0 0 6px}.drawer .sub{font-size:12px;color:#8792a5;margin-bottom:18px}.drawer a{display:block;padding:10px 11px;color:#245fc2;text-decoration:none;border-radius:8px;font-size:13px;cursor:pointer}.drawer a:hover{background:#eef4ff}.modal{display:none;position:fixed;inset:0;background:#17203366;z-index:10;align-items:center;justify-content:center;padding:24px}.modal.show{display:flex}.modalbox{background:#fff;border-radius:14px;width:min(760px,94vw);max-height:82vh;display:flex;flex-direction:column;box-shadow:0 20px 60px #17203344}.modalhead{padding:16px 20px;border-bottom:1px solid #e8ebf1;display:flex;justify-content:space-between;align-items:center;font-weight:650}.close{margin:0;padding:2px 9px;background:transparent;color:#65738a;font-size:24px;font-weight:400}.close:hover{background:#f0f2f6;color:#172033;transform:none}.modalbody{padding:22px;overflow:auto;white-space:pre-wrap;line-height:1.75;color:#39465a;font-size:14px}</style>
<div class="drawer"><h2>文件抽屉</h2><div class="sub">点击文件名查看使用说明</div><div id="docs">正在读取说明…</div></div><main><div class="card"><h1>飞书报销表归档工具</h1><p class="hint">线上表格下载 PDF，本地 Excel 补充浮动图片，最后合并生成员工目录和 ZIP。</p>
<label>飞书表格链接</label><input id="url" placeholder="粘贴 https://...feishu.cn/wiki/... 链接"><label>本地 Excel 文件（可选）</label><input id="excel" type="file" accept=".xlsx,.xlsm"><label>截图 PDF 图片尺寸（毫米）</label><div class="row"><input id="w" type="number" min="1" step="1" value="180" placeholder="宽度"><input id="h" type="number" min="1" step="1" value="260" placeholder="高度"></div><label>输出目录</label><div class="row"><input id="out" value="{{.DefaultOut}}" readonly></div><p class="hint">云服务存档：归档文件和运行日志仅保留 10 分钟，随后自动清理。</p><button id="go" onclick="start()">开始归档</button><progress id="bar" value="0" max="100"></progress><div id="status">等待开始</div><div id="download"></div></div></main><div id="modal" class="modal" onclick="if(event.target===this)closeDoc()"><div class="modalbox"><div class="modalhead"><span id="modalTitle">使用说明</span><button class="close" onclick="closeDoc()">×</button></div><div id="modalBody" class="modalbody"></div></div></div>
<script>let timer;async function chooseOut(){let r=await fetch('/api/choose-folder',{method:'POST'});if(r.ok){document.getElementById('out').value=(await r.json()).path}else{alert(await r.text())}}async function loadDocs(){let r=await fetch('/api/docs');let d=await r.json();let box=document.getElementById('docs');box.replaceChildren();if(!d.length){let empty=document.createElement('span');empty.className='hint';empty.textContent='暂无说明文件';box.append(empty);return}d.forEach(name=>{let a=document.createElement('a');a.href='#';a.textContent=name;a.addEventListener('click',e=>{e.preventDefault();openDoc(name)});box.append(a)})}async function openDoc(name){let r=await fetch('/api/docs/read?name='+encodeURIComponent(name));let body=await r.text();document.getElementById('modalTitle').textContent=name;document.getElementById('modalBody').textContent=r.ok?body:'读取失败：'+body;document.getElementById('modal').classList.add('show')}function closeDoc(){document.getElementById('modal').classList.remove('show')}document.addEventListener('keydown',e=>{if(e.key==='Escape')closeDoc()});function renderDownload(s){let box=document.getElementById('download');box.replaceChildren();if(!s.download_url)return;let a=document.createElement('a');a.href=s.download_url;a.textContent='下载 ZIP：'+(s.zip_name||'归档结果.zip');a.style.display='inline-block';a.style.marginTop='18px';a.style.padding='11px 18px';a.style.borderRadius='9px';a.style.background='#14a36f';a.style.color='#fff';a.style.textDecoration='none';a.style.fontWeight='650';box.append(a)}async function start(){let b=document.getElementById('go'),u=document.getElementById('url').value,o=document.getElementById('out').value;let w=Number(document.getElementById('w').value),h=Number(document.getElementById('h').value);b.disabled=true;document.getElementById('bar').value=5;document.getElementById('download').replaceChildren();let x='';let f=document.getElementById('excel').files[0];if(f){let fd=new FormData();fd.append('file',f);let up=await fetch('/api/upload-excel',{method:'POST',body:fd});if(!up.ok){document.getElementById('status').textContent=await up.text();b.disabled=false;return}x=(await up.json()).path}let r=await fetch('/api/start',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({url:u,excel:x,out:o,width:w,height:h})});if(!r.ok){document.getElementById('status').textContent=await r.text();b.disabled=false;return}timer=setInterval(poll,700)}async function poll(){let r=await fetch('/api/status'),s=await r.json();document.getElementById('status').textContent=s.logs.join('\n');renderDownload(s);document.getElementById('bar').value=s.done?100:(s.running?Math.min(95,10+s.logs.length*4):0);if(s.done||s.error){clearInterval(timer);document.getElementById('go').disabled=false;if(s.error)document.getElementById('bar').value=0}}loadDocs();poll()</script></html>`

func main() {
	startOutputCleanup()
	startLogCleanup()
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		data := struct {
			DefaultOut          string
			FolderPickerEnabled bool
		}{
			DefaultOut:          defaultOutputDir(),
			FolderPickerEnabled: folderPickerEnabled(),
		}
		_ = template.Must(template.New("p").Parse(page)).Execute(w, data)
	})
	http.HandleFunc("/api/start", startHandler)
	http.HandleFunc("/api/upload-excel", uploadExcelHandler)
	http.HandleFunc("/api/choose-folder", chooseFolderHandler)
	http.HandleFunc("/api/docs", docsHandler)
	http.HandleFunc("/api/docs/read", docReadHandler)
	http.HandleFunc("/api/download", downloadHandler)
	http.Handle("/docs/", http.StripPrefix("/docs/", http.FileServer(http.FS(docsSource()))))
	http.HandleFunc("/api/status", statusHandler)
	addr := listenAddr()
	if os.Getenv("NO_BROWSER_OPEN") != "1" {
		go func() { time.Sleep(500 * time.Millisecond); openBrowser("http://" + addr) }()
	}
	fmt.Println("飞书报销归档工具已启动：http://" + addr)
	if err := http.ListenAndServe(addr, nil); err != nil {
		panic(err)
	}
}

func chooseFolderHandler(w http.ResponseWriter, r *http.Request) {
	out, err := chooseFolder()
	if err != nil {
		http.Error(w, "无法选择目录："+err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"path": strings.TrimSpace(string(out))})
}

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

func uploadExcelHandler(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(100 << 20); err != nil {
		http.Error(w, "无法读取 Excel 文件", http.StatusBadRequest)
		return
	}
	f, hdr, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "请选择 Excel 文件", http.StatusBadRequest)
		return
	}
	defer f.Close()
	if !strings.HasSuffix(strings.ToLower(hdr.Filename), ".xlsx") && !strings.HasSuffix(strings.ToLower(hdr.Filename), ".xlsm") {
		http.Error(w, "仅支持 .xlsx 或 .xlsm", http.StatusBadRequest)
		return
	}
	tmp, err := os.CreateTemp("", "reimbursement-*.xlsx")
	if err != nil {
		http.Error(w, "无法保存上传文件", 500)
		return
	}
	defer tmp.Close()
	if _, err = io.Copy(tmp, f); err != nil {
		http.Error(w, "保存 Excel 文件失败", 500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"path": tmp.Name()})
}

func startHandler(w http.ResponseWriter, r *http.Request) {
	var req struct {
		URL, Out, Excel string
		Width, Height   float64
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.URL) == "" {
		http.Error(w, "请输入飞书表格链接", http.StatusBadRequest)
		return
	}
	job.Lock()
	if job.Running {
		job.Unlock()
		http.Error(w, "已有任务正在运行", http.StatusConflict)
		return
	}
	job.Running, job.Done, job.Error, job.ArchivePath, job.ZipName, job.DownloadURL, job.Logs = true, false, "", "", "", "", []string{"准备开始"}
	job.logsExpireAt = time.Time{}
	job.Unlock()
	// 云服务存档必须写入受控目录，不能采信客户端传入的路径。
	req.Out = defaultOutputDir()
	go runArchive(req.URL, req.Out, req.Excel, req.Width, req.Height)
	w.WriteHeader(http.StatusAccepted)
}

func listenAddr() string {
	host := strings.TrimSpace(os.Getenv("REIMBURSEMENT_HOST"))
	if host == "" {
		host = "127.0.0.1"
	}
	port := strings.TrimSpace(os.Getenv("REIMBURSEMENT_PORT"))
	if port == "" {
		port = "8765"
	}
	return host + ":" + port
}

func defaultOutputDir() string {
	if out := strings.TrimSpace(os.Getenv("REIMBURSEMENT_OUTPUT_DIR")); out != "" {
		return out
	}
	return "outputs"
}

func outputRetention() time.Duration {
	return envDuration("REIMBURSEMENT_OUTPUT_RETENTION", 10*time.Minute)
}

func outputCleanupInterval() time.Duration {
	return envDuration("REIMBURSEMENT_OUTPUT_CLEANUP_INTERVAL", time.Minute)
}

const logRetention = 10 * time.Minute

func envDuration(name string, fallback time.Duration) time.Duration {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s=%q 无法解析，使用默认值 %s\n", name, raw, fallback)
		return fallback
	}
	return d
}

func startOutputCleanup() {
	retention := outputRetention()
	interval := outputCleanupInterval()
	if retention <= 0 || interval <= 0 {
		fmt.Println("outputs 自动清理已关闭")
		return
	}
	go func() {
		cleanupOutputDirOnce(defaultOutputDir(), retention, time.Now())
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for now := range ticker.C {
			cleanupOutputDirOnce(defaultOutputDir(), retention, now)
		}
	}()
}

// startLogCleanup 清理已结束任务的页面日志，避免云服务长期保留运行信息。
func startLogCleanup() {
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for now := range ticker.C {
			cleanupExpiredLogs(now)
		}
	}()
}

func cleanupExpiredLogs(now time.Time) {
	job.Lock()
	defer job.Unlock()
	if !job.Running && !job.logsExpireAt.IsZero() && !now.Before(job.logsExpireAt) {
		job.Logs = nil
		job.logsExpireAt = time.Time{}
	}
}

func cleanupOutputDirOnce(dir string, retention time.Duration, now time.Time) {
	dir = strings.TrimSpace(dir)
	if dir == "" || retention <= 0 {
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			fmt.Fprintf(os.Stderr, "清理 outputs 失败，无法读取 %s：%v\n", dir, err)
		}
		return
	}
	for _, entry := range entries {
		path := filepath.Join(dir, entry.Name())
		info, err := entry.Info()
		if err != nil {
			fmt.Fprintf(os.Stderr, "清理 outputs 跳过 %s：%v\n", path, err)
			continue
		}
		if now.Sub(info.ModTime()) < retention {
			continue
		}
		if err := os.RemoveAll(path); err != nil {
			fmt.Fprintf(os.Stderr, "清理 outputs 删除 %s 失败：%v\n", path, err)
		}
	}
}

func folderPickerEnabled() bool {
	return os.Getenv("REIMBURSEMENT_DISABLE_FOLDER_PICKER") != "1"
}

func runArchive(link, out, excel string, width, height float64) {
	args := []string{"--archive", "--archive-dir", out, "--url", link}
	resultFile, err := os.CreateTemp("", "reimbursement-result-*.json")
	if err != nil {
		finish(err)
		return
	}
	resultPath := resultFile.Name()
	_ = resultFile.Close()
	defer os.Remove(resultPath)
	args = append(args, "--output", resultPath)
	if width > 0 {
		args = append(args, "--screenshot-width-mm", fmt.Sprintf("%g", width))
	}
	if height > 0 {
		args = append(args, "--screenshot-height-mm", fmt.Sprintf("%g", height))
	}
	if strings.TrimSpace(excel) != "" {
		args = append(args, "--excel", strings.TrimSpace(excel))
	}
	cmd, err := archiveCommand(args)
	if err != nil {
		finish(err)
		return
	}
	cmd.Dir, cmd.Env = projectRoot(), os.Environ()
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		finish(err)
		return
	}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		finish(err)
		return
	}
	buf := make([]byte, 4096)
	for {
		n, readErr := pipe.Read(buf)
		if n > 0 {
			for _, line := range strings.Split(strings.TrimSpace(string(buf[:n])), "\n") {
				if line != "" {
					addLog(line)
				}
			}
		}
		if readErr != nil {
			break
		}
	}
	if err := cmd.Wait(); err != nil {
		finish(err)
		return
	}
	archivePath, err := archivePathFromResult(resultPath)
	if err != nil {
		finish(err)
		return
	}
	job.Lock()
	job.Running, job.Done = false, true
	job.ArchivePath = archivePath
	job.ZipName = filepath.Base(archivePath)
	job.DownloadURL = "/api/download"
	job.Logs = append(job.Logs, "完成，可下载 ZIP："+job.ZipName)
	job.logsExpireAt = time.Now().Add(logRetention)
	job.Unlock()
}

func archivePathFromResult(path string) (string, error) {
	var payload struct {
		ArchivePath string `json:"archive_path"`
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("读取归档结果失败：%w", err)
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return "", fmt.Errorf("解析归档结果失败：%w", err)
	}
	if strings.TrimSpace(payload.ArchivePath) == "" {
		return "", errors.New("归档完成但结果中没有 ZIP 路径")
	}
	info, err := os.Stat(payload.ArchivePath)
	if err != nil {
		return "", fmt.Errorf("确认 ZIP 文件失败：%w", err)
	}
	if info.IsDir() {
		return "", errors.New("归档结果不是 ZIP 文件")
	}
	return payload.ArchivePath, nil
}

func addLog(line string) {
	job.Lock()
	job.Logs = append(job.Logs, line)
	if len(job.Logs) > 200 {
		job.Logs = job.Logs[len(job.Logs)-200:]
	}
	job.Unlock()
}
func finish(err error) {
	job.Lock()
	job.Running, job.Done, job.Error = false, true, err.Error()
	job.Logs = append(job.Logs, "处理失败："+err.Error())
	job.logsExpireAt = time.Now().Add(logRetention)
	job.Unlock()
}
func statusHandler(w http.ResponseWriter, r *http.Request) {
	cleanupExpiredLogs(time.Now())
	job.Lock()
	defer job.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(job.jobState)
}

func downloadHandler(w http.ResponseWriter, r *http.Request) {
	job.Lock()
	path := job.ArchivePath
	name := job.ZipName
	ready := job.Done && job.Error == "" && path != ""
	job.Unlock()
	if !ready {
		http.Error(w, "ZIP 尚未生成", http.StatusNotFound)
		return
	}
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		http.Error(w, "ZIP 文件不存在", http.StatusNotFound)
		return
	}
	if name == "" {
		name = filepath.Base(path)
	}
	file, err := os.Open(path)
	if err != nil {
		http.Error(w, "ZIP 文件不存在", http.StatusNotFound)
		return
	}
	defer file.Close()

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", "attachment; filename*=UTF-8''"+url.QueryEscape(name))
	w.Header().Set("Content-Length", fmt.Sprintf("%d", info.Size()))
	if _, err := io.Copy(w, file); err != nil {
		fmt.Fprintf(os.Stderr, "下载 ZIP 中断，暂不清理 %s：%v\n", path, err)
		return
	}
	cleanupDownloadedArchive(path)
}

func cleanupDownloadedArchive(archivePath string) {
	if strings.TrimSpace(archivePath) == "" {
		return
	}
	paths := []string{archivePath}
	if filepath.Ext(archivePath) != "" {
		paths = append(paths, strings.TrimSuffix(archivePath, filepath.Ext(archivePath)))
	}
	for _, path := range paths {
		if err := os.RemoveAll(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			fmt.Fprintf(os.Stderr, "下载后清理 %s 失败：%v\n", path, err)
		}
	}
	job.Lock()
	if job.ArchivePath == archivePath {
		job.ArchivePath = ""
		job.DownloadURL = ""
		job.Logs = append(job.Logs, "ZIP 已下载，服务器临时文件已清理")
		job.logsExpireAt = time.Now().Add(logRetention)
	}
	job.Unlock()
}

func projectRoot() string {
	var candidates []string
	if configured := strings.TrimSpace(os.Getenv("REIMBURSEMENT_PROJECT_ROOT")); configured != "" {
		candidates = append(candidates, configured)
	}
	if cwd, err := os.Getwd(); err == nil {
		candidates = append(candidates, cwd)
	}
	if executable, err := os.Executable(); err == nil {
		dir := filepath.Dir(executable)
		candidates = append(candidates, dir, filepath.Dir(dir))
	}
	for _, candidate := range candidates {
		root, err := filepath.Abs(candidate)
		if err != nil {
			continue
		}
		if isProjectRoot(root) {
			return root
		}
	}
	if p, err := os.Getwd(); err == nil {
		return p
	}
	return "."
}

func isProjectRoot(dir string) bool {
	_, docsErr := os.Stat(filepath.Join(dir, "docs"))
	_, cmdErr := os.Stat(filepath.Join(dir, "cmd"))
	return docsErr == nil && cmdErr == nil
}

func openBrowser(target string) {
	var command *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		command = exec.Command("rundll32.exe", "url.dll,FileProtocolHandler", target)
	case "darwin":
		command = exec.Command("open", target)
	default:
		command = exec.Command("xdg-open", target)
	}
	_ = command.Start()
}

func chooseFolder() ([]byte, error) {
	switch runtime.GOOS {
	case "windows":
		script := `[Console]::OutputEncoding = New-Object System.Text.UTF8Encoding($false); Add-Type -AssemblyName System.Windows.Forms; $dialog = New-Object System.Windows.Forms.FolderBrowserDialog; $dialog.Description = '选择归档输出目录'; $dialog.ShowNewFolderButton = $true; if ($dialog.ShowDialog() -eq [System.Windows.Forms.DialogResult]::OK) { [Console]::Write($dialog.SelectedPath) }`
		out, err := exec.Command("powershell.exe", "-NoProfile", "-STA", "-ExecutionPolicy", "Bypass", "-Command", script).Output()
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(string(out)) == "" {
			return nil, fmt.Errorf("已取消选择目录")
		}
		return out, nil
	case "darwin":
		return exec.Command("osascript", "-e", `POSIX path of (choose folder with prompt "选择归档输出目录")`).Output()
	default:
		out, err := exec.Command("zenity", "--file-selection", "--directory", "--title=选择归档输出目录").Output()
		if err != nil {
			var exitErr *exec.ExitError
			switch {
			case errors.Is(err, exec.ErrNotFound):
				return nil, fmt.Errorf("当前系统不支持目录选择，请直接填写输出路径")
			case errors.As(err, &exitErr) && exitErr.ExitCode() == 1:
				return nil, fmt.Errorf("已取消选择目录")
			default:
				return nil, fmt.Errorf("zenity 选择失败：%w，请直接填写输出路径", err)
			}
		}
		if strings.TrimSpace(string(out)) == "" {
			return nil, fmt.Errorf("已取消选择目录")
		}
		return out, nil
	}
}

func archiveCommand(args []string) (*exec.Cmd, error) {
	if executable, err := os.Executable(); err == nil {
		dir := filepath.Dir(executable)
		names := []string{"feishu-probe"}
		if runtime.GOOS == "windows" {
			names = append(names, "feishu-probe.exe")
		}
		for _, name := range names {
			path := filepath.Join(dir, name)
			if info, statErr := os.Stat(path); statErr == nil && !info.IsDir() {
				return exec.Command(path, args...), nil
			}
		}
	}
	path, err := extractEmbeddedProbe()
	if err == nil {
		return exec.Command(path, args...), nil
	}
	if !errors.Is(err, errNoEmbeddedProbe) {
		return nil, fmt.Errorf("内置程序释放失败：%w", err)
	}
	goArgs := append([]string{"run", "./cmd/feishu-probe"}, args...)
	return exec.Command("go", goArgs...), nil
}
