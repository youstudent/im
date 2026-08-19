-- 阶段五补充：单聊双方会话共享同一个会话 ID
-- 需求：A 与 B 建立单聊时，双方各自一条会话视图，但共享同一个会话 id（conv_id），
--       使双方历史消息归属到同一会话下。
-- 将 conversations 主键从 id 改为 (owner_uid, target_id)，id 变为业务会话 ID（可重复）。

-- 1) 删除旧的主键（id）
ALTER TABLE conversations DROP PRIMARY KEY;

-- 2) 将 id 改为普通业务会话 ID 列，并加索引
ALTER TABLE conversations ADD KEY idx_conv_id (id);

-- 3) 设置新的复合主键（owner_uid, target_id）
ALTER TABLE conversations ADD PRIMARY KEY (owner_uid, target_id);
