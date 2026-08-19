/**
 * SQLCipher 数据库密钥管理：每账户一个随机 32 字节密钥（hex），
 * 用 Electron safeStorage 系统级加密后存于 {userData}/db-keyring.json。
 *
 * 设计依据 docs/桌面端本地存储方案.md 第 7 节（SQLCipher 全库加密进阶方案）：
 * - 密钥文件放在 userData 而非 storageRoot：密钥与数据分离，
 *   setStorageRoot 迁移存储目录时密钥不受影响
 * - safeStorage 不可用时降级存明文密钥并告警（与 src/api/token.js 的 token 降级策略一致）
 * - 密钥使用纯 hex 字符串，避免 PRAGMA key 的引号转义问题
 */
const path = require('node:path')
const fs = require('node:fs')
const crypto = require('node:crypto')
const { app, safeStorage } = require('electron')

const KEYRING_NAME = 'db-keyring.json'

function keyringPath() {
  return path.join(app.getPath('userData'), KEYRING_NAME)
}

function loadKeyring() {
  try {
    const data = JSON.parse(fs.readFileSync(keyringPath(), 'utf8'))
    if (data && typeof data === 'object') return data
  } catch {}
  return {}
}

function saveKeyring(ring) {
  try {
    fs.writeFileSync(keyringPath(), JSON.stringify(ring, null, 2), 'utf8')
  } catch (e) {
    console.warn('[store] 写入密钥文件失败:', e?.message || e)
  }
}

// 加密存储密钥；safeStorage 不可用时降级明文（与 token 存储同信任模型）
function encryptKey(keyHex) {
  if (safeStorage.isEncryptionAvailable()) {
    try {
      return { mode: 'safe', value: safeStorage.encryptString(keyHex).toString('base64') }
    } catch (e) {
      console.warn('[store] safeStorage 加密失败，降级明文密钥:', e?.message || e)
    }
  } else {
    console.warn('[store] safeStorage 不可用，数据库密钥将以明文存放')
  }
  return { mode: 'plain', value: keyHex }
}

function decryptKey(entry) {
  if (!entry || typeof entry.key !== 'string') return null
  if (entry.mode === 'plain') return entry.key
  try {
    return safeStorage.decryptString(Buffer.from(entry.key, 'base64'))
  } catch (e) {
    console.warn('[store] safeStorage 解密失败:', e?.message || e)
    return null
  }
}

// 获取某账户的数据库密钥；不存在则生成新的 32 字节随机密钥并加密持久化
function getKey(uid) {
  uid = String(uid || '').trim()
  if (!uid) throw new Error('getKey: uid 不能为空')
  const ring = loadKeyring()
  const existing = decryptKey(ring[uid])
  if (existing) return existing

  const keyHex = crypto.randomBytes(32).toString('hex')
  const { mode, value } = encryptKey(keyHex)
  ring[uid] = { mode, key: value, created: Date.now() }
  saveKeyring(ring)
  console.log(`[store] 已为账户 ${uid} 生成数据库密钥（${mode}）`)
  return keyHex
}

// 清除某账户的数据库密钥（账户本地数据清除时调用，避免残留密钥）
function removeKey(uid) {
  uid = String(uid || '').trim()
  if (!uid) return
  const ring = loadKeyring()
  if (ring[uid]) {
    delete ring[uid]
    saveKeyring(ring)
    console.log('[store] 已清除账户密钥:', uid)
  }
}

module.exports = { getKey, removeKey }
