/** 轻量 CDP 客户端：连接 Edge/Chrome remote-debugging，驱动页面。 */
import { spawn } from 'child_process'
import fs from 'fs'
import os from 'os'
import path from 'path'

const EDGE = 'C:\\Program Files (x86)\\Microsoft\\Edge\\Application\\msedge.exe'

let msgIdCounter = 0
const pending = new Map()

// 启动 Edge headless，返回 ws url 和 devtools ws
export async function launchEdge({ port, userDataDir, url }) {
  const args = [
    '--headless=new',
    '--disable-gpu',
    '--no-sandbox',
    '--remote-debugging-port=' + port,
    '--user-data-dir=' + userDataDir,
    '--window-size=1400,900',
    '--autoplay-policy=no-user-gesture-required',
    url,
  ]
  const child = spawn(EDGE, args, { stdio: 'ignore' })
  // 等待调试端口就绪
  let targets
  for (let i = 0; i < 30; i++) {
    await sleep(500)
    try {
      const res = await fetch(`http://127.0.0.1:${port}/json/list`)
      targets = await res.json()
      if (targets && targets.length) break
    } catch {}
  }
  if (!targets || !targets.length) throw new Error('Edge devtools not ready on port ' + port)
  return { child, targets }
}

export function sleep(ms) { return new Promise((r) => setTimeout(r, ms)) }

export function makeClient() {
  let ws
  const callbacks = new Map()
  function connect(url) {
    return new Promise((resolve, reject) => {
      ws = new WebSocket(url)
      ws.onopen = () => resolve()
      ws.onerror = (e) => reject(new Error('cdp ws error'))
      ws.onmessage = (e) => {
        const m = JSON.parse(e.data)
        if (m.id && callbacks.has(m.id)) {
          callbacks.get(m.id)(m)
          callbacks.delete(m.id)
        }
      }
    })
  }
  function send(method, params = {}) {
    return new Promise((resolve) => {
      const id = ++msgIdCounter
      callbacks.set(id, resolve)
      ws.send(JSON.stringify({ id, method, params }))
    })
  }
  return { connect, send, get ws() { return ws } }
}

// 在页面执行 JS，返回求值结果
export async function evaluate(client, target, expression) {
  const res = await client.send('Runtime.evaluate', {
    expression,
    awaitPromise: true,
    returnByValue: true,
  })
  if (res.error) throw new Error('CDP error: ' + JSON.stringify(res.error))
  const r = res.result && res.result.result
  if (r && r.exceptionDetails) {
    throw new Error('page JS error: ' + JSON.stringify(r.exceptionDetails.exception && r.exceptionDetails.exception.description || r.exceptionDetails.text))
  }
  return r && r.value !== undefined ? r.value : (r ? r.description || r.value : res)
}
