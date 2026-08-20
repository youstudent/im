/**
 * 本地存储：仓储层（prepared statements）。
 * 所有操作作用于当前会话账户的库（多账户隔离由 db.js 的文件级会话保证）。
 */
const { getDb, getSessionUid } = require('./db')

// 不落盘（敏感）会话 id 集合：主进程强制拦截源头，渲染进程无法绕过
function noPersistIds(db) {
  return new Set(
    db.prepare('SELECT id FROM conversations WHERE no_persist = 1').all().map((r) => r.id)
  )
}

// ---- conversations ----
const conversationRepo = {
  // 按最后消息时间倒序（最新在最上）
  listByOwner() {
    const db = getDb()
    if (!db) return []
    return db.prepare('SELECT * FROM conversations WHERE owner_uid = ? ORDER BY last_msg_time DESC').all(getSessionUid())
  },

  // 批量 upsert：字段缺失（NULL）时保留本地已有值（COALESCE），防止部分行 upsert
  // （如 WS 到达时只更新摘要/未读）把 type/target_id/置顶免打扰等字段清空——
  // 历史 bug：活跃会话每收一条消息 target_id 被写成 NULL，本地秒开出现“用户 null”且无法发送。
  // 水位类字段（last_synced_seq / peer_read_seq）用 MAX 单调推进，永不回退。
  upsertMany(convs) {
    const db = getDb()
    if (!db || !Array.isArray(convs) || !convs.length) return 0
    const owner = getSessionUid()
    const npSet = noPersistIds(db)
    const stmt = db.prepare(`
      INSERT INTO conversations (id, owner_uid, type, target_id, last_msg, last_msg_time, unread, last_synced_seq, peer_read_seq, muted, pinned, last_sender_uid, last_sender_name)
      VALUES (@id, @owner_uid, @type, @target_id, @last_msg, @last_msg_time, @unread, @last_synced_seq, @peer_read_seq, @muted, @pinned, @last_sender_uid, @last_sender_name)
      ON CONFLICT(id) DO UPDATE SET
        type = COALESCE(excluded.type, conversations.type),
        target_id = COALESCE(excluded.target_id, conversations.target_id),
        last_msg = COALESCE(excluded.last_msg, conversations.last_msg),
        last_msg_time = COALESCE(excluded.last_msg_time, conversations.last_msg_time),
        unread = COALESCE(excluded.unread, conversations.unread),
        last_synced_seq = MAX(conversations.last_synced_seq, excluded.last_synced_seq),
        peer_read_seq = MAX(conversations.peer_read_seq, excluded.peer_read_seq),
        muted = COALESCE(excluded.muted, conversations.muted),
        pinned = COALESCE(excluded.pinned, conversations.pinned),
        last_sender_uid = COALESCE(excluded.last_sender_uid, conversations.last_sender_uid),
        last_sender_name = COALESCE(excluded.last_sender_name, conversations.last_sender_name)
    `)
    const tx = db.transaction((rows) => {
      for (const c of rows) stmt.run(c)
    })
    const rows = convs.map((c) => ({
      id: String(c.id),
      owner_uid: owner,
      // 部分行缺失的字段传 NULL：冲突时 COALESCE 保留本地已有值，不再覆盖成 NULL/0
      type: c.type != null ? Number(c.type) : null,
      target_id: c.target_id != null ? String(c.target_id) : null,
      last_msg: npSet.has(String(c.id)) ? '' : c.last_msg != null ? c.last_msg : null, // 不落盘会话不存摘要内容
      last_msg_time: c.last_msg_time != null ? Number(c.last_msg_time) : null,
      unread: c.unread != null ? Number(c.unread) : null,
      last_synced_seq: Number(c.last_synced_seq) || 0,
      peer_read_seq: c.peer_read_seq != null ? Number(c.peer_read_seq) : null,
      muted: c.muted != null ? Number(c.muted) : null,
      pinned: c.pinned != null ? Number(c.pinned) : null,
      // 最后消息发送者（群聊列表名称前缀用）：缺失传 NULL 保留本地已有值；系统/撤回传 '0'
      last_sender_uid: c.last_sender_uid != null ? String(c.last_sender_uid) : null,
      last_sender_name: c.last_sender_name != null ? String(c.last_sender_name) : null,
      // 注：marked_unread 不参与 upsert（列 NOT NULL，显式 NULL 会违反约束）：
      // 新行取 DEFAULT 0，状态变更走专用 setMarkedUnread
    }))
    tx(rows)
    return rows.length
  },

  // 设置会话不落盘标记；开启时立即清除该会话已落盘的全部消息
  setNoPersist(convId, flag) {
    const db = getDb()
    if (!db || !convId) return false
    const f = flag ? 1 : 0
    db.prepare('UPDATE conversations SET no_persist = ? WHERE id = ?').run(f, String(convId))
    if (f === 1) {
      db.prepare('DELETE FROM messages WHERE conv_id = ?').run(String(convId))
    }
    return true
  },

  // 收到新消息：更新摘要/时间，未读 +1（活跃会话由渲染进程随后清零）；
  // 不落盘会话只更新时间与未读，摘要置空；senderUid/senderName 记录最后消息发送者（系统/无传 '0'/''）
  bumpLastMessage(convId, lastMsg, lastMsgTime, senderUid, senderName) {
    const db = getDb()
    if (!db || !convId) return
    const row = db.prepare('SELECT no_persist FROM conversations WHERE id = ?').get(String(convId))
    if (row && row.no_persist === 1) {
      db.prepare(
        "UPDATE conversations SET last_msg = '', last_msg_time = ?, unread = unread + 1 WHERE id = ?"
      ).run(Number(lastMsgTime) || 0, String(convId))
      return
    }
    db.prepare(
      'UPDATE conversations SET last_msg = ?, last_msg_time = ?, unread = unread + 1, last_sender_uid = ?, last_sender_name = ? WHERE id = ?'
    ).run(
      lastMsg ?? '',
      Number(lastMsgTime) || 0,
      senderUid != null ? String(senderUid) : null,
      senderName != null ? String(senderName) : null,
      String(convId)
    )
  },

  setUnread(convId, n) {
    const db = getDb()
    if (!db || !convId) return
    db.prepare('UPDATE conversations SET unread = ? WHERE id = ?').run(Number(n) || 0, String(convId))
  },

  // 标记未读（纯本地状态）：手动挂起/清除红点，不与服务端同步
  setMarkedUnread(convId, flag) {
    const db = getDb()
    if (!db || !convId) return
    db.prepare('UPDATE conversations SET marked_unread = ? WHERE id = ?').run(flag ? 1 : 0, String(convId))
  },

  updateSyncSeq(convId, seq) {
    const db = getDb()
    if (!db || !convId) return
    db.prepare('UPDATE conversations SET last_synced_seq = MAX(last_synced_seq, ?) WHERE id = ?').run(
      Number(seq) || 0,
      String(convId)
    )
  },

  // 设置会话草稿（纯本地；空字符串/空值清除）
  setDraft(convId, draft) {
    const db = getDb()
    if (!db || !convId) return false
    const text = String(draft ?? '').trim() ? String(draft) : null
    db.prepare('UPDATE conversations SET draft = ? WHERE id = ?').run(text, String(convId))
    return true
  },

  // 设置会话置顶/免打扰（本地即时生效，与服务端 PUT /conversations/:id/settings 同步）
  setSettings(convId, { pinned, muted } = {}) {
    const db = getDb()
    if (!db || !convId) return false
    if (pinned != null) {
      db.prepare('UPDATE conversations SET pinned = ? WHERE id = ?').run(Number(pinned) ? 1 : 0, String(convId))
    }
    if (muted != null) {
      db.prepare('UPDATE conversations SET muted = ? WHERE id = ?').run(Number(muted) ? 1 : 0, String(convId))
    }
    return true
  },

  // 删除会话行（退群清理）；消息由 messageRepo.deleteByConv 单独清除
  remove(convId) {
    const db = getDb()
    if (!db || !convId) return false
    db.prepare('DELETE FROM conversations WHERE id = ?').run(String(convId))
    return true
  },
}

