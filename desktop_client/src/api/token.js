/**
 * 令牌安全存储：优先用 Electron safeStorage 系统级加密持久化，
 * 不可用时降级到 localStorage（非生产可接受）。
 *
 * 存储键：
 *  - workchat:token:access   access token（短期）
 *  - workchat:token:refresh  refresh token（长期，仅存密文）
 *  - workchat:token:remember 记住我开关
 */

const PREFIX = 'workchat:token:'

function getElectron() {
  return typeof window !== 'undefined' ? window.electronAPI?.secureStorage : null
}

async function encryptionAvailable() {
  const ss = getElectron()
  if (!ss) return false
  try {
    return !!(await ss.isEncryptionAvailable())
  } catch {
    return false
  }
}

// 加密态写入：优先 safeStorage 加密；加密不可用/失败时降级为明文，保证登录不丢失 token。
async function writeSecure(key, plain) {
  const ss = getElectron()
  try {
    const enc = await ss.encrypt(plain)
    if (enc && enc.ok) {
      localStorage.setItem(PREFIX + key, JSON.stringify({ v: enc.value, enc: true }))
      return true
    }
  } catch {
    /* 继续降级 */
  }
  // 降级：明文存储
  writePlain(key, plain)
  return true
}

async function readSecure(key) {
  const ss = getElectron()
  const raw = localStorage.getItem(PREFIX + key)
  if (!raw) return null
  try {
    const box = JSON.parse(raw)
    if (box.enc) {
      const dec = await ss.decrypt(box.v)
      if (dec && dec.ok) return dec.value
      // 加密读取失败（如密钥轮换）：返回 null 触发重新登录
      return null
    }
    return box.v
  } catch {
    return null
  }
}

function writePlain(key, value) {
  localStorage.setItem(PREFIX + key, JSON.stringify({ v: value, enc: false }))
}

function readPlain(key) {
  const raw = localStorage.getItem(PREFIX + key)
  if (!raw) return null
  try {
    return JSON.parse(raw).v
  } catch {
    return null
  }
}

export const tokenStore = {
  /** 保存登录令牌（refresh 走 safeStorage 加密，access 随会话）。 */
  async save({ accessToken, refreshToken, remember }) {
    if (accessToken) {
      if (remember) {
        await writeSecure('access', accessToken)
      } else {
        writePlain('access', accessToken)
      }
    }
    if (refreshToken) {
      if (remember) {
        await writeSecure('refresh', refreshToken)
      } else {
        writePlain('refresh', refreshToken)
      }
    }
    if (remember !== undefined) {
      localStorage.setItem(PREFIX + 'remember', remember ? '1' : '0')
    }
  },

  /** 读取 access token。 */
  async getAccessToken() {
    const remember = localStorage.getItem(PREFIX + 'remember') === '1'
    if (remember) return await readSecure('access')
    return readPlain('access')
  },

  /** 读取 refresh token。 */
  async getRefreshToken() {
    const remember = localStorage.getItem(PREFIX + 'remember') === '1'
    if (remember) return await readSecure('refresh')
    return readPlain('refresh')
  },

  getRemember() {
    return localStorage.getItem(PREFIX + 'remember') === '1'
  },

  /** 同步判断 refresh token 是否存在（不走 IPC 解密）。
   * 供 WS 断线等场景快速判断：避免应用退出瞬间 IPC 失败被误判为登录失效而清空令牌。 */
  hasRefreshToken() {
    try {
      return !!localStorage.getItem(PREFIX + 'refresh')
    } catch {
      return false
    }
  },

  /** 清空全部令牌。 */
  clear() {
    ;['access', 'refresh'].forEach((k) => localStorage.removeItem(PREFIX + k))
    localStorage.removeItem(PREFIX + 'remember')
  },
}
