-- 审计 P0（弱口令防护）：默认管理员 admin/admin123 为种子账号，首次登录必须修改密码。
-- 存量管理员默认 0（不强制）；种子账号与检测到仍使用默认密码的 admin 置 1。
ALTER TABLE admin_users ADD COLUMN must_change_pwd TINYINT NOT NULL DEFAULT 0 COMMENT '1 首次登录必须修改密码（种子默认账号）' AFTER status;
