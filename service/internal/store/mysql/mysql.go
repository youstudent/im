// Package mysql 负责 MySQL 连接池初始化、迁移与公共 DAO 工具。
package mysql

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql"

	"im/service/internal/config"
	"im/service/internal/pkg/log"
)

// DB 封装 MySQL 连接池。
type DB struct {
	*sql.DB
}

// New 根据配置建立连接池并 Ping 验证连通性。
func New(cfg config.MySQL) (*DB, error) {
	db, err := sql.Open("mysql", cfg.DSN)
	if err != nil {
		return nil, fmt.Errorf("mysql open: %w", err)
	}
	db.SetMaxOpenConns(cfg.MaxOpenConns)
	db.SetMaxIdleConns(cfg.MaxIdleConns)
	db.SetConnMaxLifetime(time.Duration(cfg.ConnMaxLifetime) * time.Second)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("mysql ping: %w", err)
	}
	return &DB{DB: db}, nil
}

// Migrate 按文件名顺序执行 migrations 目录下的 .sql 文件。
// 每个文件用版本化文件名（如 0001_init.sql），通过 __migrations 表记录已执行版本。
func (d *DB) Migrate(dir string) error {
	if err := d.ensureMigrationTable(); err != nil {
		return err
	}

	files, err := filepath.Glob(filepath.Join(dir, "*.sql"))
	if err != nil {
		return fmt.Errorf("migrate glob: %w", err)
	}
	if len(files) == 0 {
		log.L().Warn("no migration files found", "dir", dir)
		return nil
	}
	sort.Strings(files)

	applied, err := d.appliedVersions()
	if err != nil {
		return err
	}

	for _, f := range files {
		name := filepath.Base(f)
		if applied[name] {
			continue
		}
		content, err := os.ReadFile(f)
		if err != nil {
			return fmt.Errorf("migrate read %s: %w", name, err)
		}
		statements := splitStatements(string(content))
		tx, err := d.Begin()
		if err != nil {
			return fmt.Errorf("migrate begin %s: %w", name, err)
		}
		for _, stmt := range statements {
			if _, err := tx.Exec(stmt); err != nil {
				_ = tx.Rollback()
				return fmt.Errorf("migrate apply %s: %w", name, err)
			}
		}
		if _, err := tx.Exec("INSERT INTO __migrations (name) VALUES (?)", name); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("migrate record %s: %w", name, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("migrate commit %s: %w", name, err)
		}
		log.L().Info("migration applied", "file", name)
	}
	return nil
}

func (d *DB) ensureMigrationTable() error {
	_, err := d.Exec(`CREATE TABLE IF NOT EXISTS __migrations (
		id INT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
		name VARCHAR(255) NOT NULL UNIQUE,
		applied_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`)
	return err
}

func (d *DB) appliedVersions() (map[string]bool, error) {
	rows, err := d.Query("SELECT name FROM __migrations")
	if err != nil {
		return nil, fmt.Errorf("query applied migrations: %w", err)
	}
	defer rows.Close()
	res := make(map[string]bool)
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		res[name] = true
	}
	return res, rows.Err()
}

// splitStatements 按分号拆分 SQL 语句，忽略注释与空语句。
// 不支持存储过程/触发器内的分号（迁移文件应避免使用）。
func splitStatements(content string) []string {
	var stmts []string
	// 简单按行处理：去掉注释与空行，再按分号切分
	lines := strings.Split(content, "\n")
	var buf strings.Builder
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "--") {
			continue
		}
		buf.WriteString(line)
		buf.WriteString("\n")
	}
	full := buf.String()
	for _, part := range strings.Split(full, ";") {
		if s := strings.TrimSpace(part); s != "" {
			stmts = append(stmts, s)
		}
	}
	return stmts
}

// Shard 工具：消息分表路由算法。
// 不能直接用 conv_id % shardCount：conv_id 是雪花 ID，其低位（nodeID<<12 | sequence）
// 对 shardCount 取模几乎固定（本项目 nodeID=1 且 sequence 在跨毫秒会话创建时恒为 0），
// 会导致所有会话路由到同一分表。这里改用 FNV-1a 散列，保证路由均匀且稳定。
func Shard(convID int64, shardCount int) int {
	if shardCount <= 1 {
		return 0
	}
	h := uint64(14695981039346656037) // FNV-1a 64-bit offset basis
	for _, b := range []byte(fmt.Sprintf("%d", convID)) {
		h ^= uint64(b)
		h *= 1099511628211 // FNV prime
	}
	return int(h % uint64(shardCount))
}

// TableName 返回分表名，如 messages_0。
func TableName(base string, shard int) string {
	return strings.ToLower(fmt.Sprintf("%s_%d", base, shard))
}
