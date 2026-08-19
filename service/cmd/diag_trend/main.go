// Command diag_trend：诊断看板趋势数据。
package main

import (
	"database/sql"
	"fmt"
	"os"

	_ "github.com/go-sql-driver/mysql"
)

func main() {
	dsn := "root:root@tcp(127.0.0.1:3306)/im?charset=utf8mb4"
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		fmt.Println("open error:", err)
		os.Exit(1)
	}
	defer db.Close()
	if err := db.Ping(); err != nil {
		fmt.Println("ping error:", err)
		os.Exit(1)
	}

	fmt.Println("===== users 表 created_at 状态 =====")
	var total, nullCnt int
	db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&total)
	db.QueryRow(`SELECT COUNT(*) FROM users WHERE created_at IS NULL`).Scan(&nullCnt)
	fmt.Printf("users 总数=%d  created_at 为 NULL 的=%d\n", total, nullCnt)

	fmt.Println("\n===== users 前 15 条 created_at =====")
	rows, err := db.Query(`SELECT id, uid, account, created_at FROM users ORDER BY id DESC LIMIT 15`)
	if err != nil {
		fmt.Println("err:", err)
		os.Exit(1)
	}
	for rows.Next() {
		var id, uid int64
		var account string
		var ca sql.NullTime
		if err := rows.Scan(&id, &uid, &account, &ca); err != nil {
			fmt.Println("scan:", err)
			break
		}
		if ca.Valid {
			fmt.Printf("  id=%d uid=%d acc=%s created_at=%v\n", id, uid, account, ca.Time)
		} else {
			fmt.Printf("  id=%d uid=%d acc=%s created_at=NULL\n", id, uid, account)
		}
	}
	rows.Close()

	fmt.Println("\n===== messages_0 近7天 created_at 分布（对照）=====")
	rows, err = db.Query(`SELECT DATE(created_at) d, COUNT(1) FROM messages_0 WHERE created_at >= DATE_SUB(NOW(), INTERVAL 6 DAY) GROUP BY d ORDER BY d`)
	if err != nil {
		fmt.Println("err:", err)
		os.Exit(1)
	}
	for rows.Next() {
		var date string
		var n int
		rows.Scan(&date, &n)
		fmt.Printf("  %s = %d\n", date, n)
	}
	rows.Close()
}
