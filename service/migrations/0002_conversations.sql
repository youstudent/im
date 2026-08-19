-- 阶段三：会话 / 消息（分表）/ 已读 表结构
-- 字段与 docs/IM系统架构设计.md 6.5 / 6.6 / 6.7 对齐。

-- 会话表：每个用户一个会话视图（单聊 target=对方uid，群聊 target=群g_uid）
CREATE TABLE IF NOT EXISTS conversations (
    id            BIGINT       NOT NULL COMMENT '会话 ID（雪花，内部主键）',
    type          TINYINT      NOT NULL COMMENT '1 单聊 / 2 群聊',
    owner_uid     BIGINT       NOT NULL COMMENT '归属用户 uid',
    target_id     BIGINT       NOT NULL COMMENT '对方 uid（单聊）或群 g_uid（群聊）',
    last_msg_id   BIGINT       DEFAULT NULL COMMENT '最后一条消息 id',
    last_msg_text VARCHAR(500) DEFAULT NULL COMMENT '最后消息摘要（列表展示）',
    last_msg_time DATETIME     DEFAULT NULL COMMENT '最后消息时间（会话排序）',
    last_synced_seq BIGINT     NOT NULL DEFAULT 0 COMMENT '已同步到的最大 seq（增量拉取游标）',
    muted         TINYINT      NOT NULL DEFAULT 0 COMMENT '免打扰',
    pinned        TINYINT      NOT NULL DEFAULT 0 COMMENT '置顶',
    created_at    DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (id),
    UNIQUE KEY uk_owner_target (owner_uid, target_id),
    KEY idx_owner_last (owner_uid, last_msg_time)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='会话表';

-- 消息表（分 4 张表，路由 conv_id % 4）
-- 用存储过程循环建表，避免重复；迁移执行器按分号切分语句，因此这里拆成 4 条独立 CREATE。
CREATE TABLE IF NOT EXISTS messages_0 (
    id         BIGINT       NOT NULL COMMENT '消息 ID（雪花，全局唯一）',
    conv_id    BIGINT       NOT NULL COMMENT '所属会话（分表键）',
    seq        BIGINT       NOT NULL COMMENT '会话内单调递增序号',
    sender_uid BIGINT       NOT NULL COMMENT '发送者 uid',
    type       TINYINT      NOT NULL DEFAULT 1 COMMENT '1 text / 2 image / 3 file / 4 voice / 5 video / 6 system',
    content    TEXT         COMMENT '文本或结构化 JSON',
    extra      JSON         DEFAULT NULL COMMENT '扩展（文件信息、链接 URL、撤回标记等）',
    status     TINYINT      NOT NULL DEFAULT 0 COMMENT '0 正常 / 1 已撤回',
    created_at DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '发送时间',
    PRIMARY KEY (id),
    UNIQUE KEY uk_conv_seq (conv_id, seq),
    KEY idx_conv_time (conv_id, created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='消息分表0';

CREATE TABLE IF NOT EXISTS messages_1 (
    id         BIGINT       NOT NULL COMMENT '消息 ID（雪花，全局唯一）',
    conv_id    BIGINT       NOT NULL COMMENT '所属会话（分表键）',
    seq        BIGINT       NOT NULL COMMENT '会话内单调递增序号',
    sender_uid BIGINT       NOT NULL COMMENT '发送者 uid',
    type       TINYINT      NOT NULL DEFAULT 1 COMMENT '1 text / 2 image / 3 file / 4 voice / 5 video / 6 system',
    content    TEXT         COMMENT '文本或结构化 JSON',
    extra      JSON         DEFAULT NULL COMMENT '扩展',
    status     TINYINT      NOT NULL DEFAULT 0 COMMENT '0 正常 / 1 已撤回',
    created_at DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (id),
    UNIQUE KEY uk_conv_seq (conv_id, seq),
    KEY idx_conv_time (conv_id, created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='消息分表1';

CREATE TABLE IF NOT EXISTS messages_2 (
    id         BIGINT       NOT NULL COMMENT '消息 ID（雪花，全局唯一）',
    conv_id    BIGINT       NOT NULL COMMENT '所属会话（分表键）',
    seq        BIGINT       NOT NULL COMMENT '会话内单调递增序号',
    sender_uid BIGINT       NOT NULL COMMENT '发送者 uid',
    type       TINYINT      NOT NULL DEFAULT 1,
    content    TEXT,
    extra      JSON         DEFAULT NULL,
    status     TINYINT      NOT NULL DEFAULT 0,
    created_at DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (id),
    UNIQUE KEY uk_conv_seq (conv_id, seq),
    KEY idx_conv_time (conv_id, created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='消息分表2';

CREATE TABLE IF NOT EXISTS messages_3 (
    id         BIGINT       NOT NULL COMMENT '消息 ID（雪花，全局唯一）',
    conv_id    BIGINT       NOT NULL COMMENT '所属会话（分表键）',
    seq        BIGINT       NOT NULL COMMENT '会话内单调递增序号',
    sender_uid BIGINT       NOT NULL COMMENT '发送者 uid',
    type       TINYINT      NOT NULL DEFAULT 1,
    content    TEXT,
    extra      JSON         DEFAULT NULL,
    status     TINYINT      NOT NULL DEFAULT 0,
    created_at DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (id),
    UNIQUE KEY uk_conv_seq (conv_id, seq),
    KEY idx_conv_time (conv_id, created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='消息分表3';

-- 已读状态表：每成员每会话最后已读 seq
CREATE TABLE IF NOT EXISTS message_reads (
    uid            BIGINT   NOT NULL COMMENT '成员 uid',
    conv_id        BIGINT   NOT NULL COMMENT '会话',
    last_read_seq  BIGINT   NOT NULL DEFAULT 0 COMMENT '最后已读 seq',
    updated_at     DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    PRIMARY KEY (uid, conv_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='已读状态表';
