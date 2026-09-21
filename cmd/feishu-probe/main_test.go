package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"regexp"
	"reimbursement-archiver/internal/credential"
	"strconv"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"
)

func testPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{uint8(x % 256), uint8(y % 256), 128, 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func testJPEG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{200, uint8(x % 256), uint8(y % 256), 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestParseLink(t *testing.T) {
	got, err := parseLink("https://ucn81zkkano6.feishu.cn/wiki/Rbv1wt09tiHbsOkWq6qc5lhinAe?fromScene=spaceOverview&sheet=0EDeTa")
	if err != nil {
		t.Fatal(err)
	}
	if got.WikiToken != "Rbv1wt09tiHbsOkWq6qc5lhinAe" || got.SheetID != "0EDeTa" {
		t.Fatalf("unexpected parse: %#v", got)
	}
}

func TestFindAttachments(t *testing.T) {
	raw := []byte(`[{"fileToken":"abc","mimeType":"application/pdf","text":"invoice.pdf","type":"attachment"},{"fileToken":"img","type":"embed-image","link":"https://example.invalid/image"}]`)
	items := findAttachments(raw)
	if len(items) != 2 || items[0].FileToken != "abc" || items[1].Link == "" {
		t.Fatalf("unexpected attachments: %#v", items)
	}
}

func TestAttachmentExtension(t *testing.T) {
	if got := attachmentExtension("截图", "image/png", nil); got != ".png" {
		t.Fatalf("png extension = %q", got)
	}
	if got := attachmentExtension("发票", "application/pdf", nil); got != ".pdf" {
		t.Fatalf("pdf extension = %q", got)
	}
	if got := attachmentExtension("已有.jpg", "image/png", nil); got != ".jpg" {
		t.Fatalf("existing extension = %q", got)
	}
}

func TestFirstSheetID(t *testing.T) {
	raw := json.RawMessage(`[{"sheet_id":"sheetA","title":"报销"}]`)
	if got := firstSheetID(raw); got != "sheetA" {
		t.Fatalf("sheet id = %q", got)
	}
}

func TestExcelPictureKindUsesAnchorColumn(t *testing.T) {
	headers := []string{"员工姓名", "发票", "支付", "会员开通截图"}
	// A neighboring/out-of-range anchor must not inherit the membership kind.
	if got := excelPictureKind(headers, 8); got != "其他附件" {
		t.Fatalf("trailing column kind = %q", got)
	}
	if got := excelPictureKind(headers, 3); got != "会员开通截图" {
		t.Fatalf("header column kind = %q", got)
	}
}

func TestExcelPictureKindDoesNotInheritUnknownHeader(t *testing.T) {
	headers := []string{"员工姓名", "发票", "备注", "会员开通截图"}
	if got := excelPictureKind(headers, 2); got != "其他附件" {
		t.Fatalf("unknown column kind = %q", got)
	}
}

func TestHasFileBytes(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "attachment.png"), []byte("same-image"), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := hasFileBytes(dir, []byte("same-image"))
	if err != nil || !got {
		t.Fatalf("duplicate image = %v, err = %v", got, err)
	}
	got, err = hasFileBytes(dir, []byte("different-image"))
	if err != nil || got {
		t.Fatalf("different image = %v, err = %v", got, err)
	}
}

func TestCountRegularFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.png"), []byte("a"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "nested"), 0700); err != nil {
		t.Fatal(err)
	}
	if got, err := countRegularFiles(dir); err != nil || got != 1 {
		t.Fatalf("file count = %d, err = %v", got, err)
	}
}

func TestOutputAttachmentKindUnifiesScreenshots(t *testing.T) {
	cases := map[string]string{
		"发票":          "发票",
		"支付_账号充值成功截图": "截图",
		"会员开通截图":      "截图",
		"其他附件":        "其他附件",
	}
	for kind, want := range cases {
		if got := outputAttachmentKind(kind); got != want {
			t.Errorf("outputAttachmentKind(%q) = %q, want %q", kind, got, want)
		}
	}
}

func TestAttachmentTarget(t *testing.T) {
	dir := t.TempDir()
	if got, want := attachmentTarget(dir, "支付_账号充值成功截图", "a.png"), filepath.Join(dir, "截图", "a.png"); got != want {
		t.Errorf("payment screenshot target = %q, want %q", got, want)
	}
	if got, want := attachmentTarget(dir, "会员开通截图", "a.png"), filepath.Join(dir, "截图", "a.png"); got != want {
		t.Errorf("membership screenshot target = %q, want %q", got, want)
	}
	if got, want := attachmentTarget(dir, "发票", "b.pdf"), filepath.Join(dir, "发票", "b.pdf"); got != want {
		t.Errorf("invoice target = %q, want %q", got, want)
	}
}

