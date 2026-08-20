// 临时校验脚本：检查 MainWindow 脚本区是否误引用已迁移走的符号（仅应通过 gp/staged/media/call/voicePlayer/msgMenuApi 访问）
const fs = require('fs')
const s = fs.readFileSync('e:/vibe-coding/new_im/desktop_client/src/components/MainWindow.vue', 'utf8')
const scr = s.slice(0, s.indexOf('<template>'))

// 这些符号已迁移走：MainWindow 脚本中不应出现（除注释外的裸引用都算残留）
const moved = [
  'emojiList', 'isAllowedFile', 'isAllowedImage', 'VOICE_MAX_SECONDS', 'MAX_UPLOAD_SIZE',
  'IMAGE_ACCEPT', 'FILE_ACCEPT', 'callLogText', 'buildFullMembers', 'convNameFallback',
  'function avatarColor', 'formatCallDuration', 'sameDay', 'formatDayLabel', 'computeDisplayMeta',
  'memberSearch =', 'liveGroupMembers =', 'editingGroupName', 'announcementDraft',
  'inviteFriends', 'selectedInviteUIDs', 'gsName', 'gsSaving', 'showLeaveConfirm =',
  'voicePlayedSet', 'voiceBubbleWidth', 'voiceDurationLabel', 'togglePlayVoice',
  'recording =', 'toggleRecordVoice', 'uploadingPreviewMap =', 'stagedFilesMap =',
  'function stageFiles', 'function removeStaged', 'function sendImage', 'function sendFile',
  'function sendStagedMedia', 'function openImage', 'function openVideo', 'function openFile',
  'function closeVideo', 'function onPlayerError', 'function onBubbleVideoError', 'function closeImagePreview',
  'function openMsgMenu', 'function copyMsgText', 'function canRecall', 'function recallMessage', 'function recallEdit',
  'function startVoiceCall', 'function startVideoCall', 'function insertCallLog',
  'function loadLiveGroupMembers', 'function openInviteModal', 'function confirmInvite',
  'function askLeaveGroup', 'function confirmLeaveGroup', 'function openGroupSettings',
  'function startEditGroupName', 'function startEditAnnouncement', 'function saveGroupSettings',
  'function toastState', 'MSG_TYPE =', 'SEND_TIMEOUT_MS =',
]
const bad = []
for (const n of moved) {
  if (scr.includes(n)) bad.push(n)
}
console.log('RESIDUAL DEFINITIONS:', bad.join(', ') || 'none')

// 使用到但必须存在（import 或本地定义）的符号
const must = [
  'formatConvTime', 'formatMsgTime', 'formatTimeDivider', 'formatUnread', 'formatFileSize',
  'groupMessagesByDay', 'messagePreview', 'convLastPreview', 'isHiddenLeaveMsg', 'toDbMessage',
  'toConvItem', 'createMessageMapper', 'isAudioMsg', 'isVideoMsg', 'MSG_TYPE', 'SEND_TIMEOUT_MS',
  'mapServerMessage', 'mapLocalMessage', 'meUid', 'friendDisplayName',
]
const miss = []
for (const n of must) {
  if (!scr.includes(n)) miss.push(n)
}
console.log('MUST EXIST MISSING:', miss.join(', ') || 'none')
