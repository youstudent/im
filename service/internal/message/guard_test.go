package message

import (
	"strings"
	"testing"
	"time"
)

// TestFilterSensitive 验证敏感词过滤。
func TestFilterSensitive(t *testing.T) {
	cases := []struct {
		in   string
		want string
		hit  bool
	}{
		{"你好世界", "你好世界", false},
		{"你个傻逼", "你个***", true},
		{"fuck you", "*** you", true},
		{"正常消息", "正常消息", false},
	}
	for _, c := range cases {
		got, hit := FilterSensitive(c.in)
		if got != c.want || hit != c.hit {
			t.Fatalf("FilterSensitive(%q) = (%q, %v)，期望 (%q, %v)", c.in, got, hit, c.want, c.hit)
		}
	}
}

// TestRateLimiter 验证消息频率风控：窗口内超限被拒绝，窗口重置后恢复。
func TestRateLimiter(t *testing.T) {
	r := newRateLimiter(100*time.Millisecond, 3)
	// 窗口内发 3 条（允许）
	for i := 0; i < 3; i++ {
		if ok, _ := r.Allow(1); !ok {
			t.Fatalf("第 %d 条应被允许", i+1)
		}
	}
	// 第 4 条应被拒绝
	if ok, _ := r.Allow(1); ok {
		t.Fatal("第 4 条应被拒绝（超过窗口上限）")
	}
	// 窗口重置后恢复
	time.Sleep(120 * time.Millisecond)
	if ok, _ := r.Allow(1); !ok {
		t.Fatal("窗口重置后应恢复允许")
	}
	// 不同用户互不影响
	if ok, _ := r.Allow(2); !ok {
		t.Fatal("其他用户不应受影响")
	}
}

// TestSensitiveFilteredInSend 验证 Send 中对文本消息应用敏感词过滤。
func TestSensitiveFilteredInSend(t *testing.T) {
	svc, store, _ := newService()
	msg, _, err := svc.Send(10, &SendReq{ConvID: 1, TargetID: 2, ConvType: 1, Type: 1, MsgID: 500, Content: "你这个傻逼"})
	if err != nil {
		t.Fatalf("Send failed: %v", err)
	}
	if strings.Contains(msg.Content, "傻逼") {
		t.Fatalf("敏感词应被过滤，实际内容: %q", msg.Content)
	}
	_ = store
}
