/**
 * 认证接口：注册 / 登录 / 刷新 / 退出 / 二维码登录。
 * 服务端路径前缀 /auth，响应统一 { code, message, data }。
 */
import { http } from './http'

export const authApi = {
  register(data) {
    return http.post('/auth/register', data, { auth: false })
  },
  login(data) {
    return http.post('/auth/login', data, { auth: false })
  },
  refresh(refreshToken) {
    return http.post('/auth/refresh', { refresh_token: refreshToken }, { auth: false })
  },
  logout(refreshToken) {
    return http.post('/auth/logout', { refresh_token: refreshToken }, { auth: false })
  },
  createQR() {
    return http.post('/auth/qrcode/create', {}, { auth: false })
  },
  pollQR(qrcodeId) {
    return http.post('/auth/qrcode/poll', { qrcode_id: qrcodeId }, { auth: false })
  },
  confirmQR(qrcodeId, uid) {
    return http.post('/auth/qrcode/confirm', { qrcode_id: qrcodeId, uid }, { auth: false })
  },
}