// ---- messages ----
const messageRepo = {
  // 会话历史：升序返回；beforeSeq 用于向上翻页（取更早的 limit 条再反转）；不落盘会话恒为空
  listByConv(convId, { beforeSeq, limit } = {}) {
    const db = getDb()
    if (!db || !convId) return []
    const np = db.prepare('SELECT no_persist FROM conversations WHERE id = ?').get(String(convId))
    if (np && np.no_persist === 1) return []
    const lim = Math.max(1, Math.min(Number(limit) || 50, 200))
    if (beforeSeq && Number(beforeSeq) > 0) {
      // pending（seq=0）消息不属于"更早历史"，翻页只查已同步消息
      return db
        .prepare(
          `SELECT * FROM messages WHERE conv_id = ? AND seq > 0 AND seq < ? ORDER BY seq DESC LIMIT ?`
        )
        .all(String(convId), Number(beforeSeq), lim)
        .reverse()
    }
    // 首页：最新 lim 条 = 已同步按 seq 倒序取 + 本地未同步(pending/failed) + 本地专属记录；
    // 合并按创建时间归并排序，保证发送失败消息按真实时间位置展示，而非固定堆在末尾。
    // 本地专属记录（seq=0 且 sync_state='synced'，如通话记录）：无服务端 seq 但已终结，
    // 与 pending 同属 seq=0，一并按时间并入首页（向上翻页不含，与 pending 一致）
    const synced = db
      .prepare('SELECT * FROM messages WHERE conv_id = ? AND seq > 0 ORDER BY seq DESC LIMIT ?')
      .all(String(convId), lim)
      .reverse()
    const local = db
      .prepare('SELECT * FROM messages WHERE conv_id = ? AND seq = 0 ORDER BY created_at ASC')
      .all(String(convId))
    return synced
      .concat(local)
      .sort((a, b) => (a.created_at - b.created_at) || ((a.seq || Number.MAX_SAFE_INTEGER) - (b.seq || Number.MAX_SAFE_INTEGER)))
  },

  // 消息搜索（查找聊天记录）：按关键字 LIKE 匹配 content，时间倒序。
  // convId 非空时仅搜该会话（当前会话内查找）；否则跨全部会话。
  // type 过滤：2 图片 / 3 文件 / 4 链接（内容含 URL）；排除不落盘会话、已撤回与系统消息；
  // 仅搜已同步消息（seq>0），避免 pending 行重复命中。
  // offset/limit 支持滚动分页；排序加 seq/local_id 次级键，保证同秒消息翻页顺序稳定。
  search(keyword, { type, limit, convId, offset } = {}) {
    const db = getDb()
    if (!db) return []
    const lim = Math.max(1, Math.min(Number(limit) || 50, 200))
    const off = Math.max(0, Number(offset) || 0)
    const params = []
    let sql = `
      SELECT m.* FROM messages m
      JOIN conversations c ON c.id = m.conv_id
      WHERE COALESCE(c.no_persist, 0) != 1 AND m.status != 1 AND m.type != 6 AND m.seq > 0
    `
    if (convId != null && String(convId) !== '') {
      sql += ' AND m.conv_id = ?'
      params.push(String(convId))
    }
    const t = Number(type) || 0
    if (t === 2 || t === 3) {
      sql += ' AND m.type = ?'
      params.push(t)
    } else if (t === 4) {
      sql += " AND (m.content LIKE 'http%' OR m.content LIKE '%www.%')"
    }
    const kw = String(keyword ?? '').trim()
    if (kw) {
      // 转义 LIKE 通配符，防止用户输入的 %/_ 被当作模式
      const escaped = kw.replace(/[\\%_]/g, (ch) => '\\' + ch)
      sql += " AND m.content LIKE ? ESCAPE '\\'"
      params.push(`%${escaped}%`)
    }
    sql += ' ORDER BY m.created_at DESC, m.seq DESC, m.local_id DESC LIMIT ? OFFSET ?'
    params.push(lim, off)
    return db.prepare(sql).all(...params)
  },

  // 批量写入（网络拉取/WS 推送统一入口）：按 conv_id + server_id 去重；不落盘会话的消息直接丢弃
  upsertMany(msgs) {
    const db = getDb()
    if (!db || !Array.isArray(msgs) || !msgs.length) return 0
    const npSet = noPersistIds(db)
    const stmt = db.prepare(`
      INSERT INTO messages (server_id, conv_id, seq, sender_uid, sender_name, type, content, extra, status, created_at, sync_state)
      VALUES (@server_id, @conv_id, @seq, @sender_uid, @sender_name, @type, @content, @extra, @status, @created_at, 'synced')
      ON CONFLICT(conv_id, server_id) DO UPDATE SET
        seq = excluded.seq,
        sender_uid = excluded.sender_uid,
        sender_name = excluded.sender_name,
        type = excluded.type,
        content = excluded.content,
        extra = excluded.extra,
        status = excluded.status,
        created_at = excluded.created_at,
        sync_state = 'synced'
    `)
    const tx = db.transaction((rows) => {
      for (const m of rows) stmt.run(m)
    })
    const rows = msgs
      .filter((m) => m && m.server_id != null && m.server_id !== '')
      .filter((m) => !npSet.has(String(m.conv_id))) // 不落盘会话拦截
      .map((m) => ({
        server_id: String(m.server_id),
        conv_id: String(m.conv_id),
        seq: Number(m.seq) || 0,
        sender_uid: m.sender_uid != null ? String(m.sender_uid) : null,
        sender_name: m.sender_name ?? '',
        type: Number(m.type) || 1,
        content: m.content ?? '',
        extra: m.extra ?? '',
        status: Number(m.status) || 0,
        created_at: Number(m.created_at) || 0,
      }))
    if (!rows.length) return 0
    tx(rows)
    return rows.length
  },

  // 离线发送入队：返回 local_id 供后续回填同步状态；不落盘会话不入队（无离线重发，仅内存乐观展示）
  appendPending(msg) {
    const db = getDb()
    if (!db || !msg) return null
    const np = db.prepare('SELECT no_persist FROM conversations WHERE id = ?').get(String(msg.conv_id))
    if (np && np.no_persist === 1) return null
    const info = db
      .prepare(`
        INSERT INTO messages (server_id, conv_id, seq, sender_uid, sender_name, type, content, extra, status, created_at, sync_state)
        VALUES (NULL, @conv_id, 0, @sender_uid, @sender_name, @type, @content, @extra, 0, @created_at, 'pending')
      `)
      .run({
        conv_id: String(msg.conv_id),
        sender_uid: msg.sender_uid != null ? String(msg.sender_uid) : null,
        sender_name: msg.sender_name ?? '',
        type: Number(msg.type) || 1,
        content: msg.content ?? '',
        extra: msg.extra ?? '',
        created_at: Number(msg.created_at) || 0,
      })
    return info.lastInsertRowid
  },

  // 回填同步状态：成功时带服务端 id/seq；localId 为空时按临时内容匹配（乐观消息兜底）
  setSyncState(localId, state, { serverId, seq, convId, createdAt } = {}) {
    const db = getDb()
    if (!db) return false
    if (localId) {
      if (serverId) {
        db.prepare('UPDATE messages SET sync_state = ?, server_id = ?, seq = ? WHERE local_id = ?').run(
          state,
          String(serverId),
          Number(seq) || 0,
          Number(localId)
        )
      } else {
        db.prepare('UPDATE messages SET sync_state = ? WHERE local_id = ?').run(state, Number(localId))
      }
      return true
    }
    // 兜底：按 conv_id + 时间匹配 pending 消息
    if (convId) {
      db.prepare(
        "UPDATE messages SET sync_state = ?, server_id = ?, seq = ? WHERE conv_id = ? AND created_at = ? AND sync_state = 'pending'"
      ).run(state, serverId ? String(serverId) : null, Number(seq) || 0, String(convId), Number(createdAt) || 0)
      return true
    }
    return false
  },

  // 离线发送队列（重连后重发）
  listPending() {
    const db = getDb()
    if (!db) return []
    return db.prepare("SELECT * FROM messages WHERE sync_state = 'pending' ORDER BY created_at ASC").all()
  },

  // 认领 pending 消息：重发后服务端回显到达时，按 conv_id + 内容匹配最早的 pending 行，
  // 避免回显 upsert 与 pending 行产生重复；未命中返回 null（调用方改走 upsert）。
  claimPending(convId, content) {
    const db = getDb()
    if (!db || !convId) return null
    const row = db
      .prepare(
        "SELECT * FROM messages WHERE conv_id = ? AND content = ? AND sync_state = 'pending' ORDER BY created_at ASC LIMIT 1"
      )
      .get(String(convId), content ?? '')
    return row || null
  },

  // 标记语音已播放：按 server_id 置 voice_played=1（雪花 ID 全局唯一）；
  // upsert 冲突更新不含该列，已标记状态不会被消息同步覆盖；pending 行无 server_id 自然忽略
  markVoicePlayed(serverId) {
    const db = getDb()
    if (!db || !serverId) return false
    const info = db
      .prepare('UPDATE messages SET voice_played = 1 WHERE server_id = ? AND voice_played = 0')
      .run(String(serverId))
    return info.changes > 0
  },

  // 删除某会话的全部消息（退群清理），返回删除行数
  deleteByConv(convId) {
    const db = getDb()
    if (!db || !convId) return 0
    const info = db.prepare('DELETE FROM messages WHERE conv_id = ?').run(String(convId))
    return info.changes || 0
  },

  // 批量删除消息（多选删除，仅本地视角不影响他人）：按 server_id / local_id 集合删除
  deleteMany(convId, { serverIds = [], localIds = [] } = {}) {
    const db = getDb()
    if (!db || !convId) return 0
    let n = 0
    if (serverIds.length) {
      const ph = serverIds.map(() => '?').join(',')
      const info = db
        .prepare(`DELETE FROM messages WHERE conv_id = ? AND server_id IN (${ph})`)
        .run(String(convId), ...serverIds.map(String))
      n += info.changes || 0
    }
    if (localIds.length) {
      const ph = localIds.map(() => '?').join(',')
      const info = db
        .prepare(`DELETE FROM messages WHERE conv_id = ? AND local_id IN (${ph})`)
        .run(String(convId), ...localIds.map(Number))
      n += info.changes || 0
    }
    return n
  },
}

// ---- kv ----
const kvRepo = {
  get(key) {
    const db = getDb()
    if (!db) return null
    const row = db.prepare('SELECT value FROM kv WHERE key = ?').get(String(key))
    return row ? row.value : null
  },
  set(key, value) {
    const db = getDb()
    if (!db) return false
    db.prepare('INSERT INTO kv(key, value) VALUES(?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value').run(
      String(key),
      value == null ? '' : String(value)
    )
    return true
  },
}

module.exports = { conversationRepo, messageRepo, kvRepo }
