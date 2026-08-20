/**
 * 任务栏未读角标：纯像素绘制（红色胶囊底 + 白色数字）→ PNG Buffer。
 *
 * 背景：Electron 主进程无 DOM canvas，nativeImage 也不解码 SVG dataURL，
 * 因此用 5x7 点阵字体逐像素绘制后经 zlib 编码为 PNG（无任何第三方依赖）。
 * 产物交给 nativeImage.createFromBuffer → win.setOverlayIcon（Windows 任务栏覆盖图标）。
 */
const zlib = require('node:zlib')

// 5x7 点阵字体（0-9 与 +）：每行 5 位，'1' 为亮点
const FONT = {
  '0': ['01110', '10001', '10011', '10101', '11001', '10001', '01110'],
  '1': ['00100', '01100', '00100', '00100', '00100', '00100', '01110'],
  '2': ['01110', '10001', '00001', '00010', '00100', '01000', '11111'],
  '3': ['11111', '00010', '00100', '00010', '00001', '10001', '01110'],
  '4': ['00010', '00110', '01010', '10010', '11111', '00010', '00010'],
  '5': ['11111', '10000', '11110', '00001', '00001', '10001', '01110'],
  '6': ['00110', '01000', '10000', '11110', '10001', '10001', '01110'],
  '7': ['11111', '00001', '00010', '00100', '01000', '01000', '01000'],
  '8': ['01110', '10001', '10001', '01110', '10001', '10001', '01110'],
  '9': ['01110', '10001', '10001', '01111', '00001', '00010', '01100'],
  '+': ['00000', '00100', '00100', '11111', '00100', '00100', '00000'],
}

// ---- 最小 PNG 编码器（RGBA，filter=0）----
const CRC_TABLE = (() => {
  const t = new Int32Array(256)
  for (let n = 0; n < 256; n++) {
    let c = n
    for (let k = 0; k < 8; k++) c = c & 1 ? 0xedb88320 ^ (c >>> 1) : c >>> 1
    t[n] = c
  }
  return t
})()

function crc32(buf) {
  let crc = -1
  for (let i = 0; i < buf.length; i++) crc = (crc >>> 8) ^ CRC_TABLE[(crc ^ buf[i]) & 0xff]
  return (crc ^ -1) >>> 0
}

function pngChunk(type, data) {
  const len = Buffer.alloc(4)
  len.writeUInt32BE(data.length)
  const body = Buffer.concat([Buffer.from(type, 'ascii'), data])
  const crc = Buffer.alloc(4)
  crc.writeUInt32BE(crc32(body))
  return Buffer.concat([len, body, crc])
}

function encodePNG(w, h, rgba) {
  const sig = Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a])
  const ihdr = Buffer.alloc(13)
  ihdr.writeUInt32BE(w, 0)
  ihdr.writeUInt32BE(h, 4)
  ihdr[8] = 8 // 位深
  ihdr[9] = 6 // 色彩类型：RGBA
  const raw = Buffer.alloc((w * 4 + 1) * h)
  for (let y = 0; y < h; y++) {
    raw[y * (w * 4 + 1)] = 0 // 每行 filter=none
    rgba.copy(raw, y * (w * 4 + 1) + 1, y * w * 4, (y + 1) * w * 4)
  }
  return Buffer.concat([
    sig,
    pngChunk('IHDR', ihdr),
    pngChunk('IDAT', zlib.deflateSync(raw)),
    pngChunk('IEND', Buffer.alloc(0)),
  ])
}

/**
 * 渲染未读角标 PNG：text 为 '1'..'99' 或 '99+'。
 * 外形为胶囊（高度 16px，单位数字时为正圆），红底 #f0483e 白字。
 */
function renderBadgePNG(text) {
  const chars = String(text || '').split('').filter((ch) => FONT[ch])
  if (!chars.length) return null
  const h = 16
  const digitW = 5
  const digitH = 7
  const gap = 1
  const textW = chars.length * (digitW + gap) - gap
  const padX = 4
  const w = Math.max(h, textW + padX * 2)
  const r = h / 2 // 胶囊半径

  const px = Buffer.alloc(w * h * 4)
  // 胶囊内判定：中段矩形 + 左右两个半圆
  const inside = (x, y) => {
    const fx = x + 0.5
    const fy = y + 0.5
    if (fx >= r && fx <= w - r) return true
    const cx = fx < r ? r : w - r
    const dx = fx - cx
    const dy = fy - h / 2
    return dx * dx + dy * dy <= r * r
  }
  // 红色底
  for (let y = 0; y < h; y++) {
    for (let x = 0; x < w; x++) {
      if (!inside(x, y)) continue
      const o = (y * w + x) * 4
      px[o] = 0xf0
      px[o + 1] = 0x48
      px[o + 2] = 0x3e
      px[o + 3] = 0xff
    }
  }
  // 白色点阵数字（整体水平居中）
  const x0 = Math.floor((w - textW) / 2)
  const y0 = Math.floor((h - digitH) / 2)
  chars.forEach((ch, ci) => {
    const glyph = FONT[ch]
    const gx = x0 + ci * (digitW + gap)
    for (let row = 0; row < digitH; row++) {
      for (let col = 0; col < digitW; col++) {
        if (glyph[row][col] !== '1') continue
        const o = ((y0 + row) * w + gx + col) * 4
        px[o] = 0xff
        px[o + 1] = 0xff
        px[o + 2] = 0xff
        px[o + 3] = 0xff
      }
    }
  })
  return encodePNG(w, h, px)
}

/**
 * 渲染灰点角标 PNG（L7 托盘灰点）：免打扰会话只有未读红点、不贡献数字时使用。
 * 直径 16px 灰点（#94a3b8），与红色数字角标区分。
 */
function renderDotPNG() {
  const d = 16
  const px = Buffer.alloc(d * d * 4)
  const c = d / 2
  const r = d / 2
  for (let y = 0; y < d; y++) {
    for (let x = 0; x < d; x++) {
      const dx = x + 0.5 - c
      const dy = y + 0.5 - c
      if (dx * dx + dy * dy > r * r) continue
      const o = (y * d + x) * 4
      px[o] = 0x94
      px[o + 1] = 0xa3
      px[o + 2] = 0xb8
      px[o + 3] = 0xff
    }
  }
  return encodePNG(d, d, px)
}

module.exports = { renderBadgePNG, renderDotPNG }
