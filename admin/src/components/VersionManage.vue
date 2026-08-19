<script setup>
import { ref, computed, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { adminApi } from '../api/admin'
import { setToken } from '../api/http'
import { useUi } from '../composables/useUi'

const router = useRouter()
const { showToast } = useUi()

// 登录态失效：清空令牌并跳转登录页
function handleUnauth() {
  setToken('')
  router.replace('/login')
}

// ---- 发布表单 ----
const form = ref({ version: '', download_url: '', sha256: '', release_notes: '' })
const publishing = ref(false)

// 计算文件 SHA-256（小写 hex）：客户端自动更新下载后校验此摘要，防安装包被篡改（审计 P1）
async function fileSha256(file) {
  const buf = await file.arrayBuffer()
  const digest = await crypto.subtle.digest('SHA-256', buf)
  return Array.from(new Uint8Array(digest))
    .map((b) => b.toString(16).padStart(2, '0'))
    .join('')
}

// ---- 安装包上传（预签名直传 OSS，成功后自动回填下载地址） ----
const fileInputRef = ref(null)
const uploading = ref(false)
const uploadStatus = ref('') // 展示当前上传文件名/状态
const MAX_SIZE = 500 * 1024 * 1024 // 500MB
const ALLOW_EXT = /\.(exe|msi|dmg|pkg|AppImage|deb|rpm|zip)$/i

function pickInstaller() {
  fileInputRef.value && fileInputRef.value.click()
}

async function onInstallerPicked(e) {
  const file = e.target.files && e.target.files[0]
  e.target.value = '' // 允许重复选择同一文件
  if (!file) return
  if (!ALLOW_EXT.test(file.name)) {
    showToast('仅支持安装包文件（.exe / .msi / .dmg / .zip 等）')
    return
  }
  if (file.size > MAX_SIZE) {
    showToast('安装包不能超过 500MB')
    return
  }
  uploading.value = true
  uploadStatus.value = `正在计算 ${file.name} 的 SHA-256…`
  try {
    // 0. 本地计算 SHA-256（发布时随版本记录，客户端自动更新下载后校验）
    const sha = await fileSha256(file)
    // 1. 预签名（Content-Type 与签名绑定，上传时必须一致）
    uploadStatus.value = `正在上传 ${file.name}…`
    const contentType = file.type || 'application/octet-stream'
    const presign = await adminApi.presignFile({
      file_name: file.name,
      type: 'installer',
      size: file.size,
      content_type: contentType,
    })
    if (!presign || !presign.upload_url) throw new Error('获取上传链接失败')
    // 2. PUT 直传 OSS
    const res = await fetch(presign.upload_url, {
      method: 'PUT',
      body: file,
      headers: { 'Content-Type': contentType },
    })
    if (!res.ok) throw new Error('上传失败，状态码 ' + res.status)
    // 3. 回填下载地址（公共读固定 URL，永不过期）与 SHA-256
    form.value.download_url = presign.download_url
    form.value.sha256 = sha
    uploadStatus.value = `已上传：${file.name}`
    showToast('安装包上传成功，下载地址与 SHA-256 已自动填入', 'ok')
  } catch (err) {
    if (err.code === 401) handleUnauth()
    uploadStatus.value = ''
    showToast(err.message || '上传失败', 'error')
  } finally {
    uploading.value = false
  }
}

async function publish() {
  const version = form.value.version.trim()
  const url = form.value.download_url.trim()
  const sha = form.value.sha256.trim().toLowerCase()
  if (!/^\d+\.\d+(\.\d+)?$/.test(version)) {
    showToast('版本号格式不正确，应形如 1.1.0')
    return
  }
  if (!url) {
    showToast('下载地址不能为空')
    return
  }
  if (!/^https?:\/\//.test(url)) {
    showToast('下载地址需以 http:// 或 https:// 开头')
    return
  }
  if (!/^[0-9a-f]{64}$/.test(sha)) {
    showToast('请填写安装包的 SHA-256（64 位十六进制；上传安装包时会自动计算）')
    return
  }
  publishing.value = true
  try {
    await adminApi.publishVersion({
      version,
      download_url: url,
      sha256: sha,
      release_notes: form.value.release_notes.trim(),
    })
    showToast(`版本 ${version} 发布成功，客户端检查更新即可收到`, 'ok')
    form.value = { version: '', download_url: '', sha256: '', release_notes: '' }
    versionsPage.value = 1
    loadVersions()
  } catch (e) {
    if (e.code === 401) handleUnauth()
    showToast(e.message || '发布失败', 'error')
  } finally {
    publishing.value = false
  }
}

// ---- 版本列表 ----
const versions = ref([])
const versionsTotal = ref(0)
const versionsPage = ref(1)
const pageSize = 10
const versionsPageCount = computed(() => Math.max(1, Math.ceil(versionsTotal.value / pageSize)))

function fmtTime(unixSec) {
  if (!unixSec) return '-'
  const d = new Date(unixSec * 1000)
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')} ${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`
}

async function loadVersions() {
  try {
    const offset = (versionsPage.value - 1) * pageSize
    const res = await adminApi.listVersions(offset, pageSize)
    versions.value = res.list || []
    versionsTotal.value = res.total || 0
  } catch (e) {
    if (e.code === 401) handleUnauth()
  }
}

onMounted(loadVersions)
</script>

<template>
  <section class="content">
    <h2>版本管理 <span class="count">已发布 {{ versionsTotal }} 个版本</span></h2>

    <!-- 发布新版本 -->
    <div class="toolbar" style="flex-direction: column; align-items: stretch; gap: 8px">
      <div style="display: flex; gap: 8px">
        <input v-model="form.version" class="search-input" style="max-width: 160px" placeholder="版本号，如 1.1.0" />
        <input v-model="form.download_url" class="search-input" placeholder="安装包下载地址（上传后自动填入，也可手填）" />
        <input v-model="form.sha256" class="search-input" style="max-width: 280px" placeholder="SHA-256（上传后自动计算）" />
        <button class="btn" type="button" :disabled="uploading" @click="pickInstaller">
          {{ uploading ? '上传中…' : '上传安装包' }}
        </button>
        <button class="btn" :disabled="publishing || uploading" @click="publish">
          {{ publishing ? '发布中…' : '发布版本' }}
        </button>
        <input
          ref="fileInputRef"
          type="file"
          accept=".exe,.msi,.dmg,.pkg,.zip,.deb,.rpm,.AppImage"
          style="display: none"
          @change="onInstallerPicked"
        />
      </div>
      <div v-if="uploadStatus" class="upload-status">{{ uploadStatus }}</div>
      <textarea
        v-model="form.release_notes"
        class="search-input"
        rows="3"
        style="resize: vertical"
        placeholder="更新说明（可选，将展示在客户端的更新弹框中）"
      ></textarea>
    </div>

    <!-- 已发布版本列表 -->
    <table class="table">
      <thead>
        <tr><th>版本号</th><th>下载地址</th><th>SHA-256</th><th>更新说明</th><th>发布者</th><th>发布时间</th></tr>
      </thead>
      <tbody>
        <tr v-for="v in versions" :key="v.version">
          <td>{{ v.version }}</td>
          <td class="url-cell" :title="v.download_url">{{ v.download_url }}</td>
          <td class="url-cell" :title="v.sha256">{{ v.sha256 ? v.sha256.slice(0, 12) + '…' : '-' }}</td>
          <td class="notes-cell" :title="v.release_notes">{{ v.release_notes || '-' }}</td>
          <td>{{ v.publisher || '-' }}</td>
          <td>{{ fmtTime(v.created_at) }}</td>
        </tr>
        <tr v-if="!versions.length"><td colspan="6" class="empty">暂无已发布版本</td></tr>
      </tbody>
    </table>
    <div class="pager">
      <button :disabled="versionsPage <= 1" @click="versionsPage--; loadVersions()">上一页</button>
      <span>{{ versionsPage }} / {{ versionsPageCount }}</span>
      <button :disabled="versionsPage >= versionsPageCount" @click="versionsPage++; loadVersions()">下一页</button>
    </div>
  </section>
</template>

<style scoped>
.url-cell {
  max-width: 260px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.notes-cell {
  max-width: 320px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.upload-status {
  font-size: 13px;
  color: #6b7280;
}
</style>
