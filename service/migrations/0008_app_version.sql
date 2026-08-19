-- 阶段六：客户端版本发布（检查更新）
-- app_versions 版本发布记录表
CREATE TABLE IF NOT EXISTS app_versions (
    id            BIGINT       NOT NULL COMMENT '版本记录 ID（雪花）',
    version       VARCHAR(32)  NOT NULL COMMENT '版本号（如 1.1.0）',
    download_url  VARCHAR(512) NOT NULL DEFAULT '' COMMENT '安装包下载地址',
    release_notes TEXT         COMMENT '更新说明',
    publisher     VARCHAR(64)  DEFAULT '' COMMENT '发布管理员用户名',
    created_at    DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '发布时间',
    PRIMARY KEY (id),
    UNIQUE KEY uk_version (version)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='客户端版本发布表';
