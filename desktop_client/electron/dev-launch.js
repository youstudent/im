/**
 * Electron 开发启动器
 *
 * 背景：当前环境注入的 NODE_OPTIONS（含 --use-system-ca）与
 * ELECTRON_RUN_AS_NODE=1 会导致 Electron 启动失败：
 *  - --use-system-ca 被 Electron 拒绝（退出码 9）
 *  - ELECTRON_RUN_AS_NODE=1 让 electron.exe 以纯 Node 模式运行，
 *    此时 require('electron') 返回路径字符串，ipcMain/app 均为 undefined
 *
 * cross-env 只能将变量"置空"，而 Electron 对 ELECTRON_RUN_AS_NODE
 * 是"变量存在即生效"，置空无效，必须彻底删除。因此这里用 Node 删除
 * 这两个环境变量后再以子进程方式启动 electron.exe。
 */

const { spawn } = require('child_process')
const path = require('path')

// 彻底删除（delete 而非置空）
delete process.env.ELECTRON_RUN_AS_NODE
delete process.env.NODE_OPTIONS

// require('electron') 在普通 Node 下返回 electron.exe 的路径字符串
const electronPath = require('electron')

const child = spawn(electronPath, ['--disable-gpu', '--no-sandbox', '.'], {
  cwd: process.cwd(),
  stdio: 'inherit',
  env: process.env,
})

child.on('exit', (code) => process.exit(code ?? 0))
