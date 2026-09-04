package lark

import "testing"

func TestDecodeContent(t *testing.T) {
	tests := []struct{ kind, raw, want string }{
		{"text", `{"text":"hello"}`, "hello"},
		{"image", `{"image_key":"img_x"}`, "[图片]"},
		{"post", `{"zh_cn":{"title":"更新","content":[[{"tag":"text","text":"已经发布"}]]}}`, "更新 已经发布"},
	}
	for _, tt := range tests {
		if got := decodeContent(tt.kind, tt.raw); got != tt.want {
			t.Fatalf("decodeContent(%s)=%q, want %q", tt.kind, got, tt.want)
		}
	}
}
