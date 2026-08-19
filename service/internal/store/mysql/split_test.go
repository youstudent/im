package mysql

import (
	"reflect"
	"testing"
)

func TestSplitStatements(t *testing.T) {
	content := `
-- 注释行
CREATE TABLE IF NOT EXISTS a (id INT);

-- 空语句处理
CREATE TABLE IF NOT EXISTS b (id INT);
`
	got := splitStatements(content)
	want := []string{
		"CREATE TABLE IF NOT EXISTS a (id INT)",
		"CREATE TABLE IF NOT EXISTS b (id INT)",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestSplitStatementsEmpty(t *testing.T) {
	if got := splitStatements("-- only comment\n\n"); len(got) != 0 {
		t.Fatalf("expected no statements, got %v", got)
	}
}
