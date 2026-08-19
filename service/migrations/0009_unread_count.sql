-- P2：会话未读数改由 unread_count 列维护（发消息累加、已读清零、撤回递减）
-- 替代旧"拉取时用 last_synced_seq - last_read_seq 差值计算"：
--   1. 消除撤回场景未读虚高（撤回时递减计数，而 seq 差值仍计入已撤回消息）；
--   2. 消除会话列表逐会话查 message_reads 的 N+1。

ALTER TABLE conversations ADD COLUMN unread_count INT NOT NULL DEFAULT 0 COMMENT '未读消息数（发消息累加，已读清零，撤回递减）' AFTER last_synced_seq;

-- 存量数据回填：按旧口径（已同步 seq - 已读 seq）一次性折算，下限 0
UPDATE conversations c SET c.unread_count = GREATEST(c.last_synced_seq - COALESCE((SELECT MAX(r.last_read_seq) FROM message_reads r WHERE r.uid = c.owner_uid AND r.conv_id = c.id), 0), 0) WHERE c.last_msg_id IS NOT NULL;
