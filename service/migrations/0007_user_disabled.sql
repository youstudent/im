-- 用户表增加"账号状态"字段：0 正常 / 1 禁用
-- 背景：users.status 用于在线状态（0离线/1在线/2忙碌/3隐身），
--      禁用账号需独立字段承载，避免与在线状态冲突。
ALTER TABLE users ADD COLUMN disabled TINYINT NOT NULL DEFAULT 0 COMMENT '账号状态：0正常 1禁用' AFTER status;

-- 历史兜底：此前管理后台把 status=0 误用作"禁用"，迁移后统一用 disabled 表达禁用，
-- 在线状态字段 status 恢复其本义。默认所有存量用户为正常。
UPDATE users SET disabled = 0 WHERE disabled IS NULL;
