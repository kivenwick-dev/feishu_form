// feishu-probe validates read-only access to a Feishu Wiki spreadsheet.
package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"github.com/xuri/excelize/v2"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const apiRoot = "https://open.feishu.cn/open-apis"

type apiEnvelope struct {
	Code int             `json:"code"`
	Msg  string          `json:"msg"`
	Data json.RawMessage `json:"data"`
}

type sourceLink struct {
	WikiToken string `json:"wiki_token"`
	SheetID   string `json:"sheet_id,omitempty"`
}

type wikiNode struct {
	ObjType  string `json:"obj_type"`
	ObjToken string `json:"obj_token"`
	Title    string `json:"title"`
}

type attachmentProbe struct {
	HasFileToken bool `json:"has_file_token"`
	HasURL       bool `json:"has_url"`
	HasPDFName   bool `json:"has_pdf_name"`
}

type attachment struct {
	FileToken string
	Name      string
	MIMEType  string
	Type      string
	Link      string
}

type downloadCheck struct {
	FileName string `json:"file_name"`
	MIMEType string `json:"mime_type"`
	Size     int    `json:"size"`
	IsPDF    bool   `json:"is_pdf"`
	Path     string `json:"path"`
}

type result struct {
	Source          sourceLink      `json:"source"`
	WikiNode        wikiNode        `json:"wiki_node"`
	Spreadsheet     json.RawMessage `json:"spreadsheet,omitempty"`
	Sheets          json.RawMessage `json:"sheets,omitempty"`
	Preview         json.RawMessage `json:"preview,omitempty"`
	AttachmentProbe attachmentProbe `json:"attachment_probe,omitempty"`
	DownloadCheck   *downloadCheck  `json:"download_check,omitempty"`
	ArchivePath     string          `json:"archive_path,omitempty"`
	Decision        string          `json:"decision"`
}

func parseLink(raw string) (sourceLink, error) {
	raw = strings.TrimSpace(raw)
	// 兼容从聊天窗口复制出的 Markdown 链接：[标题](https://...)
	if matches := regexp.MustCompile(`^\[[^\]]*\]\((https?://[^)]+)\)$`).FindStringSubmatch(raw); len(matches) == 2 {
		raw = matches[1]
	}
	raw = strings.ReplaceAll(raw, `\&`, "&")
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || !strings.HasSuffix(u.Hostname(), "feishu.cn") {
		return sourceLink{}, errors.New("请输入 feishu.cn 的 HTTPS Wiki/电子表格链接")
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	for i, part := range parts {
		if part == "wiki" && i+1 < len(parts) && parts[i+1] != "" {
			return sourceLink{WikiToken: parts[i+1], SheetID: u.Query().Get("sheet")}, nil
		}
	}
	return sourceLink{}, errors.New("URL 中未找到 /wiki/{token}")
}

func apiJSON(client *http.Client, method, path, token string, body any, output any) error {
	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(payload)
	}
	req, err := http.NewRequest(method, apiRoot+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return fmt.Errorf("HTTP %d 返回空响应；请检查网络代理/公司网关是否拦截 open.feishu.cn", resp.StatusCode)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, raw)
	}
	var envelope apiEnvelope
	if err := json.Unmarshal(raw, &envelope); err != nil {
		preview := string(raw)
		if len(preview) > 500 {
			preview = preview[:500] + "…"
		}
		return fmt.Errorf("飞书返回的内容不是 JSON：%w；响应片段：%q", err, preview)
	}
	if envelope.Code != 0 {
		return fmt.Errorf("飞书 API 错误 %d：%s", envelope.Code, envelope.Msg)
	}
	if output == nil {
		return nil
	}
	// 鉴权接口将 tenant_access_token 直接放在顶层；其他接口通常放在 data 内。
	if len(bytes.TrimSpace(envelope.Data)) == 0 || string(bytes.TrimSpace(envelope.Data)) == "null" {
		if err := json.Unmarshal(raw, output); err != nil {
			return fmt.Errorf("解析飞书顶层响应失败：%w", err)
		}
		return nil
	}
	if err := json.Unmarshal(envelope.Data, output); err != nil {
		return fmt.Errorf("解析飞书 data 响应失败：%w", err)
	}
	return nil
}

