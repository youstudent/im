-- 初始表结构：用户表（阶段二认证/登录的基础）
-- 阶段三起陆续增加会话/消息/好友/群组/通知等表

CREATE TABLE IF NOT EXISTS users (
    id            BIGINT       NOT NULL COMMENT '内部主键（雪花 ID）',
    uid           BIGINT       NOT NULL COMMENT '业务 UID，10 位随机数字，对外展示（workchatId）',
    account       VARCHAR(64)  NOT NULL COMMENT '登录账号（手机/邮箱）',
    password_hash VARCHAR(255) NOT NULL COMMENT '密码哈希（bcrypt）',
    email         VARCHAR(128) DEFAULT NULL COMMENT '绑定邮箱（用于找回密码）',
    nickname      VARCHAR(64)  NOT NULL COMMENT '昵称',
    avatar        VARCHAR(512) DEFAULT NULL COMMENT '头像 URL',
    signature     VARCHAR(255) DEFAULT NULL COMMENT '个性签名',
    status        TINYINT      NOT NULL DEFAULT 0 COMMENT '在线状态：0离线 1在线 2忙碌 3隐身',
    last_seen_at  DATETIME     DEFAULT NULL COMMENT '最后上线时间',
    created_at    DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '注册时间',
    PRIMARY KEY (id),
    UNIQUE KEY uk_uid (uid),
    UNIQUE KEY uk_account (account)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='用户表';
