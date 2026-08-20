// 临时校验脚本：检查 SFC 模板表达式中的标识符是否在 script 中定义（粗检）
const fs = require('fs')

function check(file, extraKeywords = []) {
  const src = fs.readFileSync(file, 'utf8')
  const tpl = src.slice(src.indexOf('<template>'), src.indexOf('</template>'))
  const scr = src.slice(0, src.indexOf('<template>'))
  const exprs = []
  const re = /(?:v-model(?::[\w-]+)?|:[\w.-]+|@[\w.-]+|v-if|v-else-if|v-for|v-show)="([^"]+)"|\{\{\s*([^{}]+?)\s*\}\}/g
  let m
  while ((m = re.exec(tpl))) exprs.push(m[1] || m[2])
  const ids = new Set()
  const idRe = /[A-Za-z_$][\w$]*/g
  for (const e of exprs) for (const id of e.match(idRe) || []) ids.add(id)
  const keywords = new Set([
    'true', 'false', 'null', 'undefined', 'typeof', 'in', 'of', 'if', 'else', 'return', 'new',
    'String', 'Number', 'Math', 'Date', 'JSON', 'parseInt', 'parseFloat', 'isNaN',
    'encodeURIComponent', 'decodeURIComponent', 'console', 'window', 'document', 'navigator',
    'Array', 'Object', 'Set', 'Map', 'Promise', 'alert', 'confirm',
    // 模板局部变量（v-for 项 / 事件参数）
    'e', 'ev', 'm', 'c', 's', 'f', 'i', 'meta', 'msg', 'dayGroup', 'member', 'tile', 'ti', 'mi', 'fi',
    ...extraKeywords,
  ])
  const missing = []
  for (const id of ids) {
    if (keywords.has(id)) continue
    if (scr.includes(id)) continue
    missing.push(id)
  }
  console.log(file.split('/').pop() + ' MISSING: ' + (missing.join(', ') || 'none'))
}

const base = 'e:/vibe-coding/new_im/desktop_client/src/components/'
check(base + 'MainWindow.vue', ['gp', 'media', 'staged', 'call', 'voicePlayer', 'msgMenuApi', 'toastState'])
check(base + 'MessageBubble.vue', ['msg', 'side', 'voice', 'emit', 'props'])
check(base + 'GroupProfilePanel.vue', ['gp', 'muteDnd', 'isNoPersist', 'emit'])
check(base + 'SingleProfilePanel.vue', ['meta', 'friendInfo', 'remark', 'remarkSaving', 'muteDnd', 'isNoPersist', 'emit', 'editingRemark', 'remarkDraft'])
check(base + 'InviteMembersModal.vue', ['emit', 'inviteSearch', 'filteredInviteFriends', 'selectedInviteUIDs', 'inviting'])
check(base + 'GroupSettingsModal.vue', ['emit', 'gsName', 'gsAnnouncement', 'gsSaving', 'isAdmin'])
check(base + 'LeaveGroupConfirm.vue', ['emit', 'groupName', 'leaving'])
check(base + 'EmojiPanel.vue', ['emit', 'emojiList'])
