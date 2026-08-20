// 群聊资料面板：群成员/群资料加载缓存、群名与公告编辑、邀请成员、退出群聊
import { ref, computed, watch } from 'vue'
import { friendApi, groupApi } from '../api/social'
import { localdb } from '../api/localdb'
import { MOCK_NAME_POOL, MOCK_COLOR_POOL } from '../utils/palette'

export function useGroupPanel(ctx) {
  const { currentContact, conversations, activeId, showProfile, realConvMap, noPersistSet, showToast } = ctx

  // 群聊展示名称（去掉 "(28)" 成员数后缀）
  const groupDisplayName = computed(() =>
    currentContact.value.name.replace(/\s*\(\d+\)\s*$/, '')
  )

  // 由 memberCount 补全出一个完整成员列表（保证 >= 显示数量）
  function buildFullMembers(c) {
    const seed = (c.members ?? []).map((m) => ({ ...m, online: m.online ?? true }))
    const count = c.memberCount ?? Math.max(seed.length, 1)
    if (seed.length >= count) return seed
    const rest = []
    for (let i = 0; i < count - seed.length; i++) {
      const name = MOCK_NAME_POOL[(seed.length + i) % MOCK_NAME_POOL.length]
      rest.push({
        name,
        avatar: name[0],
        color: MOCK_COLOR_POOL[(seed.length + i) % MOCK_COLOR_POOL.length],
        online: (seed.length + i) % 3 !== 0,
      })
    }
    return [...seed, ...rest]
  }

  // 群成员搜索关键字
  const memberSearch = ref('')

  // 从后端加载的当前群真实成员（uid / 昵称 / 头像），替代 mock 补全
  const liveGroupMembers = ref([])
  // 群成员缓存：gUid → 成员数组。避免同一群聊会话反复打开时重复请求 /groups/:gid（群详情接口）。
  // 仅在打开群聊会话（真正需要展示群资料时）才首次请求；群成员变化（如邀请）时失效对应缓存。
  const groupMembersCache = new Map()

  // ===== 邀请成员 =====
  const showInviteModal = ref(false) // 邀请弹框开关

  function openInviteModal() {
    const c = currentContact.value
    if (!c || c.type !== 'group' || !c.targetId) return
    showInviteModal.value = true
  }

  // 邀请完成：失效该群缓存并刷新群成员（邀请不改变自己的会话列表，无需全量重载——阶段二减压）
  async function onInvited() {
    showInviteModal.value = false
    const c = currentContact.value
    if (c && c.targetId) groupMembersCache.delete(c.targetId)
    await loadLiveGroupMembers()
  }

  // 加载当前群的真实成员（群详情接口）。
  // 仅在真正需要展示群资料（打开群聊会话）时才请求 /groups/:gid；同一群已加载过则复用缓存，不重复请求。
  async function loadLiveGroupMembers() {
    const c = currentContact.value
    if (!c || c.type !== 'group') return
    const gUid = c.targetId
    if (!gUid) return
    // 该群已加载过：直接复用缓存（成员 + 群资料），避免反复打开同一群聊时重复请求群详情接口
    if (groupMembersCache.has(gUid)) {
      const cached = groupMembersCache.get(gUid)
      liveGroupMembers.value = cached.members
      groupInfo.value = cached.info
      return
    }
    try {
      const g = await groupApi.get(gUid)
      const uids = Array.isArray(g.members) ? g.members : []
      // 通过好友列表构建 uid→昵称 映射
      const friendList = await friendApi.list()
      const nameMap = {}
      ;(friendList || []).forEach((f) => {
        nameMap[f.uid] = f.remark || f.nickname || `用户${f.uid}`
      })
      const members = uids.map((uid, i) => ({
        uid,
        name: nameMap[uid] || `成员${uid}`,
        avatar: (nameMap[uid] || '?')[0],
        color: MOCK_COLOR_POOL[i % MOCK_COLOR_POOL.length],
      }))
      // 群资料：名称/公告/我的角色（0 群主 / 1 管理员 / 2 成员），角色决定群设置可编辑性
      const info = {
        name: g.name || '',
        announcement: g.announcement || '',
        myRole: g.my_role != null ? Number(g.my_role) : 2,
      }
      liveGroupMembers.value = members
      groupInfo.value = info
      groupMembersCache.set(gUid, { members, info })
    } catch (e) {
      console.warn('[GroupPanel] 加载群成员失败:', e?.message || e)
    }
  }

  // 群组信息元数据（头像组合、成员、公告、文件）
  const groupMeta = computed(() => {
    const c = currentContact.value
    const allMembers = liveGroupMembers.value.length ? liveGroupMembers.value : buildFullMembers(c)
    return {
      groupId: c.groupId ?? '',
      memberCount: allMembers.length,
      members: allMembers,
      announcement: groupInfo.value.announcement || c.announcement || '',
      files: c.files ?? [],
      avatarTiles: allMembers.slice(0, 4).map((m) => m.color),
    }
  })

  // ===== 群设置（群名/群公告）：仅群主或管理员可编辑（后端鉴权兜底） =====
  // 当前群资料（名称/公告/我的角色），loadLiveGroupMembers 时填充
  const groupInfo = ref({})
  // 群主或管理员（role 0/1）才可编辑群设置
  const isGroupAdmin = computed(() => groupInfo.value.myRole != null && groupInfo.value.myRole <= 1)

  const editingGroupName = ref(false)
  const groupNameDraft = ref('')
  const editingAnnouncement = ref(false)
  const announcementDraft = ref('')
  const savingGroupInfo = ref(false)

  function startEditGroupName() {
    groupNameDraft.value = currentContact.value.name || ''
    editingGroupName.value = true
  }

  function startEditAnnouncement() {
    announcementDraft.value = groupMeta.value.announcement || ''
    editingAnnouncement.value = true
  }

  // 保存群设置（面板内联编辑）：只提交处于编辑态的字段，另一字段保持当前值；成功后本地即时生效
  async function saveGroupSettings() {
    const c = currentContact.value
    if (!c || c.type !== 'group' || !c.targetId || savingGroupInfo.value) return
    const name = (editingGroupName.value ? groupNameDraft.value : c.name || '').trim()
    const announcement = (editingAnnouncement.value ? announcementDraft.value : groupInfo.value.announcement || '').trim()
    if (!name) {
      showToast('群名不能为空', 'error')
      return
    }
    savingGroupInfo.value = true
    try {
      await groupApi.update(c.targetId, name, announcement)
      showToast('群设置已保存', 'success')
      editingGroupName.value = false
      editingAnnouncement.value = false
      applyGroupSettings(c, name, announcement)
    } catch (e) {
      showToast(e.message || '群设置保存失败', 'error')
    } finally {
      savingGroupInfo.value = false
    }
  }

  // 群设置本地即时生效：会话项名称、群资料缓存（面板内联编辑与设置弹窗共用）
  function applyGroupSettings(c, name, announcement) {
    c.name = name
    const info = { ...groupInfo.value, name, announcement }
    groupInfo.value = info
    const cached = groupMembersCache.get(c.targetId)
    if (cached) cached.info = info
  }

  // ===== 群设置弹窗：群名/群公告（管理员可编辑，普通成员只读） =====
  const showGroupSettings = ref(false)

  function openGroupSettings() {
    const c = currentContact.value
    if (!c || c.type !== 'group') return
    // 打开前确保群资料已加载（角色决定可编辑性）
    if (!groupMembersCache.has(c.targetId)) loadLiveGroupMembers()
    showGroupSettings.value = true
  }

  // 弹窗保存成功回调（接口调用在弹窗组件内完成）
  function onGroupSettingsSaved({ name, announcement }) {
    const c = currentContact.value
    if (!c) return
    applyGroupSettings(c, name, announcement)
    showToast('群设置已保存', 'success')
  }

  function onGroupSettingsFailed(msg) {
    showToast(msg || '群设置保存失败', 'error')
  }

  // ===== 退出群聊：二次确认弹窗 → 调后端退群 → 彻底清理本地相关信息 =====
  const leavingGroup = ref(false)
  const showLeaveConfirm = ref(false) // 退群二次确认弹窗开关

  function askLeaveGroup() {
    const c = currentContact.value
    if (!c || c.type !== 'group' || !c.targetId || leavingGroup.value) return
    showLeaveConfirm.value = true
  }

  async function confirmLeaveGroup() {
    if (leavingGroup.value) return
    leavingGroup.value = true
    showLeaveConfirm.value = false
    try {
      const c = currentContact.value
      await groupApi.leave(c.targetId)
      // 清理本地：会话项、本地库会话行与全部消息、群资料缓存、conv_id 映射
      const convIdStr = String(c.convId || '')
      conversations.value = conversations.value.filter((x) => x.id !== c.id)
      if (convIdStr) {
        localdb.messages.removeByConv(convIdStr)
        localdb.conversations.remove(convIdStr)
        noPersistSet.value.delete(convIdStr)
      }
      delete realConvMap.value[c.id]
      groupMembersCache.delete(c.targetId)
      groupInfo.value = {}
      showProfile.value = false
      if (activeId.value === c.id) activeId.value = ''
    } catch (e) {
      alert(e.message || '退出群聊失败')
    } finally {
      leavingGroup.value = false
    }
  }

  // 按关键字过滤后的成员列表（搜索用）
  const filteredMembers = computed(() => {
    const kw = memberSearch.value.trim().toLowerCase()
    if (!kw) return groupMeta.value.members
    return groupMeta.value.members.filter((m) =>
      (m.name ?? '').toLowerCase().includes(kw)
    )
  })

  // 切换会话时退出群设置编辑态，避免编辑状态串到另一个会话
  watch(activeId, () => {
    editingGroupName.value = false
    editingAnnouncement.value = false
  })

  return {
    groupDisplayName, memberSearch, liveGroupMembers, groupMembersCache,
    groupMeta, filteredMembers, groupInfo, isGroupAdmin,
    editingGroupName, groupNameDraft, editingAnnouncement, announcementDraft, savingGroupInfo,
    startEditGroupName, startEditAnnouncement, saveGroupSettings,
    showInviteModal, openInviteModal, onInvited,
    showGroupSettings, openGroupSettings, onGroupSettingsSaved, onGroupSettingsFailed,
    leavingGroup, showLeaveConfirm, askLeaveGroup, confirmLeaveGroup,
    loadLiveGroupMembers,
  }
}
