package message

import (
	"sync/atomic"
	"testing"
)

// ===================== 6.5 压测：消息吞吐 =====================
// 验证消息发送的吞吐量与长连接场景下的稳定性。
// 运行：go test ./internal/message/ -bench=Benchmark -benchmem

// benchService 构造压测用 Service（禁用限流，专注吞吐）。
func benchService(b *testing.B) *Service {
	s := newMockStore()
	var idSeq int64
	svc := New(s, func() int64 { return atomic.AddInt64(&idSeq, 1) },
		func(_ int64, m *MessageDTO) {}, nil)
	svc.limiter = nil
	return svc
}

// BenchmarkMsgSendThroughput 单协程消息发送吞吐。
// 预期：每秒发送量 = b.N / 耗时。
func BenchmarkMsgSendThroughput(b *testing.B) {
	svc := benchService(b)
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		var i int64
		for pb.Next() {
			n := atomic.AddInt64(&i, 1)
			// 每个并行协程用不同 target，避免共享会话写锁竞争被误算
			_, _, _ = svc.Send(int64(n%100+1), &SendReq{ConvID: 1, TargetID: int64(n%500 + 100), ConvType: 1, Type: 1, MsgID: n, Content: "bench"})
		}
	})
}

// BenchmarkMsgSeqContention 同会话高并发消息（seq 分配竞争）。
// 验证会话内 seq 分配在高并发下的吞吐。
func BenchmarkMsgSeqContention(b *testing.B) {
	svc := benchService(b)
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		var i int64
		for pb.Next() {
			n := atomic.AddInt64(&i, 1)
			_, _, _ = svc.Send(10, &SendReq{ConvID: 1, TargetID: 2, ConvType: 1, Type: 1, MsgID: n, Content: "seq"})
		}
	})
}