func tenantToken(client *http.Client, appID, appSecret string) (string, error) {
	var data struct {
		Token string `json:"tenant_access_token"`
	}
	err := apiJSON(client, http.MethodPost, "/auth/v3/tenant_access_token/internal", "", map[string]string{
		"app_id": appID, "app_secret": appSecret,
	}, &data)
	return data.Token, err
}

func findAttachments(raw json.RawMessage) []attachment {
	var root any
	if json.Unmarshal(raw, &root) != nil {
		return nil
	}
	var found []attachment
	var walk func(any)
	walk = func(value any) {
		switch typed := value.(type) {
		case []any:
			for _, child := range typed {
				walk(child)
			}
		case map[string]any:
			if token, ok := typed["fileToken"].(string); ok && token != "" {
				kind, _ := typed["type"].(string)
				name, _ := typed["text"].(string)
				mime, _ := typed["mimeType"].(string)
				link, _ := typed["link"].(string)
				found = append(found, attachment{FileToken: token, Name: name, MIMEType: mime, Type: kind, Link: link})
			}
			for _, child := range typed {
				walk(child)
			}
		}
	}
	walk(root)
	return found
}

func safeFileName(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "attachment"
	}
	value = regexp.MustCompile(`[\\/:*?"<>|\x00-\x1F]`).ReplaceAllString(value, "_")
	return strings.Trim(value, ". ")
}

// attachmentExtension chooses a useful extension when Feishu does not include
// one in the attachment display name.  This keeps image and PDF attachments
// usable after download instead of falling back to a generic .bin file.
func attachmentExtension(name, mime string, data []byte) string {
	if ext := filepath.Ext(name); ext != "" {
		return ext
	}
	m := strings.ToLower(strings.TrimSpace(strings.Split(mime, ";")[0]))
	switch m {
	case "application/pdf":
		return ".pdf"
	case "image/jpeg", "image/jpg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	case "image/bmp":
		return ".bmp"
	}
	// Content-Type is occasionally absent; detect the common signatures.
	if bytes.HasPrefix(data, []byte("%PDF-")) {
		return ".pdf"
	}
	if len(data) >= 8 && bytes.Equal(data[:8], []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}) {
		return ".png"
	}
	if len(data) >= 3 && bytes.Equal(data[:3], []byte{0xff, 0xd8, 0xff}) {
		return ".jpg"
	}
	return ".bin"
}

func cellText(value any) string {
	if value == nil {
		return ""
	}
	switch v := value.(type) {
	case string:
		return v
	case float64:
		return fmt.Sprintf("%v", v)
	case []any:
		var parts []string
		for _, item := range v {
			parts = append(parts, cellText(item))
		}
		return strings.Join(parts, "")
	case map[string]any:
		if text, ok := v["text"].(string); ok {
			return text
		}
		return ""
	default:
		return fmt.Sprint(value)
	}
}

func normalizeHeader(value string) string {
	return regexp.MustCompile(`[\s「」【】()（）_\-:/：·%％]`).ReplaceAllString(strings.ToLower(value), "")
}

func headerColumn(headers []any, aliases ...string) int {
	for i, raw := range headers {
		h := normalizeHeader(cellText(raw))
		if h == "" {
			continue
		}
		for _, alias := range aliases {
			a := normalizeHeader(alias)
			if strings.Contains(h, a) || strings.Contains(a, h) {
				return i
			}
		}
	}
	return -1
}

func attachmentKind(header string) string {
	h := normalizeHeader(header)
	if strings.Contains(h, "发票") || strings.Contains(h, "invoice") {
		return "发票"
	}
	if strings.Contains(h, "支付") || strings.Contains(h, "充值成功") || strings.Contains(h, "付款") {
		return "支付_账号充值成功截图"
	}
	if strings.Contains(h, "会员开通") || strings.Contains(h, "开通截图") || strings.Contains(h, "续费") {
		return "会员开通截图"
	}
	return "其他附件"
}

// excelPictureKind resolves the attachment category strictly from the
// picture's anchor column. Do not inherit a category from a neighboring
// column: a visually overlapping picture may belong to a different cell.
func excelPictureKind(headers []string, col int) string {
	if col < 0 {
		return "其他附件"
	}
	if col < len(headers) {
		if kind := attachmentKind(headers[col]); kind != "其他附件" {
			return kind
		}
	}
	return "其他附件"
}

