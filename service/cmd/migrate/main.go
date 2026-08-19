// Command migrate 用于独立执行数据库迁移。
package main

import (
	"flag"
	"os"

	"im/service/internal/config"
	"im/service/internal/pkg/log"
	"im/service/internal/store/mysql"
)

var (
	configPath = flag.String("config", "../../configs/config.yaml", "path to config file")
	migrateDir = flag.String("dir", "../../migrations", "migrations directory")
)

func main() {
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Init("info", "stdout")
		log.L().Error("load config failed", "error", err)
		os.Exit(1)
	}
	log.Init(cfg.Log.Level, cfg.Log.Output)

	db, err := mysql.New(cfg.MySQL)
	if err != nil {
		log.L().Error("init mysql failed", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	dir := *migrateDir
	if dir == "" {
		dir = cfg.MySQL.MigrateDir
	}
	if err := db.Migrate(dir); err != nil {
		log.L().Error("migration failed", "error", err)
		os.Exit(1)
	}
	log.L().Info("migration completed")
}
