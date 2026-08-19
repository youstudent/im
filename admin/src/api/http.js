// 管理后台 HTTP 客户端：baseURL /api/admin，统一带 admin token，401 跳登录。
const BASE = '/api/admin'

export class ApiError extends Error {
  constructor(message, code) {
    super(message)
    this.code = code
  }
}

function getToken() {
  return localStorage.getItem('wc_admin_token') || ''
}

export function setToken(t) {
  if (t) localStorage.setItem('wc_admin_token', t)
  else localStorage.removeItem('wc_admin_token')
}

export function isAuthed() {
  return !!getToken()
}

async function request(path, { method = 'GET', data } = {}) {
  const headers = { 'Content-Type': 'application/json' }
  const token = getToken()
  if (token) headers['Authorization'] = `Bearer ${token}`

  let res
  try {
    res = await fetch(`${BASE}${path}`, {
      method,
      headers,
      body: data ? JSON.stringify(data) : undefined,
    })
  } catch {
    throw new ApiError('网络异常，请检查服务是否已启动', 0)
  }

  let body = {}
  try {
    body = await res.json()
  } catch {
    /* ignore */
  }

  if (res.status === 401) {
    setToken('')
    throw new ApiError(body.message || '登录已过期', 401)
  }
  if (!res.ok || (body.code !== undefined && body.code !== 0)) {
    throw new ApiError(body.message || `请求失败 (${res.status})`, body.code ?? res.status)
  }
  return body.data
}

export const http = {
  get: (p) => request(p),
  post: (p, data) => request(p, { method: 'POST', data }),
  delete: (p) => request(p, { method: 'DELETE' }),
}