// hasFileBytes reports whether a regular file in dir already contains data.
// Online attachments and Excel floating images can represent the same image
// with different names/extensions, so comparing bytes is more reliable than
// comparing filenames.
func hasFileBytes(dir string, data []byte) (bool, error) {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		content, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return false, err
		}
		if bytes.Equal(content, data) {
			return true, nil
		}
	}
	return false, nil
}

func countRegularFiles(dir string) (int, error) {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	count := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			count++
		}
	}
	return count, nil
}

func addMoney(total *big.Rat, value any) {
	s := strings.TrimSpace(cellText(value))
	s = strings.ReplaceAll(strings.ReplaceAll(s, ",", ""), "¥", "")
	if s == "" {
		return
	}
	if n, ok := new(big.Rat).SetString(s); ok {
		total.Add(total, n)
	}
}

func archiveFromValues(client *http.Client, token, spreadsheetName, outDir string, valueRanges json.RawMessage) (string, error) {
	var ranges []struct {
		Values [][]any `json:"values"`
	}
	if err := json.Unmarshal(valueRanges, &ranges); err != nil || len(ranges) == 0 {
		return "", errors.New("无法解析表格数据")
	}
	values := ranges[0].Values
	var headers []any
	headerRow := -1
	nameCol, amountCol, actualCol := -1, -1, -1
	for i, row := range values {
		n := headerColumn(row, "员工姓名", "姓名", "申请人", "报销人", "人员")
		d := headerColumn(row, "所属部门", "部门", "组织", "团队")
		a := headerColumn(row, "报销金额", "人民币金额", "金额")
		if n >= 0 && (d >= 0 || a >= 0) {
			headerRow, headers, nameCol, amountCol = i, row, n, a
			actualCol = headerColumn(row, "90%实际报销金额", "实际报销金额", "90%报销金额")
			break
		}
	}
	if headerRow < 0 {
		return "", errors.New("未找到员工姓名表头")
	}
	root := filepath.Join(outDir, safeFileName(spreadsheetName))
	for suffix := 1; ; suffix++ {
		if _, err := os.Stat(root); os.IsNotExist(err) {
			break
		}
		root = filepath.Join(outDir, fmt.Sprintf("%s_副本%d", safeFileName(spreadsheetName), suffix))
	}
	if err := os.MkdirAll(root, 0755); err != nil {
		return "", err
	}
	attachmentCols := map[int]bool{}
	for i, h := range headers {
		if attachmentKind(cellText(h)) != "其他附件" {
			attachmentCols[i] = true
		}
	}
	usedNames := map[string]int{}
	var failures []string
	amountTotal, actualTotal := new(big.Rat), new(big.Rat)
	rowItems := make([][]any, 0)
	for _, row := range values[headerRow+1:] {
		if nameCol >= len(row) || strings.TrimSpace(cellText(row[nameCol])) == "" {
			continue
		}
		name := strings.TrimSpace(cellText(row[nameCol]))
		if name == "合计" || name == "总计" {
			continue
		}
		rowItems = append(rowItems, row)
		usedNames[name]++
		folderName := safeFileName(name)
		if usedNames[name] > 1 {
			folderName = fmt.Sprintf("%s_%d", folderName, usedNames[name])
		}
		folder := filepath.Join(root, folderName)
		if err := os.MkdirAll(folder, 0755); err != nil {
			return "", err
		}
		for _, sub := range []string{"发票", "支付_账号充值成功截图", "会员开通截图"} {
			if err := os.MkdirAll(filepath.Join(folder, sub), 0755); err != nil {
				return "", err
			}
		}
		var info []string
		for i, h := range headers {
			if i >= len(row) || attachmentCols[i] || strings.TrimSpace(cellText(h)) == "" {
				continue
			}
			if text := strings.TrimSpace(cellText(row[i])); text != "" {
				info = append(info, fmt.Sprintf("%s: %s", cellText(h), text))
			}
		}
		if err := os.WriteFile(filepath.Join(folder, "报销信息.txt"), []byte(strings.Join(info, "\n")+"\n"), 0600); err != nil {
			return "", err
		}
		if amountCol >= 0 && amountCol < len(row) {
			addMoney(amountTotal, row[amountCol])
		}
		if actualCol >= 0 && actualCol < len(row) {
			addMoney(actualTotal, row[actualCol])
		}
		for i := range row {
			if !attachmentCols[i] {
				continue
			}
			items := findAttachments(mustJSON(row[i]))
			for j, att := range items {
				if att.FileToken == "" {
					continue
				}
				kind := attachmentKind(cellText(headers[i]))
				if kind == "其他附件" {
					continue
				}
				data, mime, err := downloadAttachment(client, token, att)
				if err != nil {
					failures = append(failures, fmt.Sprintf("%s\t%s\t%s\t%s", name, kind, att.Name, err))
					continue
				}
				fn := safeFileName(att.Name)
				if filepath.Ext(fn) == "" {
					fn += attachmentExtension(fn, firstNonEmpty(mime, att.MIMEType), data)
				}
				target := filepath.Join(folder, kind, fn)
				if j > 0 {
					target = filepath.Join(folder, kind, fmt.Sprintf("%d_%s", j+1, fn))
				}
				if err := os.WriteFile(target, data, 0600); err != nil {
					return "", err
				}
			}
		}
		fmt.Printf("已归档：%s\n", name)
	}
	summary := fmt.Sprintf("源表：%s\n员工人数：%d\n报销金额总和：%s\n90%%实际报销金额总和：%s\n", spreadsheetName, len(rowItems), amountTotal.FloatString(2), actualTotal.FloatString(2))
	if err := os.WriteFile(filepath.Join(root, "汇总.txt"), []byte(summary), 0600); err != nil {
		return "", err
	}
	if len(failures) > 0 {
		_ = os.WriteFile(filepath.Join(root, "失败清单.txt"), []byte("员工\t附件类型\t文件名\t错误\n"+strings.Join(failures, "\n")+"\n"), 0600)
	}
	zipPath := root + ".zip"
	zf, err := os.Create(zipPath)
	if err != nil {
		return "", err
	}
	zw := zip.NewWriter(zf)
	err = filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil || info.IsDir() {
			return walkErr
		}
		rel, _ := filepath.Rel(outDir, path)
		w, e := zw.Create(rel)
		if e != nil {
			return e
		}
		b, e := os.ReadFile(path)
		if e == nil {
			_, e = w.Write(b)
		}
		return e
	})
	if err == nil {
		err = zw.Close()
	}
	_ = zf.Close()
	if err != nil {
		return "", err
	}
	return zipPath, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func firstSheetID(raw json.RawMessage) string {
	var sheets []struct {
		SheetID string `json:"sheet_id"`
		ID      string `json:"id"`
	}
	if json.Unmarshal(raw, &sheets) != nil || len(sheets) == 0 {
		return ""
	}
	return firstNonEmpty(sheets[0].SheetID, sheets[0].ID)
}

