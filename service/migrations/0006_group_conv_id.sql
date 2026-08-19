-- 阶段五补充：群表增加共享会话 ID（conv_id）
-- 需求：群聊所有成员共享同一个会话 ID（conv_id），用于邀请新成员时为其创建同一会话视图。
ALTER TABLE `groups` ADD COLUMN conv_id BIGINT DEFAULT NULL COMMENT '群聊统一会话 ID（雪花）' AFTER avatar;

-- 历史群数据兜底：无 conv_id 的群用其内部主键作为初始会话 ID
UPDATE `groups` SET conv_id = id WHERE conv_id IS NULL;
