// feishu-probe validates read-only access to a Feishu Wiki spreadsheet.
package main

import (
	"archive/zip"
	"bytes"
	"compress/zlib"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"github.com/xuri/excelize/v2"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"math"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"reimbursement-archiver/internal/credential"
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

const screenshotFolder = "截图"

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

type screenshotPDFOptions struct {
	WidthMM  float64
	HeightMM float64
}

func outputAttachmentKind(kind string) string {
	if kind == "发票" {
		return "发票"
	}
	if kind == "支付_账号充值成功截图" || kind == "会员开通截图" {
		return screenshotFolder
	}
	return kind
}

// attachmentTarget 返回附件落盘路径。所有截图类附件统一写入“截图”文件夹，
// 同名文件会自动追加序号，避免同一员工的多张截图互相覆盖。
func attachmentTarget(personFolder, kind, fileName string) string {
	dir := filepath.Join(personFolder, outputAttachmentKind(kind))
	return filepath.Join(dir, uniqueFileName(dir, fileName))
}

// uniqueFileName 在 dir 中返回不与现有文件冲突的名称；冲突时在扩展名前追加 _2、_3……
func uniqueFileName(dir, name string) string {
	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	candidate := name
	for i := 2; ; i++ {
		if _, err := os.Stat(filepath.Join(dir, candidate)); err != nil {
			return candidate
		}
		candidate = fmt.Sprintf("%s_%d%s", base, i, ext)
	}
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

func createScreenshotPDF(folder, outputPath string, options screenshotPDFOptions) error {
	if options.WidthMM <= 0 || options.HeightMM <= 0 {
		return errors.New("截图 PDF 图片宽度和高度必须大于 0")
	}
	const (
		pageWidth  = 595.275590551
		pageHeight = 841.88976378
		mmToPoints = 72 / 25.4
	)
	boxWidth := options.WidthMM * mmToPoints
	boxHeight := options.HeightMM * mmToPoints
	if boxWidth > pageWidth || boxHeight > pageHeight {
		return fmt.Errorf("截图尺寸 %.2fmm x %.2fmm 超出 A4 页面", options.WidthMM, options.HeightMM)
	}

	entries, err := os.ReadDir(folder)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var images []pdfImage
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		path := filepath.Join(folder, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		imageData, err := makePDFImage(data)
		if errors.Is(err, errUnsupportedPDFImage) {
			continue
		}
		if err != nil {
			return fmt.Errorf("读取截图 %s 失败：%w", entry.Name(), err)
		}
		images = append(images, imageData)
	}
	if len(images) == 0 {
		return nil
	}
	return os.WriteFile(outputPath, buildImagePDF(images, pageWidth, pageHeight, boxWidth, boxHeight), 0600)
}

var errUnsupportedPDFImage = errors.New("不是支持的图片格式")

var screenshotWidthMM = 180.0
var screenshotHeightMM = 260.0

type pdfImage struct {
	Width       int
	Height      int
	ColorSpace  string
	BitsPerComp int
	Filter      string
	Data        []byte
}

func makePDFImage(data []byte) (pdfImage, error) {
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return pdfImage{}, errUnsupportedPDFImage
	}
	switch format {
	case "jpeg":
		return pdfImage{
			Width: config.Width, Height: config.Height,
			ColorSpace: "/DeviceRGB", BitsPerComp: 8, Filter: "/DCTDecode", Data: data,
		}, nil
	case "png", "gif":
		decoded, _, err := image.Decode(bytes.NewReader(data))
		if err != nil {
			return pdfImage{}, err
		}
		raw := make([]byte, 0, config.Width*config.Height*3)
		for y := 0; y < config.Height; y++ {
			for x := 0; x < config.Width; x++ {
				r, g, b, _ := decoded.At(x, y).RGBA()
				raw = append(raw, byte(r>>8), byte(g>>8), byte(b>>8))
			}
		}
		var compressed bytes.Buffer
		writer := zlib.NewWriter(&compressed)
		if _, err := writer.Write(raw); err != nil {
			return pdfImage{}, err
		}
		if err := writer.Close(); err != nil {
			return pdfImage{}, err
		}
		return pdfImage{
			Width: config.Width, Height: config.Height,
			ColorSpace: "/DeviceRGB", BitsPerComp: 8, Filter: "/FlateDecode", Data: compressed.Bytes(),
		}, nil
	default:
		return pdfImage{}, errUnsupportedPDFImage
	}
}

func buildImagePDF(images []pdfImage, pageWidth, pageHeight, boxWidth, boxHeight float64) []byte {
	// 对象编号固定：1 = 页面树，2 = 目录，其后每张图片占用“图片 / 内容 / 页面”三个对象。
	const (
		pagesObject   = 1
		catalogObject = 2
		firstImage    = 3
	)
	w := newPDFWriter(2 + len(images)*3)
	w.buf.WriteString("%PDF-1.4\n%\xE2\xE3\xCF\xD3\n")

	kids := make([]string, 0, len(images))
	for i, img := range images {
		imageObject := firstImage + i*3
		contentObject := imageObject + 1
		pageObject := imageObject + 2

		w.stream(imageObject, fmt.Sprintf(
			"/Type /XObject /Subtype /Image /Width %d /Height %d /ColorSpace %s /BitsPerComponent %d /Filter %s",
			img.Width, img.Height, img.ColorSpace, img.BitsPerComp, img.Filter,
		), img.Data)
		w.stream(contentObject, "", buildPageContent(pageWidth, pageHeight, boxWidth, boxHeight, img.Width, img.Height, imageObject))
		w.object(pageObject, fmt.Sprintf(
			"<< /Type /Page /Parent %d 0 R /MediaBox [0 0 %.4f %.4f] /Resources << /ProcSet [/PDF /ImageC] /XObject << /Im%d %d 0 R >> >> /Contents %d 0 R >>",
			pagesObject, pageWidth, pageHeight, imageObject, imageObject, contentObject,
		))
		kids = append(kids, fmt.Sprintf("%d 0 R", pageObject))
	}
	w.object(pagesObject, fmt.Sprintf("<< /Type /Pages /Count %d /Kids [%s] >>", len(images), strings.Join(kids, " ")))
	w.object(catalogObject, fmt.Sprintf("<< /Type /Catalog /Pages %d 0 R >>", pagesObject))
	return w.finish(catalogObject)
}

// pdfWriter 按对象编号记录字节偏移，保证生成的 xref 表与对象一一对应。
type pdfWriter struct {
	buf     bytes.Buffer
	offsets []int // offsets[n] 为对象 n 的偏移；偏移 0 为固定空闲项。
}

func newPDFWriter(objectCount int) *pdfWriter {
	return &pdfWriter{offsets: make([]int, objectCount+1)}
}

func (w *pdfWriter) object(number int, body string) {
	w.offsets[number] = w.buf.Len()
	fmt.Fprintf(&w.buf, "%d 0 obj\n%s\nendobj\n", number, body)
}

func (w *pdfWriter) stream(number int, dict string, data []byte) {
	w.offsets[number] = w.buf.Len()
	if dict = strings.TrimSpace(dict); dict != "" {
		dict = " " + dict
	}
	fmt.Fprintf(&w.buf, "%d 0 obj\n<<%s /Length %d >>\nstream\n", number, dict, len(data))
	w.buf.Write(data)
	w.buf.WriteString("\nendstream\nendobj\n")
}

func (w *pdfWriter) finish(rootObject int) []byte {
	xref := w.buf.Len()
	fmt.Fprintf(&w.buf, "xref\n0 %d\n", len(w.offsets))
	w.buf.WriteString("0000000000 65535 f \n")
	for _, offset := range w.offsets[1:] {
		fmt.Fprintf(&w.buf, "%010d 00000 n \n", offset)
	}
	fmt.Fprintf(&w.buf, "trailer\n<< /Size %d /Root %d 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(w.offsets), rootObject, xref)
	return w.buf.Bytes()
}

func buildPageContent(pageWidth, pageHeight, boxWidth, boxHeight float64, imageWidth, imageHeight, imageObject int) []byte {
	scale := math.Min(boxWidth/float64(imageWidth), boxHeight/float64(imageHeight))
	drawWidth := float64(imageWidth) * scale
	drawHeight := float64(imageHeight) * scale
	x := (pageWidth - drawWidth) / 2
	y := (pageHeight - drawHeight) / 2
	return []byte(fmt.Sprintf("q\n%.4f 0 0 %.4f %.4f %.4f cm\n/Im%d Do\nQ", drawWidth, drawHeight, x, y, imageObject))
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
		for _, sub := range []string{"发票", screenshotFolder} {
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
			for _, att := range items {
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
				if err := os.WriteFile(attachmentTarget(folder, kind, fn), data, 0600); err != nil {
					return "", err
				}
			}
		}
		if err := createScreenshotPDF(filepath.Join(folder, screenshotFolder), filepath.Join(folder, "截图.pdf"), screenshotPDFOptions{WidthMM: screenshotWidthMM, HeightMM: screenshotHeightMM}); err != nil {
			return "", fmt.Errorf("生成 %s 截图 PDF 失败：%w", name, err)
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
	archivableImageCount := 0
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
			archivableImageCount++
			dir := filepath.Join(root, person, outputAttachmentKind(kind))
			if err := os.MkdirAll(dir, 0755); err != nil {
				return err
			}
			duplicate, err := hasFileBytes(dir, pic.File)
			if err != nil {
				return err
			}
			if duplicate {
				fmt.Printf("跳过重复图片：%s -> %s/%s\n", cell, person, outputAttachmentKind(kind))
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
			fmt.Printf("Excel 图片：%s -> %s/%s/%s\n", cell, person, outputAttachmentKind(kind), name)
		}
	}
	f.Close()
	if archivableImageCount == 0 {
		return errors.New("Excel 中未找到可归档的浮动图片")
	}
	if imageCount == 0 {
		fmt.Println("Excel 浮动图片均已存在，未新增保存")
	}
	for _, person := range rowPerson {
		invoiceCount, err := countRegularFiles(filepath.Join(root, person, "发票"))
		if err != nil {
			return err
		}
		screenshotCount, err := countRegularFiles(filepath.Join(root, person, screenshotFolder))
		if err != nil {
			return err
		}
		fmt.Printf("图片统计（文件夹实际数量）：%s 发票=%d 截图=%d\n", person, invoiceCount, screenshotCount)
	}
	for _, person := range rowPerson {
		folder := filepath.Join(root, person)
		if err := createScreenshotPDF(filepath.Join(folder, screenshotFolder), filepath.Join(folder, "截图.pdf"), screenshotPDFOptions{WidthMM: screenshotWidthMM, HeightMM: screenshotHeightMM}); err != nil {
			return err
		}
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
	appID := flag.String("app-id", resolveCredential(os.Getenv("FEISHU_APP_ID"), embeddedAppIDBlob), "飞书 App ID")
	appSecret := flag.String("app-secret", resolveCredential(os.Getenv("FEISHU_APP_SECRET"), embeddedAppSecretBlob), "飞书 App Secret")
	output := flag.String("output", defaultOutput, "探测结果 JSON 文件")
	downloadOne := flag.Bool("download-one", false, "下载预览中的第一份普通附件进行验证")
	downloadDir := flag.String("download-dir", filepath.Join("outputs", "attachment_download_check"), "单附件验证下载目录")
	archive := flag.Bool("archive", false, "批量下载附件并生成员工归档 ZIP")
	archiveDir := flag.String("archive-dir", "outputs", "批量归档输出目录")
	excelPath := flag.String("excel", "", "可选：本地 Excel 路径，用于补充浮动图片")
	widthMM := flag.Float64("screenshot-width-mm", screenshotWidthMM, "截图 PDF 图片宽度（毫米）")
	heightMM := flag.Float64("screenshot-height-mm", screenshotHeightMM, "截图 PDF 图片高度（毫米）")
	flag.Parse()
	screenshotWidthMM, screenshotHeightMM = *widthMM, *heightMM
	if *urlFlag == "" || *appID == "" || *appSecret == "" {
		fmt.Fprintln(os.Stderr, "请提供表格链接，并通过内置凭据、FEISHU_APP_ID / FEISHU_APP_SECRET 环境变量或 --app-id / --app-secret 参数提供应用凭据。")
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