func mustJSON(value any) json.RawMessage { b, _ := json.Marshal(value); return b }

func downloadMedia(client *http.Client, token, fileToken string) ([]byte, string, error) {
	return downloadURL(client, token, apiRoot+"/drive/v1/medias/"+url.PathEscape(fileToken)+"/download")
}

func downloadURL(client *http.Client, token, targetURL string) ([]byte, string, error) {
	var lastErr error
	for attempt := 1; attempt <= 3; attempt++ {
		req, err := http.NewRequest(http.MethodGet, targetURL, nil)
		if err != nil {
			return nil, "", err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
		} else {
			data, readErr := io.ReadAll(resp.Body)
			contentType := resp.Header.Get("Content-Type")
			resp.Body.Close()
			if readErr == nil && resp.StatusCode >= 200 && resp.StatusCode < 300 {
				return data, contentType, nil
			}
			preview := string(data)
			if len(preview) > 500 {
				preview = preview[:500] + "…"
			}
			if readErr != nil {
				lastErr = readErr
			} else {
				lastErr = fmt.Errorf("下载接口 HTTP %d：%s", resp.StatusCode, preview)
			}
		}
		if attempt < 3 {
			time.Sleep(time.Duration(attempt*300) * time.Millisecond)
		}
	}
	return nil, "", fmt.Errorf("重试 3 次后仍失败：%w", lastErr)
}

func downloadAttachment(client *http.Client, token string, att attachment) ([]byte, string, error) {
	data, mime, err := downloadMedia(client, token, att.FileToken)
	if err == nil {
		return data, mime, nil
	}
	// Embedded images expose an internal stream URL; use it as a fallback when
	// the generic media endpoint does not accept their file token.
	if att.Link != "" {
		if fallbackData, fallbackMIME, fallbackErr := downloadURL(client, token, att.Link); fallbackErr == nil {
			return fallbackData, fallbackMIME, nil
		}
	}
	return nil, "", err
}

