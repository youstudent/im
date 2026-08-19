package message

import (
	"strings"
	"sync"
	"time"
)

// ===================== 敏感词过滤 =====================

// 敏感词表：命中后替换为 ***。
// 实际项目可扩展为从配置/DB/词库加载。
var sensitiveWords = []string{
	"傻逼", "混蛋", "去死", "垃圾", "妈的", "操你", "fuck", "shit", "贱人", "白痴",
}

// FilterSensitive 过滤消息中的敏感词，返回脱敏后的内容与是否命中。
func FilterSensitive(content string) (string, bool) {
	hit := false
	for _, w := range sensitiveWords {
		if strings.Contains(content, w) {
			content = strings.ReplaceAll(content, w, "***")
			hit = true
		}
	}
	return content, hit
}

// ===================== 消息频率风控 =====================

// rateLimiter 简单的每用户滑动窗口限流：单位时间窗口内最多 max 条。
type rateLimiter struct {
	mu     sync.Mutex
	window time.Duration
	max    int
	// uid -> (窗口起点, 计数)
	buckets map[int64]*rateBucket
}

type rateBucket struct {
	start time.Time
	count int
}

func newRateLimiter(window time.Duration, max int) *rateLimiter {
	return &rateLimiter{window: window, max: max, buckets: make(map[int64]*rateBucket)}
}

// Allow 判断 uid 是否允许发送；不允许时返回剩余等待时间。
func (r *rateLimiter) Allow(uid int64) (ok bool, wait time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now()
	b, exists := r.buckets[uid]
	if !exists || now.Sub(b.start) >= r.window {
		// 新窗口
		r.buckets[uid] = &rateBucket{start: now, count: 1}
		return true, 0
	}
	if b.count >= r.max {
		// 超限：窗口剩余时间
		wait = r.window - now.Sub(b.start)
		return false, wait
	}
	b.count++
	return true, 0
}
