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
      // 通过好友列表构建 uid → 备注 / 用户昵称 映射（非好友两项均为空）
      const friendList = await friendApi.list()
      const remarkMap = {}
      const userNickMap = {}
      ;(friendList || []).forEach((f) => {
        remarkMap[f.uid] = f.remark || ''
        userNickMap[f.uid] = f.nickname || ''
      })
      const members = uids.map((uid, i) => {
        const remark = remarkMap[uid] || ''
        const userNickname = userNickMap[uid] || ''
        // 群内昵称（未设置为空）
        const groupNickname = (g.member_nicknames && g.member_nicknames[String(uid)]) || ''
        // 展示名优先级（微信规则）：好友备注 > 群内昵称 > 用户昵称
        const name = remark || groupNickname || userNickname || `成员${uid}`
        return {
          uid,
          name,
          remark,
          userNickname,
          avatar: name[0],
          color: MOCK_COLOR_POOL[i % MOCK_COLOR_POOL.length],
          // 成员角色（0 群主 / 1 管理员 / 2 成员）：后端 member_roles 的 JSON key 为字符串 uid
          role: g.member_roles && g.member_roles[String(uid)] != null ? Number(g.member_roles[String(uid)]) : 2,
          nickname: groupNickname,
          // G8 禁言截止（unix 毫秒，0=未禁言）：member_mutes 的 JSON key 为字符串 uid
          mutedUntil: g.member_mutes ? Number(g.member_mutes[String(uid)] || 0) : 0,
        }
      })
      // 展示顺序：群主(0) → 管理员(1) → 普通成员(2)；后端已按入群时间返回，
      // 稳定排序保证同角色内保持入群先后
      members.sort((a, b) => a.role - b.role)
      // 群资料：名称/公告/我的角色（0 群主 / 1 管理员 / 2 成员），角色决定群设置可编辑性；
      // ownerUid 供成员管理权限判定；myNickname 供群昵称编辑入口回显；
      // inviteConfirm/muteAll/saved 为 P2 群设置开关（G7/G8/G10）
      const info = {
        name: g.name || '',
        announcement: g.announcement || '',
        myRole: g.my_role != null ? Number(g.my_role) : 2,
        ownerUid: g.owner_uid != null ? String(g.owner_uid) : '',
        myNickname: g.my_nickname || '',
        inviteConfirm: g.invite_confirm != null ? Number(g.invite_confirm) : 0,
        muteAll: g.mute_all != null ? Number(g.mute_all) : 0,
        saved: g.saved != null ? Number(g.saved) : 1,
        // G8 我的禁言截止（unix 毫秒，0=未禁言）：决定输入框禁用态
        myMutedUntil: g.my_muted_until ? Number(g.my_muted_until) : 0,
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

  // 弹窗保存成功回调（接口调用在弹窗组件内完成）：群名/公告 + 入群确认/全员禁言/保存到通讯录开关
  function onGroupSettingsSaved({ name, announcement, inviteConfirm, muteAll, saved }) {
    const c = currentContact.value
    if (!c) return
    applyGroupSettings(c, name, announcement)
    const info = {
      ...groupInfo.value,
      name,
      announcement,
      inviteConfirm: inviteConfirm != null ? inviteConfirm : groupInfo.value.inviteConfirm,
      muteAll: muteAll != null ? muteAll : groupInfo.value.muteAll,
      saved: saved != null ? saved : groupInfo.value.saved,
    }
    groupInfo.value = info
    const cached = groupMembersCache.get(c.targetId)
    if (cached) cached.info = info
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

  // 成员展示名（微信优先级）：好友备注 > 群内昵称 > 用户昵称；
  // 备注/群昵称变更时只要更新对应字段，渲染处调用本函数即可同步刷新
  function memberDisplayName(m) {
    if (!m) return ''
    return m.remark || m.nickname || m.userNickname || m.name || ''
  }

  // 按关键字过滤后的成员列表（搜索用）：按展示名（备注优先）匹配，与界面所见一致
  const filteredMembers = computed(() => {
    const kw = memberSearch.value.trim().toLowerCase()
    if (!kw) return groupMeta.value.members
    return groupMeta.value.members.filter((m) =>
      memberDisplayName(m).toLowerCase().includes(kw)
    )
  })

  // ===== 成员管理：移除成员 / 设撤管理员（后端鉴权兑底，前端按角色控制入口可见性） =====
  const isGroupOwner = computed(() => groupInfo.value.myRole === 0)

  // 当前登录 uid（与 MainWindow.meUid 同源：localStorage 登录信息）
  function myUid() {
    try {
      return String(JSON.parse(localStorage.getItem('workchat:me') || '{}').uid || '')
    } catch {
      return ''
    }
  }

  // 是否可管理某成员（微信规则）：群主可管理除自己外所有人；管理员仅可管理普通成员；
  // 自己与群主永远不可被管理
  function canOperateMember(member) {
    const my = groupInfo.value.myRole
    if (my == null || my > 1) return false
    if (!member || member.uid == null) return false
    if (String(member.uid) === myUid()) return false
    if (member.role === 0 || String(member.uid) === groupInfo.value.ownerUid) return false
    if (my === 1 && member.role === 1) return false // 管理员不可动其他管理员
    return true
  }

  // 移除成员：二次确认后调接口，成功即时从成员列表移除并同步缓存
  async function removeMember(member) {
    const c = currentContact.value
    if (!c || c.type !== 'group' || !member || member.uid == null) return
    if (!confirm(`确定将“${member.name}”移出群聊吗？`)) return
    try {
      await groupApi.kick(c.targetId, member.uid)
      liveGroupMembers.value = liveGroupMembers.value.filter((m) => m.uid !== member.uid)
      const cached = groupMembersCache.get(c.targetId)
      if (cached) cached.members = liveGroupMembers.value
      showToast(`已将 ${member.name} 移出群聊`, 'success')
    } catch (e) {
      showToast(e.message || '移除成员失败', 'error')
    }
  }

  // 设为/取消管理员（仅群主）：role 1 设为管理员 / 2 撤销，成功即时更新角色标签
  async function setMemberRole(member, role) {
    const c = currentContact.value
    if (!c || c.type !== 'group' || !member || member.uid == null) return
    try {
      await groupApi.setRole(c.targetId, member.uid, role)
      member.role = role
      const cached = groupMembersCache.get(c.targetId)
      if (cached) cached.members = liveGroupMembers.value
      showToast(role === 1 ? `已将 ${member.name} 设为管理员` : `已撤销 ${member.name} 的管理员`, 'success')
    } catch (e) {
      showToast(e.message || '设置管理员失败', 'error')
    }
  }

  // 禁言/解除禁言（G8，仅群主/管理员）：until unix 毫秒，0=解除；成功即时更新成员禁言状态
  async function muteMember(member, until) {
    const c = currentContact.value
    if (!c || c.type !== 'group' || !member || member.uid == null) return
    try {
      await groupApi.muteMember(c.targetId, member.uid, until)
      member.mutedUntil = Number(until) || 0
      const cached = groupMembersCache.get(c.targetId)
      if (cached) cached.members = liveGroupMembers.value
      showToast(until > 0 ? `已将 ${member.name} 禁言` : `已解除 ${member.name} 的禁言`, 'success')
    } catch (e) {
      showToast(e.message || '设置禁言失败', 'error')
    }
  }

  // ===== 转让群主（仅群主）：成员选择弹窗 → 确认转让 → 重载群资料（角色即时变化） =====
  const showTransferModal = ref(false)
  const transferring = ref(false)

  function openTransferModal() {
    const c = currentContact.value
    if (!c || c.type !== 'group' || !isGroupOwner.value) return
    showTransferModal.value = true
  }

  async function confirmTransfer(member) {
    const c = currentContact.value
    if (!c || c.type !== 'group' || !member || member.uid == null || transferring.value) return
    transferring.value = true
    try {
      await groupApi.transferOwner(c.targetId, member.uid)
      showTransferModal.value = false
      showToast(`已将群主转让给 ${member.name}`, 'success')
      // 失效缓存并重载：我的角色变为普通成员，管理入口即时收回
      groupMembersCache.delete(c.targetId)
      await loadLiveGroupMembers()
    } catch (e) {
      showToast(e.message || '转让群主失败', 'error')
    } finally {
      transferring.value = false
    }
  }

  // ===== 我的群内昵称：内联编辑 → 保存后本地即时生效（他人侧由 WS 事件同步） =====
  const editingMyNickname = ref(false)
  const myNicknameDraft = ref('')
  const savingNickname = ref(false)

  function startEditMyNickname() {
    myNicknameDraft.value = groupInfo.value.myNickname || ''
    editingMyNickname.value = true
  }

  async function saveMyNickname() {
    const c = currentContact.value
    if (!c || c.type !== 'group' || savingNickname.value) return
    const nick = myNicknameDraft.value.trim()
    savingNickname.value = true
    try {
      await groupApi.setMyNickname(c.targetId, nick)
      groupInfo.value = { ...groupInfo.value, myNickname: nick }
      const cached = groupMembersCache.get(c.targetId)
      if (cached) cached.info = groupInfo.value
      editingMyNickname.value = false
      showToast(nick ? '群昵称已保存' : '群昵称已清除', 'success')
    } catch (e) {
      showToast(e.message || '群昵称保存失败', 'error')
    } finally {
      savingNickname.value = false
    }
  }

  // 切换会话时退出群设置编辑态，避免编辑状态串到另一个会话
  watch(activeId, () => {
    editingGroupName.value = false
    editingAnnouncement.value = false
  })

  return {
    groupDisplayName, memberSearch, liveGroupMembers, groupMembersCache,
    groupMeta, filteredMembers, memberDisplayName, groupInfo, isGroupAdmin, isGroupOwner,
    canOperateMember, removeMember, setMemberRole, muteMember,
    showTransferModal, transferring, openTransferModal, confirmTransfer,
    editingMyNickname, myNicknameDraft, savingNickname, startEditMyNickname, saveMyNickname,
    editingGroupName, groupNameDraft, editingAnnouncement, announcementDraft, savingGroupInfo,
    startEditGroupName, startEditAnnouncement, saveGroupSettings,
    showInviteModal, openInviteModal, onInvited,
    showGroupSettings, openGroupSettings, onGroupSettingsSaved, onGroupSettingsFailed,
    leavingGroup, showLeaveConfirm, askLeaveGroup, confirmLeaveGroup,
    loadLiveGroupMembers,
  }
}