func TestAttachmentTargetAvoidsCollisions(t *testing.T) {
	dir := t.TempDir()
	shot := filepath.Join(dir, "截图")
	if err := os.MkdirAll(shot, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(shot, "a.png"), []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	if got, want := attachmentTarget(dir, "会员开通截图", "a.png"), filepath.Join(shot, "a_2.png"); got != want {
		t.Errorf("collision target = %q, want %q", got, want)
	}
	if err := os.WriteFile(filepath.Join(shot, "a_2.png"), []byte("y"), 0600); err != nil {
		t.Fatal(err)
	}
	if got, want := attachmentTarget(dir, "支付_账号充值成功截图", "a.png"), filepath.Join(shot, "a_3.png"); got != want {
		t.Errorf("second collision target = %q, want %q", got, want)
	}
}

func TestSupplementExcelImagesAllowsAllDuplicates(t *testing.T) {
	dir := t.TempDir()
	img := testPNG(t, 20, 20)

	excelPath := filepath.Join(dir, "source.xlsx")
	f := excelize.NewFile()
	if err := f.SetSheetRow("Sheet1", "A1", &[]any{"员工姓名", "支付_账号充值成功截图"}); err != nil {
		t.Fatal(err)
	}
	if err := f.SetCellValue("Sheet1", "A2", "张三"); err != nil {
		t.Fatal(err)
	}
	if err := f.AddPictureFromBytes("Sheet1", "B2", &excelize.Picture{Extension: ".png", File: img}); err != nil {
		t.Fatal(err)
	}
	if err := f.SaveAs(excelPath); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	archivePath := filepath.Join(dir, "archive.zip")
	root := strings.TrimSuffix(archivePath, filepath.Ext(archivePath))
	shotDir := filepath.Join(root, "张三", screenshotFolder)
	if err := os.MkdirAll(shotDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(shotDir, "existing.png"), img, 0600); err != nil {
		t.Fatal(err)
	}

	if err := supplementExcelImages(excelPath, archivePath); err != nil {
		t.Fatalf("duplicate-only Excel images should not fail: %v", err)
	}
	if got, err := countRegularFiles(shotDir); err != nil || got != 1 {
		t.Fatalf("screenshot folder file count = %d, err = %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(root, "张三", "截图.pdf")); err != nil {
		t.Fatalf("screenshot PDF should be rebuilt: %v", err)
	}
	if _, err := os.Stat(archivePath); err != nil {
		t.Fatalf("archive ZIP should be rebuilt: %v", err)
	}
}

func TestBuildImagePDFHasValidXref(t *testing.T) {
	images := []pdfImage{
		{Width: 4, Height: 3, ColorSpace: "/DeviceRGB", BitsPerComp: 8, Filter: "/DCTDecode", Data: []byte("fake-jpeg")},
		{Width: 2, Height: 2, ColorSpace: "/DeviceRGB", BitsPerComp: 8, Filter: "/FlateDecode", Data: []byte("fake-flate")},
	}
	pdf := buildImagePDF(images, 595.275590551, 841.88976378, 200, 300)
	if !bytes.HasPrefix(pdf, []byte("%PDF")) || !bytes.Contains(pdf, []byte("%%EOF")) {
		t.Fatal("missing PDF header or trailer")
	}
	pages := regexp.MustCompile(`/Type /Page[^s]`).FindAll(pdf, -1)
	if len(pages) != len(images) {
		t.Fatalf("page count = %d, want %d", len(pages), len(images))
	}

	xrefRe := regexp.MustCompile(`(?s)xref\n0 (\d+)\n(.*?)trailer`)
	m := xrefRe.FindSubmatch(pdf)
	if m == nil {
		t.Fatal("no xref table")
	}
	count, _ := strconv.Atoi(string(m[1]))
	wantObjects := 1 + len(images)*3 + 2
	if count != wantObjects {
		t.Fatalf("xref size = %d, want %d", count, wantObjects)
	}
	lines := strings.Split(strings.TrimRight(string(m[2]), "\n"), "\n")
	if len(lines) != count {
		t.Fatalf("xref entries = %d, want %d", len(lines), count)
	}
	for number := 1; number < count; number++ {
		offsetField := strings.Fields(lines[number])
		if len(offsetField) == 0 {
			t.Fatalf("object %d: empty xref line", number)
		}
		offset, err := strconv.Atoi(offsetField[0])
		if err != nil {
			t.Fatalf("object %d: bad offset %q", number, offsetField[0])
		}
		header := []byte(fmt.Sprintf("%d 0 obj", number))
		if offset <= 0 || offset >= len(pdf) || !bytes.HasPrefix(pdf[offset:], header) {
			t.Errorf("object %d: xref offset %d does not point at %q", number, offset, header)
		}
	}
}

func TestCreateScreenshotPDFWritesPagesAndSkipsNonImages(t *testing.T) {
	folder := t.TempDir()
	if err := os.WriteFile(filepath.Join(folder, "a.png"), testPNG(t, 400, 300), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(folder, "b.jpg"), testJPEG(t, 200, 500), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(folder, "notes.txt"), []byte("ignore"), 0600); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(folder, "截图.pdf")
	if err := createScreenshotPDF(folder, out, screenshotPDFOptions{WidthMM: 180, HeightMM: 260}); err != nil {
		t.Fatalf("createScreenshotPDF: %v", err)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	pages := regexp.MustCompile(`/Type /Page[^s]`).FindAll(data, -1)
	if len(pages) != 2 {
		t.Fatalf("page count = %d, want 2", len(pages))
	}
	if bytes.Contains(data, []byte("ignore")) {
		t.Error("non-image content leaked into PDF")
	}

	boxW, boxH := 180*72/25.4, 260*72/25.4
	cmRe := regexp.MustCompile(`([0-9.]+) 0 0 ([0-9.]+) ([0-9.]+) ([0-9.]+) cm`)
	draws := cmRe.FindAllSubmatch(data, -1)
	if len(draws) != 2 {
		t.Fatalf("draw operations = %d, want 2", len(draws))
	}
	for _, cm := range draws {
		w, _ := strconv.ParseFloat(string(cm[1]), 64)
		h, _ := strconv.ParseFloat(string(cm[2]), 64)
		x, _ := strconv.ParseFloat(string(cm[3]), 64)
		y, _ := strconv.ParseFloat(string(cm[4]), 64)
		if w > boxW+0.01 || h > boxH+0.01 {
			t.Errorf("image %.2fx%.2fpt exceeds box %.2fx%.2fpt", w, h, boxW, boxH)
		}
		if x < 0 || y < 0 {
			t.Errorf("image placed outside page at (%.2f, %.2f)", x, y)
		}
	}
}

func TestCreateScreenshotPDFRejectsOversizedDimensions(t *testing.T) {
	folder := t.TempDir()
	if err := os.WriteFile(filepath.Join(folder, "a.png"), testPNG(t, 10, 10), 0600); err != nil {
		t.Fatal(err)
	}
	err := createScreenshotPDF(folder, filepath.Join(folder, "out.pdf"), screenshotPDFOptions{WidthMM: 400, HeightMM: 400})
	if err == nil {
		t.Fatal("expected oversized dimensions to be rejected")
	}
}

func TestCreateScreenshotPDFSkipsWhenNoImages(t *testing.T) {
	folder := t.TempDir()
	out := filepath.Join(folder, "out.pdf")
	if err := createScreenshotPDF(filepath.Join(folder, "截图"), out, screenshotPDFOptions{WidthMM: 180, HeightMM: 260}); err != nil {
		t.Fatalf("missing folder should not error: %v", err)
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Error("no PDF should be written when there are no images")
	}
}

func TestResolveCredential(t *testing.T) {
	blob := credential.Encode("embedded-id")
	if got := resolveCredential("env-id", blob); got != "env-id" {
		t.Fatalf("环境变量应优先：got %q", got)
	}
	if got := resolveCredential("", blob); got != "embedded-id" {
		t.Fatalf("应回退内置值：got %q", got)
	}
	if got := resolveCredential("   ", blob); got != "embedded-id" {
		t.Fatalf("空白环境变量应回退内置值：got %q", got)
	}
	if got := resolveCredential("", ""); got != "" {
		t.Fatalf("都为空应返回空：got %q", got)
	}
	if got := resolveCredential("", "!!!bad!!!"); got != "" {
		t.Fatalf("非法混淆串应返回空：got %q", got)
	}
}
