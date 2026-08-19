/**
 * HTTP 客户端封装：统一 baseURL、鉴权头注入、401 自动刷新令牌重试。
 * 使用 fetch，服务端返回统一结构 { code, message, data }（code===0 成功）。
 */
import { tokenStore } from './token'

// 服务端地址：开发模式指向本地 Go 服务。生产可替换为线上域名。
const BASE_URL = 'http://127.0.0.1:8080/api/v1'

// 通用错误，携带 message 供 UI 展示。
export class ApiError extends Error {
  constructor(message, code) {
    super(message)
    this.name = 'ApiError'
    this.code = code
  }
}

// 登录失效：通知全局（App 弹框提示并跳转登录页）
function emitAuthExpired() {
  if (typeof window !== 'undefined') {
    window.dispatchEvent(new CustomEvent('auth:expired', { detail: { source: 'http' } }))
  }
}

let refreshPromise = null

// 刷新 access token 并落盘（并发去重，多个 401 只触发一次）。
async function doRefresh() {
  const refreshToken = await tokenStore.getRefreshToken()
  if (!refreshToken) {
    throw new ApiError('登录已过期，请重新登录', 401)
  }
  const res = await fetch(`${BASE_URL}/auth/refresh`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ refresh_token: refreshToken }),
  })
  const body = await res.json()
  if (!res.ok || body.code !== 0) {
    throw new ApiError(body.message || '刷新登录态失败', body.code)
  }
  // 落盘前校验会话未变化：若期间用户已退出登录/令牌已被清除（当前 refresh 与发起时不一致），
  // 丢弃刷新结果，防止在途请求把已清理的令牌“复活”回 localStorage
  const currentRefresh = await tokenStore.getRefreshToken()
  if (currentRefresh !== refreshToken) {
    throw new ApiError('登录态已变化', 401)
  }
  const remember = tokenStore.getRemember()
  await tokenStore.save({
    accessToken: body.data.access_token,
    refreshToken: body.data.refresh_token,
    remember,
  })
  return body.data.access_token
}

function ensureRefresh() {
  if (!refreshPromise) {
    refreshPromise = doRefresh().finally(() => {
      refreshPromise = null
    })
  }
  return refreshPromise
}

// 启动时静默刷新：用 refresh token 换新 access（并发去重，与 401 重试共享同一刷新 Promise）。
// 失败（离线/refresh 失效）不抛错、不清令牌，由调用方决定降级策略。
export async function trySilentRefresh() {
  try {
    await ensureRefresh()
    return true
  } catch {
    return false
  }
}

async function request(path, { method = 'GET', data, auth = true, retry = true } = {}) {
  const headers = { 'Content-Type': 'application/json' }
  let tokenUsed = ''
  if (auth) {
    tokenUsed = (await tokenStore.getAccessToken()) || ''
    if (tokenUsed) headers['Authorization'] = `Bearer ${tokenUsed}`
  }

  let res
  try {
    res = await fetch(`${BASE_URL}${path}`, {
      method,
      headers,
      body: data ? JSON.stringify(data) : undefined,
    })
  } catch (e) {
    throw new ApiError('网络异常，请检查服务是否已启动', 0)
  }

  // 401 且携带了 token：尝试刷新后重试一次
  if (res.status === 401 && auth && retry) {
    try {
      await ensureRefresh()
      return request(path, { method, data, auth, retry: false })
    } catch (e) {
      // 刷新失败：若令牌已被新登录/新刷新替换（当前 token 与本请求使用的不同），
      // 本请求属于陈旧请求——不得清空新令牌、不得报登录失效（防刚登录就被陈旧 401 杀会话）
      const current = await tokenStore.getAccessToken()
      if (current && current !== tokenUsed) {
        throw new ApiError('请求已过期', 401)
      }
      tokenStore.clear()
      emitAuthExpired()
      throw e
    }
  }

  let body = {}
  try {
    body = await res.json()
  } catch {
    /* 非 JSON 响应 */
  }

  if (!res.ok || (body.code !== undefined && body.code !== 0)) {
    // 401 未带 token 或重试后仍失败：仅当令牌未被新登录替换时才清会话并报失效
    if (res.status === 401) {
      const current = await tokenStore.getAccessToken()
      if (!current || current === tokenUsed) {
        tokenStore.clear()
        emitAuthExpired()
      }
    }
    throw new ApiError(body.message || `请求失败 (${res.status})`, body.code ?? res.status)
  }
  return body.data
}

export const http = {
  get: (path, opts) => request(path, { ...opts, method: 'GET' }),
  post: (path, data, opts) => request(path, { ...opts, method: 'POST', data }),
  put: (path, data, opts) => request(path, { ...opts, method: 'PUT', data }),
  patch: (path, data, opts) => request(path, { ...opts, method: 'PATCH', data }),
  delete: (path, opts) => request(path, { ...opts, method: 'DELETE' }),
}
