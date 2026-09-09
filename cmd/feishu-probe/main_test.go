package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

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
