package main

import (
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type jobState struct {
	Running bool     `json:"running"`
	Done    bool     `json:"done"`
	Error   string   `json:"error,omitempty"`
	Logs    []string `json:"logs"`
}

var job = struct {
	sync.Mutex
	jobState
}{jobState: jobState{Logs: []string{"等待开始"}}}

const page = `<!doctype html><html lang="zh-CN"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>飞书报销表归档</title>
<style>body{font-family:-apple-system,BlinkMacSystemFont,"PingFang SC",sans-serif;margin:0;padding:0 34px 48px 300px;color:#172033;background:#f5f7fb}main{max-width:860px;margin:42px auto}.card{background:#fff;border:1px solid #e7eaf0;border-radius:16px;padding:30px 34px;box-shadow:0 10px 30px #20304d0d}h1{font-size:28px;margin:0 0 8px}label{display:block;margin:22px 0 8px;font-weight:650;font-size:14px}input{width:100%;box-sizing:border-box;padding:12px 13px;border:1px solid #d5dbe5;border-radius:9px;font-size:14px;background:#fff}input:focus{outline:3px solid #1677ff22;border-color:#1677ff}.row{display:flex;align-items:center}.row input{flex:1}button{margin-top:24px;padding:12px 24px;border:0;border-radius:9px;background:#1677ff;color:#fff;font-size:15px;font-weight:600;cursor:pointer;transition:.15s}button:hover{background:#0f63d8;transform:translateY(-1px)}button.small{margin:0 0 0 10px;padding:10px 14px;font-size:13px;background:#eef4ff;color:#145dcc}button.small:hover{background:#dceaff}button:disabled{background:#aeb8c8;cursor:not-allowed;transform:none}progress{width:100%;height:12px;margin-top:24px;accent-color:#1677ff}#status{margin-top:18px;padding:13px 15px;border-radius:9px;background:#f6f8fb;white-space:pre-wrap;color:#526078;line-height:1.7;font-size:13px;min-height:22px}.hint{color:#718096;font-size:13px}.drawer{position:fixed;left:0;top:0;width:260px;height:100vh;box-sizing:border-box;padding:28px 18px;background:#fff;border-right:1px solid #e3e7ef;overflow:auto;box-shadow:4px 0 18px #20304d08}.drawer h2{font-size:18px;margin:0 0 6px}.drawer .sub{font-size:12px;color:#8792a5;margin-bottom:18px}.drawer a{display:block;padding:10px 11px;color:#245fc2;text-decoration:none;border-radius:8px;font-size:13px;cursor:pointer}.drawer a:hover{background:#eef4ff}.modal{display:none;position:fixed;inset:0;background:#17203366;z-index:10;align-items:center;justify-content:center;padding:24px}.modal.show{display:flex}.modalbox{background:#fff;border-radius:14px;width:min(760px,94vw);max-height:82vh;display:flex;flex-direction:column;box-shadow:0 20px 60px #17203344}.modalhead{padding:16px 20px;border-bottom:1px solid #e8ebf1;display:flex;justify-content:space-between;align-items:center;font-weight:650}.close{margin:0;padding:2px 9px;background:transparent;color:#65738a;font-size:24px;font-weight:400}.close:hover{background:#f0f2f6;color:#172033;transform:none}.modalbody{padding:22px;overflow:auto;white-space:pre-wrap;line-height:1.75;color:#39465a;font-size:14px}</style>
<div class="drawer"><h2>文件抽屉</h2><div class="sub">点击文件名查看使用说明</div><div id="docs">正在读取说明…</div></div><main><div class="card"><h1>飞书报销表归档工具</h1><p class="hint">线上表格下载 PDF，本地 Excel 补充浮动图片，最后合并生成员工目录和 ZIP。</p>
<label>飞书表格链接</label><input id="url" placeholder="粘贴 https://...feishu.cn/wiki/... 链接"><label>本地 Excel 文件（可选）</label><input id="excel" type="file" accept=".xlsx,.xlsm"><label>输出目录</label><div class="row"><input id="out" value="outputs"><button class="small" onclick="chooseOut()">选择目录</button></div><button id="go" onclick="start()">开始归档</button><progress id="bar" value="0" max="100"></progress><div id="status">等待开始</div></div></main><div id="modal" class="modal" onclick="if(event.target===this)closeDoc()"><div class="modalbox"><div class="modalhead"><span id="modalTitle">使用说明</span><button class="close" onclick="closeDoc()">×</button></div><div id="modalBody" class="modalbody"></div></div></div>
<script>let timer;async function chooseOut(){let r=await fetch('/api/choose-folder',{method:'POST'});if(r.ok)document.getElementById('out').value=(await r.json()).path}async function loadDocs(){let r=await fetch('/api/docs');let d=await r.json();document.getElementById('docs').innerHTML=d.map(x=>'<a onclick="openDoc('+JSON.stringify(x)+')">'+x+'</a>').join('')||'<span class="hint">暂无说明文件</span>'}async function openDoc(name){let r=await fetch('/api/docs/read?name='+encodeURIComponent(name));let body=await r.text();document.getElementById('modalTitle').textContent=name;document.getElementById('modalBody').textContent=r.ok?body:'读取失败：'+body;document.getElementById('modal').classList.add('show')}function closeDoc(){document.getElementById('modal').classList.remove('show')}document.addEventListener('keydown',e=>{if(e.key==='Escape')closeDoc()});async function start(){let b=document.getElementById('go'),u=document.getElementById('url').value,o=document.getElementById('out').value;b.disabled=true;document.getElementById('bar').value=5;let x='';let f=document.getElementById('excel').files[0];if(f){let fd=new FormData();fd.append('file',f);let up=await fetch('/api/upload-excel',{method:'POST',body:fd});if(!up.ok){document.getElementById('status').textContent=await up.text();b.disabled=false;return}x=(await up.json()).path}let r=await fetch('/api/start',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({url:u,excel:x,out:o})});if(!r.ok){document.getElementById('status').textContent=await r.text();b.disabled=false;return}timer=setInterval(poll,700)}async function poll(){let r=await fetch('/api/status'),s=await r.json();document.getElementById('status').textContent=s.logs.join('\n');document.getElementById('bar').value=s.done?100:(s.running?Math.min(95,10+s.logs.length*4):0);if(s.done||s.error){clearInterval(timer);document.getElementById('go').disabled=false;if(s.error)document.getElementById('bar').value=0}}loadDocs();poll()</script></html>`

func main() {
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		_ = template.Must(template.New("p").Parse(page)).Execute(w, nil)
	})
	http.HandleFunc("/api/start", startHandler)
	http.HandleFunc("/api/upload-excel", uploadExcelHandler)
	http.HandleFunc("/api/choose-folder", chooseFolderHandler)
	http.HandleFunc("/api/docs", docsHandler)
	http.Handle("/docs/", http.StripPrefix("/docs/", http.FileServer(http.Dir("docs"))))
	http.HandleFunc("/api/status", statusHandler)
	addr := "127.0.0.1:8765"
	if os.Getenv("NO_BROWSER_OPEN") != "1" {
		go func() { time.Sleep(500 * time.Millisecond); _ = exec.Command("open", "http://"+addr).Start() }()
	}
	fmt.Println("飞书报销归档工具已启动：http://" + addr)
	if err := http.ListenAndServe(addr, nil); err != nil {
		panic(err)
	}
}

