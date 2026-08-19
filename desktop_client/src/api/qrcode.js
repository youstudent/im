/**
 * 二维码渲染：基于 npm 包 qrcode 生成标准 QR，返回 0/1 二维矩阵供 UI 渲染。
 * 用 create() 获取模块矩阵，避免依赖 canvas（Electron 渲染进程可用，但矩阵更轻量统一）。
 */
import QRCode from 'qrcode'

/**
 * 生成二维码矩阵。
 * @param {string} text 二维码内容（如 workchat:qrcode:{id}）
 * @param {number} version 指定版本（可选，缺省自动选择）
 * @returns {number[][]} matrix[row][col]，1 表示深色模块
 */
export function generateQR(text, version) {
  const opts = { errorCorrectionLevel: 'M', margin: 1 }
  if (version) opts.version = version
  const qr = QRCode.create(text, opts)
  const size = qr.modules.size
  const matrix = []
  for (let r = 0; r < size; r++) {
    const row = []
    for (let c = 0; c < size; c++) {
      row.push(qr.modules.data[r * size + c] ? 1 : 0)
    }
    matrix.push(row)
  }
  return matrix
}
