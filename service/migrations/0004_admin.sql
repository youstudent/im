-- 阶段五：管理后台 相关表
-- admin 管理员表
CREATE TABLE IF NOT EXISTS admin_users (
    id            BIGINT       NOT NULL COMMENT '管理员 ID（雪花）',
    username      VARCHAR(64)  NOT NULL COMMENT '登录用户名',
    password_hash VARCHAR(255) NOT NULL COMMENT '密码哈希（bcrypt）',
    nickname      VARCHAR(64)  DEFAULT NULL COMMENT '昵称',
    role          TINYINT      NOT NULL DEFAULT 1 COMMENT '1 超级管理员 / 2 普通管理员',
    status        TINYINT      NOT NULL DEFAULT 1 COMMENT '1 启用 / 0 禁用',
    created_at    DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (id),
    UNIQUE KEY uk_username (username)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='管理员表';
