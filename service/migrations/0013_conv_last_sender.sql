-- 0013_conv_last_sender.sql
-- 会话视图记录最后一条消息的发送者：群聊会话列表展示 "发送者: 内容" 前缀。
-- last_sender_name 为发送时的真实昵称快照（非好友成员无备注/群昵称可解析时的兜底）；
-- 系统消息/撤回等无发送者语义的场景写 0/空，客户端据此不加前缀。
ALTER TABLE conversations ADD COLUMN last_sender_uid BIGINT DEFAULT NULL COMMENT '最后消息发送者 uid（0/NULL 表示系统/无）';
ALTER TABLE conversations ADD COLUMN last_sender_name VARCHAR(40) DEFAULT NULL COMMENT '最后消息发送者昵称快照';