func supplementExcelImages(excelPath, archivePath string) error {
	root := strings.TrimSuffix(archivePath, filepath.Ext(archivePath))
	f, err := excelize.OpenFile(excelPath)
	if err != nil {
		return err
	}
	defer f.Close()
	sheet := f.GetSheetName(0)
	rows, err := f.GetRows(sheet)
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		return errors.New("Excel 工作表为空")
	}
	headerRow, nameCol := -1, -1
	for i, row := range rows {
		for j, v := range row {
			if normalizeHeader(v) == "员工姓名" || normalizeHeader(v) == "姓名" {
				headerRow, nameCol = i, j
				break
			}
		}
		if headerRow >= 0 {
			break
		}
	}
	if headerRow < 0 {
		return errors.New("Excel 中未找到员工姓名列")
	}
	used := map[string]int{}
	rowPerson := map[int]string{}
	for i := headerRow + 1; i < len(rows); i++ {
		if nameCol >= len(rows[i]) || strings.TrimSpace(rows[i][nameCol]) == "" {
			continue
		}
		n := safeFileName(rows[i][nameCol])
		used[n]++
		if used[n] > 1 {
			n = fmt.Sprintf("%s_%d", n, used[n])
		}
		rowPerson[i+1] = n
	}
	pictureCells, err := f.GetPictureCells(sheet)
	if err != nil {
		return err
	}
	imageCount := 0
	perPerson := map[string]int{}
	perKind := map[string]map[string]int{}
	perKindFound := map[string]map[string]int{}
	for _, person := range rowPerson {
		perKind[person] = map[string]int{"发票": 0, "支付_账号充值成功截图": 0, "会员开通截图": 0}
		perKindFound[person] = map[string]int{"发票": 0, "支付_账号充值成功截图": 0, "会员开通截图": 0}
	}
	fmt.Printf("Excel 浮动图片锚点共 %d 个\n", len(pictureCells))
	for _, cell := range pictureCells {
		col, rowNum, err := excelize.CellNameToCoordinates(cell)
		if err != nil {
			continue
		}
		col--
		rowNum--
		person, ok := rowPerson[rowNum+1]
		if !ok || col < 0 {
			fmt.Printf("跳过图片：单元格 %s，无法匹配员工行或列\n", cell)
			continue
		}
		pics, picErr := f.GetPictures(sheet, cell)
		if picErr != nil {
			fmt.Printf("读取图片失败：单元格 %s：%v\n", cell, picErr)
		}
		if len(pics) == 0 {
			fmt.Printf("跳过图片：单元格 %s 未读取到图片数据\n", cell)
		}
		for _, pic := range pics {
			kind := excelPictureKind(rows[headerRow], col)
			if kind == "其他附件" {
				var header any
				if col < len(rows[headerRow]) {
					header = rows[headerRow][col]
				}
				fmt.Printf("跳过图片：%s 行=%d 列=%d，表头=%q 未识别为附件列\n", cell, rowNum, col+1, header)
				continue
			}
			perKindFound[person][kind]++
			dir := filepath.Join(root, person, kind)
			if err := os.MkdirAll(dir, 0755); err != nil {
				return err
			}
			duplicate, err := hasFileBytes(dir, pic.File)
			if err != nil {
				return err
			}
			if duplicate {
				fmt.Printf("跳过重复图片：%s -> %s/%s\n", cell, person, kind)
				continue
			}
			ext := pic.Extension
			if ext == "" {
				ext = ".bin"
			}
			// Multiple pictures may share one anchor cell. Include a global
			// sequence so later images never overwrite earlier ones.
			name := fmt.Sprintf("Excel浮动图片_%d%s", imageCount+1, ext)
			if err := os.WriteFile(filepath.Join(dir, name), pic.File, 0600); err != nil {
				return err
			}
			imageCount++
			perPerson[person]++
			perKind[person][kind]++
			fmt.Printf("Excel 图片：%s -> %s/%s/%s\n", cell, person, kind, name)
		}
	}
	f.Close()
	if imageCount == 0 {
		return errors.New("Excel 中未找到可归档的浮动图片")
	}
	for _, person := range rowPerson {
		counts := make(map[string]int, 3)
		for _, kind := range []string{"发票", "支付_账号充值成功截图", "会员开通截图"} {
			count, err := countRegularFiles(filepath.Join(root, person, kind))
			if err != nil {
				return err
			}
			counts[kind] = count
		}
		fmt.Printf("图片统计（文件夹实际数量）：%s 发票=%d 支付截图=%d 会员截图=%d\n",
			person, counts["发票"], counts["支付_账号充值成功截图"], counts["会员开通截图"])
	}
	return rebuildZip(root, filepath.Dir(archivePath))
}

