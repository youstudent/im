/**
 * 版本检查更新：查询服务端最新版本、版本号比较、打开下载地址。
 * 服务端接口：GET /api/v1/version/latest（公开接口，返回 { has, version }）。
 */
import { http } from './http'

export const versionApi = {
  // 最新版本（无需鉴权）
  latest() {
    return http.get('/version/latest', { auth: false })
  },
}

// 获取当前应用版本号：Electron 取 app.getVersion()，浏览器兜底 1.0.0
export async function getAppVersion() {
  try {
    if (typeof window !== 'undefined' && window.electronAPI?.getAppVersion) {
      const v = await window.electronAPI.getAppVersion()
      if (v) return String(v)
    }
  } catch {}
  return '1.0.0'
}

// 版本号比较：latest 比 current 新时返回 true（按数字段逐位比较，如 1.10.0 > 1.9.9）
export function isNewerVersion(latest, current) {
  const a = String(latest || '').split('.').map((n) => parseInt(n, 10) || 0)
  const b = String(current || '').split('.').map((n) => parseInt(n, 10) || 0)
  const len = Math.max(a.length, b.length)
  for (let i = 0; i < len; i++) {
    const x = a[i] || 0
    const y = b[i] || 0
    if (x > y) return true
    if (x < y) return false
  }
  return false
}

/**
 * 统一检查更新入口：
 * 返回 { hasNew, latest, offline }；网络失败静默降级（offline=true），不打断用户。
 */
export async function checkLatestVersion(currentVersion) {
  try {
    const data = await versionApi.latest()
    if (!data || !data.has || !data.version) return { hasNew: false, latest: null }
    const latest = data.version
    return { hasNew: isNewerVersion(latest.version, currentVersion), latest }
  } catch {
    return { hasNew: false, latest: null, offline: true }
  }
}

// 打开下载地址：Electron 走系统默认浏览器（主进程 shell.openExternal），浏览器环境直接开新标签
export function openDownloadUrl(url) {
  if (!url || !/^https?:\/\//.test(url)) return
  if (typeof window !== 'undefined' && window.electronAPI?.openExternal) {
    window.electronAPI.openExternal(url)
    return
  }
  window.open(url, '_blank', 'noopener')
}

// ---- 自动更新：下载安装包并静默安装（仅 Electron + Windows） ----

// 是否支持一键自动安装（下载 + 静默安装 + 重启）
export function autoUpdateAvailable() {
  return typeof window !== 'undefined' && !!window.electronAPI?.updater?.downloadAndInstall
}

// 注册下载进度回调（重复调用替换旧监听）：cb({ percent, received, total })
export function onInstallProgress(cb) {
  try {
    window.electronAPI?.updater?.onProgress?.(cb)
  } catch {}
}

// 下载并安装：返回 { ok, error? }；成功后应用会自动退出并由安装器接管。
// 自动把当前可执行文件路径传给主进程，静默安装时覆盖安装到原目录（兼容自定义安装位置）。
// sha256：发布记录中的安装包摘要，主进程下载后校验，不一致则中止安装（防篡改，审计 P1）。
export async function downloadAndInstall(url, sha256) {
  try {
    const installDir = typeof process !== 'undefined' && process.execPath ? process.execPath : ''
    return await window.electronAPI.updater.downloadAndInstall(url, installDir, sha256 || '')
  } catch (e) {
    return { ok: false, error: e?.message || String(e) }
  }
}
