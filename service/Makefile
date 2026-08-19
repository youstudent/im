# IM 服务端 Makefile
# 目标：run / build / migrate / deps / lint / test

GO ?= go
BINARY := im-server
CONFIG ?= configs/config.yaml
MIGRATE_DIR := migrations

.PHONY: help run build migrate deps tidy lint test docker-up docker-down clean

help: ## 显示可用命令
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'

run: ## 启动服务（从 service 目录执行）
	$(GO) run ./cmd/server -config $(CONFIG)

build: ## 编译二进制到 bin/
	mkdir -p bin
	$(GO) build -o bin/$(BINARY) ./cmd/server

migrate: ## 仅执行数据库迁移
	$(GO) run ./cmd/migrate -config $(CONFIG) -dir $(MIGRATE_DIR)

deps: ## 拉取依赖
	$(GO) mod download

tidy: ## 整理 go.mod
	$(GO) mod tidy

lint: ## 静态检查
	$(GO) vet ./...

test: ## 运行测试
	$(GO) test ./...

docker-up: ## 启动本地 MySQL + Redis
	docker compose up -d

docker-down: ## 停止本地依赖
	docker compose down

clean: ## 清理构建产物
	rm -rf bin