func rebuildZip(root, outDir string) error {
	zf, err := os.Create(root + ".zip")
	if err != nil {
		return err
	}
	zw := zip.NewWriter(zf)
	err = filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil || info.IsDir() {
			return walkErr
		}
		rel, _ := filepath.Rel(outDir, path)
		w, e := zw.Create(rel)
		if e != nil {
			return e
		}
		b, e := os.ReadFile(path)
		if e != nil {
			return e
		}
		_, e = w.Write(b)
		return e
	})
	if closeErr := zw.Close(); err == nil {
		err = closeErr
	}
	if closeErr := zf.Close(); err == nil {
		err = closeErr
	}
	return err
}

func probe(rawURL, appID, appSecret, downloadDir, archiveDir, excelPath string) (result, error) {
	source, err := parseLink(rawURL)
	if err != nil {
		return result{}, err
	}
	client := &http.Client{Timeout: 30 * time.Second}
	token, err := tenantToken(client, appID, appSecret)
	if err != nil {
		return result{}, fmt.Errorf("获取访问令牌失败：%w", err)
	}

	var wikiData struct {
		Node wikiNode `json:"node"`
	}
	if err := apiJSON(client, http.MethodGet, "/wiki/v2/spaces/get_node?token="+url.QueryEscape(source.WikiToken), token, nil, &wikiData); err != nil {
		return result{}, fmt.Errorf("解析 Wiki 节点失败：%w", err)
	}
	r := result{Source: source, WikiNode: wikiData.Node}
	if wikiData.Node.ObjType != "sheet" {
		r.Decision = "该 Wiki 节点不是 Sheets；若为 bitable，应改走 field/record/attachment API。"
		return r, nil
	}

	var spreadsheet struct {
		Spreadsheet json.RawMessage `json:"spreadsheet"`
	}
	if err := apiJSON(client, http.MethodGet, "/sheets/v3/spreadsheets/"+url.PathEscape(wikiData.Node.ObjToken), token, nil, &spreadsheet); err != nil {
		return result{}, fmt.Errorf("读取电子表格信息失败：%w", err)
	}
	r.Spreadsheet = spreadsheet.Spreadsheet

	var sheetData struct {
		Sheets json.RawMessage `json:"sheets"`
	}
	if err := apiJSON(client, http.MethodGet, "/sheets/v3/spreadsheets/"+url.PathEscape(wikiData.Node.ObjToken)+"/sheets/query", token, nil, &sheetData); err != nil {
		return result{}, fmt.Errorf("获取工作表列表失败：%w", err)
	}
	r.Sheets = sheetData.Sheets
	if source.SheetID == "" {
		// Wiki links copied from Feishu often omit ?sheet=... . In that case,
		// use the first visible sheet returned by the Sheets API.
		source.SheetID = firstSheetID(sheetData.Sheets)
		if source.SheetID == "" {
			return result{}, errors.New("链接未包含 sheet 参数，且无法从工作表列表确定默认工作表")
		}
		r.Source = source
	}

	rangeRef := source.SheetID + "!A1:AZ1000"
	query := url.Values{"ranges": {rangeRef}}.Encode()
	var preview struct {
		ValueRanges json.RawMessage `json:"valueRanges"`
	}
	if err := apiJSON(client, http.MethodGet, "/sheets/v2/spreadsheets/"+url.PathEscape(wikiData.Node.ObjToken)+"/values_batch_get?"+query, token, nil, &preview); err != nil {
		return result{}, fmt.Errorf("读取单元格预览失败：%w", err)
	}
	r.Preview = preview.ValueRanges
	text := string(preview.ValueRanges)
	r.AttachmentProbe = attachmentProbe{
		HasFileToken: regexp.MustCompile(`(?i)(file_token|fileToken)`).MatchString(text),
		HasURL:       regexp.MustCompile(`(?i)https?://`).MatchString(text),
		HasPDFName:   regexp.MustCompile(`(?i)\.pdf\b`).MatchString(text),
	}
	if r.AttachmentProbe.HasFileToken {
		r.Decision = "检测到 file_token：下一步验证 Drive 媒体下载，然后接入完整归档。"
	} else {
		r.Decision = "Values API 中未发现 file_token：需要验证 Sheets 富文本/附件专用接口后才能承诺下载 PDF。"
	}
	if downloadDir != "" {
		attachments := findAttachments(preview.ValueRanges)
		var selected *attachment
		for i := range attachments {
			if attachments[i].Type == "attachment" {
				selected = &attachments[i]
				break
			}
		}
		if selected == nil {
			return result{}, errors.New("预览中没有可下载的普通附件 file_token")
		}
		data, returnedMIME, err := downloadAttachment(client, token, *selected)
		if err != nil {
			return result{}, fmt.Errorf("下载首个附件失败：%w", err)
		}
		name := safeFileName(selected.Name)
		if filepath.Ext(name) == "" && selected.MIMEType == "application/pdf" {
			name += ".pdf"
		}
		if err := os.MkdirAll(downloadDir, 0755); err != nil {
			return result{}, err
		}
		path := filepath.Join(downloadDir, name)
		if err := os.WriteFile(path, data, 0600); err != nil {
			return result{}, err
		}
		r.DownloadCheck = &downloadCheck{FileName: name, MIMEType: returnedMIME, Size: len(data), IsPDF: bytes.HasPrefix(data, []byte("%PDF-")), Path: path}
		r.Decision = "已成功下载一份原始附件；可以进入批量映射和归档实现。"
	}
	if archiveDir != "" {
		name := wikiData.Node.Title
		if name == "" {
			name = "飞书报销表"
		}
		path, err := archiveFromValues(client, token, name, archiveDir, preview.ValueRanges)
		if err != nil {
			return result{}, fmt.Errorf("批量归档失败：%w", err)
		}
		r.ArchivePath = path
		if strings.TrimSpace(excelPath) != "" {
			if err := supplementExcelImages(excelPath, path); err != nil {
				return result{}, fmt.Errorf("补充 Excel 浮动图片失败：%w", err)
			}
		}
		r.Decision = "已完成批量归档和 ZIP。"
	}
	return r, nil
}

