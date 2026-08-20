-- 第三期（P2）群聊/单聊功能完善：
--   G7 入群确认：groups.invite_confirm（开启后成员邀请需群主/管理员同意）
--   G8 群禁言：groups.mute_all（全员禁言）+ group_members.muted_until（指定成员禁言截止，unix 毫秒，0/空=未禁言）
--   G10 保存到通讯录：group_members.saved（关闭后群不在通讯录群列表展示）
--   S6 表情回应：message_reactions 独立表（消息不可变 + extra 2KB 上限，故不入消息表）

ALTER TABLE `groups` ADD COLUMN invite_confirm TINYINT NOT NULL DEFAULT 0 COMMENT '邀请需确认（1=成员邀请需群主/管理员同意）' AFTER conv_id;
ALTER TABLE `groups` ADD COLUMN mute_all TINYINT NOT NULL DEFAULT 0 COMMENT '全员禁言（1=仅群主/管理员可发言）' AFTER invite_confirm;
ALTER TABLE group_members ADD COLUMN muted_until BIGINT DEFAULT NULL COMMENT '单人禁言截止（unix 毫秒，NULL/0=未禁言）' AFTER nickname;
ALTER TABLE group_members ADD COLUMN saved TINYINT NOT NULL DEFAULT 1 COMMENT '保存到通讯录（0=不在通讯录群列表展示）' AFTER muted_until;

CREATE TABLE IF NOT EXISTS message_reactions (
  id BIGINT PRIMARY KEY,
  conv_id BIGINT NOT NULL,
  msg_id BIGINT NOT NULL,
  uid BIGINT NOT NULL,
  emoji VARCHAR(16) NOT NULL,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  UNIQUE KEY uk_reaction (msg_id, uid, emoji),
  KEY idx_reaction_conv_msg (conv_id, msg_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='消息表情回应（S6）';
