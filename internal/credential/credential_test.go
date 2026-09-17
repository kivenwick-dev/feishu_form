package credential

import (
	"strings"
	"testing"
)

func TestEncodeDecodeRoundTrip(t *testing.T) {
	values := []string{
		"cli_demo_app_id",
		"demo-app-secret-value",
		"含中文/符号!@#",
	}
	for _, want := range values {
		blob := Encode(want)
		if blob == "" {
			t.Fatalf("Encode(%q) 返回空串", want)
		}
		if strings.ContainsAny(blob, "+/=") {
			t.Fatalf("Encode(%q) 含 URL 不安全字符：%q", want, blob)
		}
		got, err := Decode(blob)
		if err != nil {
			t.Fatalf("Decode(%q) 出错：%v", blob, err)
		}
		if got != want {
			t.Fatalf("往返不一致：got %q want %q", got, want)
		}
	}
}

func TestEncodeDecodeEmpty(t *testing.T) {
	if got := Encode(""); got != "" {
		t.Fatalf("Encode(\"\") = %q，want \"\"", got)
	}
	got, err := Decode("")
	if err != nil || got != "" {
		t.Fatalf("Decode(\"\") = %q, %v，want \"\", nil", got, err)
	}
}

func TestDecodeInvalid(t *testing.T) {
	if _, err := Decode("!!!not-base64!!!"); err == nil {
		t.Fatal("非法 base64 应返回错误")
	}
}