func chooseFolderHandler(w http.ResponseWriter, r *http.Request) {
	out, err := exec.Command("osascript", "-e", `POSIX path of (choose folder with prompt "选择归档输出目录")`).Output()
	if err != nil {
		http.Error(w, "已取消选择目录", http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"path": strings.TrimSpace(string(out))})
}

func docsHandler(w http.ResponseWriter, r *http.Request) {
	entries, err := os.ReadDir("docs")
	if err != nil {
		_ = os.MkdirAll("docs", 0755)
		entries = nil
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() {
			names = append(names, e.Name())
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(names)
}

func docReadHandler(w http.ResponseWriter, r *http.Request) {
	name := filepath.Base(r.URL.Query().Get("name"))
	if name == "." || name == "" {
		http.Error(w, "缺少文件名", 400)
		return
	}
	b, err := os.ReadFile(filepath.Join("docs", name))
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
	var req struct{ URL, Out, Excel string }
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
	job.Running, job.Done, job.Error, job.Logs = true, false, "", []string{"准备开始"}
	job.Unlock()
	if strings.TrimSpace(req.Out) == "" {
		req.Out = "outputs"
	}
	go runArchive(req.URL, req.Out, req.Excel)
	w.WriteHeader(http.StatusAccepted)
}

func runArchive(link, out, excel string) {
	args := []string{"run", "./cmd/feishu-probe", "--archive", "--archive-dir", out, "--url", link}
	if strings.TrimSpace(excel) != "" {
		args = append(args, "--excel", strings.TrimSpace(excel))
	}
	cmd := exec.Command("go", args...)
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
	job.Lock()
	job.Running, job.Done = false, true
	job.Logs = append(job.Logs, "完成，可在输出目录查看 ZIP")
	job.Unlock()
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
	job.Unlock()
}
func statusHandler(w http.ResponseWriter, r *http.Request) {
	job.Lock()
	defer job.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(job.jobState)
}
func projectRoot() string {
	p, _ := os.Getwd()
	if filepath.Base(p) == "feishu-web" {
		return filepath.Clean(filepath.Join(p, "../.."))
	}
	return p
}
