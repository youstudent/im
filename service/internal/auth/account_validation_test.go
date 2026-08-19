package auth

import (
	"strings"
	"testing"
)

// TestRegisterAccountFormat 审计 M5：注册账号长度 4~64 + 字符集白名单。
func TestRegisterAccountFormat(t *testing.T) {
	svc := newTestService()
	cases := []struct {
		account string
		ok      bool
	}{
		{"abc", false},                      // 过短（<4）
		{strings.Repeat("a", 65), false},    // 过长（>64，DB VARCHAR(64) 对齐）
		{"has space1", false},               // 含空格
		{"中文账号12", false},               // 非白名单字符
		{"evil'or'1", false},                // 特殊字符
		{"user-01_x.y@example.com", true},   // 字母数字与 _ . - @ 全放行
		{"13000000001", true},               // 手机号形态
	}
	for i, c := range cases {
		_, err := svc.Register(&RegisterReq{Nickname: "格式测试", Account: c.account, Password: "pass1234"}, "")
		if c.ok && err != nil {
			t.Fatalf("用例 %d（%s）应注册成功: %v", i, c.account, err)
		}
		if !c.ok && err == nil {
			t.Fatalf("用例 %d（%s）应被拒绝", i, c.account)
		}
	}
}
