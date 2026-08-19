-- 阶段四：好友 / 群组 / 通知 表结构
-- 字段与 docs/IM系统架构设计.md 6.2 / 6.3 / 6.4 / 6.8 对齐。

-- 好友关系表：主键 (uid, friend_uid)，双向各存一条
CREATE TABLE IF NOT EXISTS friends (
    uid        BIGINT       NOT NULL COMMENT '用户 uid',
    friend_uid BIGINT       NOT NULL COMMENT '好友 uid',
    remark     VARCHAR(64)  DEFAULT NULL COMMENT '备注',
    tags       JSON         DEFAULT NULL COMMENT '标签',
    status     TINYINT      NOT NULL DEFAULT 1 COMMENT '关系状态 1正常',
    created_at DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '建立时间',
    PRIMARY KEY (uid, friend_uid)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='好友关系表';

-- 好友申请表
CREATE TABLE IF NOT EXISTS friend_requests (
    id         BIGINT       NOT NULL COMMENT '申请 ID（雪花）',
    from_uid   BIGINT       NOT NULL COMMENT '申请人 uid',
    to_uid     BIGINT       NOT NULL COMMENT '接收人 uid',
    message    VARCHAR(255) DEFAULT NULL COMMENT '验证消息',
    status     TINYINT      NOT NULL DEFAULT 0 COMMENT '0 待处理 / 1 已接受 / 2 已拒绝',
    created_at DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (id),
    KEY idx_to_status (to_uid, status),
    KEY idx_from (from_uid)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='好友申请表';

-- 群表：g_uid 为 10 位随机数字业务群号（groups 为保留字，需反引号）
CREATE TABLE IF NOT EXISTS `groups` (
    id            BIGINT       NOT NULL COMMENT '内部主键（雪花）',
    g_uid         BIGINT       NOT NULL COMMENT '业务群 ID，10 位随机数字（对外展示群号）',
    name          VARCHAR(64)  NOT NULL COMMENT '群名',
    owner_uid     BIGINT       NOT NULL COMMENT '群主 uid',
    announcement  TEXT         DEFAULT NULL COMMENT '群公告',
    member_count  INT          NOT NULL DEFAULT 0 COMMENT '成员数',
    avatar        VARCHAR(512) DEFAULT NULL COMMENT '群头像',
    created_at    DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间',
    PRIMARY KEY (id),
    UNIQUE KEY uk_g_uid (g_uid)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='群表';

-- 群成员表
CREATE TABLE IF NOT EXISTS group_members (
    g_uid     BIGINT   NOT NULL COMMENT '群 g_uid',
    uid       BIGINT   NOT NULL COMMENT '成员 uid',
    role      TINYINT  NOT NULL DEFAULT 2 COMMENT '0 群主 / 1 管理员 / 2 成员',
    join_time DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (g_uid, uid)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='群成员表';

-- 通知表（read 为保留字，需反引号）
CREATE TABLE IF NOT EXISTS notifications (
    id         BIGINT       NOT NULL COMMENT '通知 ID（雪花）',
    uid        BIGINT       NOT NULL COMMENT '接收者 uid',
    type       VARCHAR(16)  NOT NULL COMMENT 'reply/mention/system/friend/invite',
    title      VARCHAR(255) DEFAULT NULL COMMENT '标题',
    summary    VARCHAR(255) DEFAULT NULL COMMENT '摘要',
    action     JSON         DEFAULT NULL COMMENT '操作（如好友请求 accept）',
    `read`     TINYINT      NOT NULL DEFAULT 0 COMMENT '已读状态',
    created_at DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '时间',
    PRIMARY KEY (id),
    KEY idx_uid_read (uid, `read`),
    KEY idx_uid_time (uid, created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='通知表';
