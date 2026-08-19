package snowflake

import (
	"testing"
)

func TestNextIDUnique(t *testing.T) {
	sf, err := New(1)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	const n = 100000
	seen := make(map[int64]struct{}, n)
	for i := 0; i < n; i++ {
		id := sf.NextID()
		if id <= 0 {
			t.Fatalf("id should be positive, got %d", id)
		}
		if _, ok := seen[id]; ok {
			t.Fatalf("duplicate id: %d", id)
		}
		seen[id] = struct{}{}
	}
}

func TestNewNodeIDOutOfRange(t *testing.T) {
	if _, err := New(1024); err == nil {
		t.Fatal("expected error for node id 1024")
	}
	if _, err := New(-1); err == nil {
		t.Fatal("expected error for negative node id")
	}
}
