-- 第二期群聊增强：群内昵称（我在本群的昵称）
-- 展示优先级（对齐微信）：好友备注 > 群昵称 > 用户昵称
ALTER TABLE group_members ADD COLUMN nickname VARCHAR(40) DEFAULT NULL COMMENT '群内昵称（为空回落用户昵称）';