func main() {
	defaultOutput := filepath.Join("outputs", "feishu_probe_result.json")
	urlFlag := flag.String("url", "", "飞书 Wiki 电子表格链接")
	appID := flag.String("app-id", os.Getenv("FEISHU_APP_ID"), "飞书 App ID")
	appSecret := flag.String("app-secret", os.Getenv("FEISHU_APP_SECRET"), "飞书 App Secret")
	output := flag.String("output", defaultOutput, "探测结果 JSON 文件")
	downloadOne := flag.Bool("download-one", false, "下载预览中的第一份普通附件进行验证")
	downloadDir := flag.String("download-dir", filepath.Join("outputs", "attachment_download_check"), "单附件验证下载目录")
	archive := flag.Bool("archive", false, "批量下载附件并生成员工归档 ZIP")
	archiveDir := flag.String("archive-dir", "outputs", "批量归档输出目录")
	excelPath := flag.String("excel", "", "可选：本地 Excel 路径，用于补充浮动图片")
	flag.Parse()
	if *urlFlag == "" || *appID == "" || *appSecret == "" {
		fmt.Fprintln(os.Stderr, "请提供 --url，并通过 FEISHU_APP_ID / FEISHU_APP_SECRET 或参数传入应用凭据。")
		os.Exit(2)
	}
	destination := ""
	if *downloadOne {
		destination = *downloadDir
	}
	archiveDestination := ""
	if *archive {
		archiveDestination = *archiveDir
	}
	r, err := probe(*urlFlag, *appID, *appSecret, destination, archiveDestination, *excelPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "探测失败：", err)
		os.Exit(2)
	}
	encoded, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if err := os.MkdirAll(filepath.Dir(*output), 0755); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if err := os.WriteFile(*output, encoded, 0600); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	fmt.Printf("探测完成：%s\n结果：%s\n", r.Decision, *output)
}
