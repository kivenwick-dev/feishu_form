// Package credential 对内置的飞书应用凭据做可逆混淆。
// 注意：这不是安全加密，只能避免二进制中出现明文，仅用于内部可信分发。
package credential

import (
	"encoding/base64"
	"fmt"
)

const xorKey = "reimb-cred-2026!"

// Encode 把明文混淆成 URL 安全字符串；空串返回空串。
func Encode(plain string) string {
	if plain == "" {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(xor([]byte(plain)))
}

// Decode 还原 Encode 的结果；空串返回空串，非法输入返回错误。
func Decode(blob string) (string, error) {
	if blob == "" {
		return "", nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(blob)
	if err != nil {
		return "", fmt.Errorf("凭据混淆串无法解码：%w", err)
	}
	return string(xor(raw)), nil
}

func xor(data []byte) []byte {
	out := make([]byte, len(data))
	for i, b := range data {
		out[i] = b ^ xorKey[i%len(xorKey)]
	}
	return out
}
