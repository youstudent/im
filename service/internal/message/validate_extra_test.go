package message

import (
	"strings"
	"testing"
)

// TestSendContentTooLong 审计 H3：消息内容超长直接拒绝（防 HTTP 通道超大消息体滥用）。
func TestSendContentTooLong(t *testing.T) {
	svc, _, _ := newTestSvc()
	long := strings.Repeat("字", maxContentRunes+1)
	if _, _, err := svc.Send(1001, &SendReq{TargetID: 1002, ConvType: 1, Type: 1, Content: long}); err == nil {
		t.Fatal("超长内容应被拒绝")
	}
	// 恰好等于上限应放行
	ok := strings.Repeat("字", maxContentRunes)
	if _, _, err := svc.Send(1001, &SendReq{TargetID: 1002, ConvType: 1, Type: 1, Content: ok}); err != nil {
		t.Fatalf("等于上限的内容应允许: %v", err)
	}
}

// TestSendExtraValidation 审计 H3/H4：extra 必须为合法 JSON、不超 2KB，
// 且携带 url 时必须 https + 域名在可信 OSS 白名单内。
func TestSendExtraValidation(t *testing.T) {
	svc, _, _ := newTestSvc()
	svc.SetAllowedMediaHosts([]string{"img.example.com"})

	cases := []struct {
		extra string
		ok    bool
	}{
		{`{"url":"https://img.example.com/image/1/1_1.jpg","key":"image/1/1_1.jpg","name":"a.jpg"}`, true},
		{`{"url":"https://evil.com/malware.exe","name":"文件"}`, false}, // 外部域名
		{`{"url":"http://img.example.com/1.jpg"}`, false},              // 非 https
		{`not-json`, false},                                            // 非法 JSON
		{`{"name":"no-url"}`, true},                                    // 无 url 放行
		{``, true},                                                     // 空 extra 放行
	}
	for i, c := range cases {
		content := c.extra
		if content == "" {
			content = "f"
		}
		_, _, err := svc.Send(1001, &SendReq{TargetID: 1002, ConvType: 1, Type: 3, Content: content, Extra: c.extra})
		if c.ok && err != nil {
			t.Fatalf("用例 %d 应发送成功: %v", i, err)
		}
		if !c.ok && err == nil {
			t.Fatalf("用例 %d 应被拒绝: %s", i, c.extra)
		}
	}

	// extra 超限拒绝
	big := `{"name":"` + strings.Repeat("x", maxExtraBytes) + `"}`
	if _, _, err := svc.Send(1001, &SendReq{TargetID: 1002, ConvType: 1, Type: 3, Content: "f", Extra: big}); err == nil {
		t.Fatal("超大 extra 应被拒绝")
	}
}

// TestSendExtraNoWhitelist 白名单未注入（OSS 未配置降级）时不校验 url，不阻断主流程。
func TestSendExtraNoWhitelist(t *testing.T) {
	svc, _, _ := newTestSvc()
	if _, _, err := svc.Send(1001, &SendReq{TargetID: 1002, ConvType: 1, Type: 2, Content: "img", Extra: `{"url":"https://any.example.com/a.png"}`}); err != nil {
		t.Fatalf("未配置白名单时应放行: %v", err)
	}
}
