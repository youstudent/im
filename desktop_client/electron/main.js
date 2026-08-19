const { app, BrowserWindow, ipcMain, Menu, dialog, safeStorage, protocol, Notification, shell, net } = require('electron')
const path = require('node:path')
const fs = require('node:fs')
const crypto = require('node:crypto')
const { Readable } = require('node:stream')
const { pipeline } = require('node:stream/promises')
const { spawn } = require('node:child_process')

// 移除 Electron 默认的应用菜单栏（Windows/Linux 上会显示 File/Edit/View 等菜单条）
Menu.setApplicationMenu(null)

// 开发模式由 npm run dev 通过 cross-env IS_DEV=true 注入
const isDev = process.env.IS_DEV === 'true'

// ---- 本地存储：注册 SQLite 领域 IPC（store:*，含多账户会话管理） ----
require('./store/ipc').register()

// ---- 文件缓存协议：wcfile://{hash}.{ext} → 本地缓存文件 ----
// 必须在 app ready 之前声明 scheme 特权（支持作为 <img> 等资源加载）
const filecache = require('./store/filecache')
protocol.registerSchemesAsPrivileged([
  { scheme: filecache.SCHEME, privileges: { standard: true, secure: true, supportFetchAPI: true } },
])

// ---- IPC 通信示例：主进程暴露能力给渲染进程 ----
ipcMain.handle('app:version', () => app.getVersion())
ipcMain.handle('app:ping', () => 'pong from main process')

// ---- 安全令牌存储：利用系统级 safeStorage 加密 access/refresh token ----
// 返回 { ok: true, value: string } 或 { ok: false }（safeStorage 不可用时降级到明文）。
ipcMain.handle('storage:get-secure', (_event, key) => {
  try {
    if (!safeStorage.isEncryptionAvailable()) return { ok: false }
    const buf = safeStorage.decryptString(Buffer.from(key, 'base64'))
    return { ok: true, value: buf }
  } catch {
    return { ok: false }
  }
})
ipcMain.handle('storage:set-secure', (_event, key) => {
  try {
    if (!safeStorage.isEncryptionAvailable()) return { ok: false }
    const enc = safeStorage.encryptString(key)
    return { ok: true, value: enc.toString('base64') }
  } catch {
    return { ok: false }
  }
})
ipcMain.handle('storage:is-encryption-available', () => {
  try {
    return safeStorage.isEncryptionAvailable()
  } catch {
    return false
  }
})

// ---- 系统原生文件夹选择器：供设置页"本地存储位置"使用 ----
ipcMain.handle('dialog:select-directory', async (event) => {
  const win = BrowserWindow.fromWebContents(event.sender)
  const result = await dialog.showOpenDialog(win, {
    title: '选择本地存储目录',
    // 仅允许选择文件夹
    properties: ['openDirectory', 'createDirectory'],
    buttonLabel: '选择此文件夹',
  })
  // 用户取消时 result.canceled === true；选择时返回首个路径
  if (result.canceled || !result.filePaths.length) {
    return { canceled: true, path: '' }
  }
  return { canceled: false, path: result.filePaths[0] }
})

// ---- 新消息桌面通知：主进程原生 Notification，点击唤起并聚焦窗口 ----
ipcMain.handle('notify:show', (event, arg) => {
  try {
    if (!Notification.isSupported()) return { ok: false }
    const title = (arg && arg.title) || '新消息'
    const body = (arg && arg.body) || ''
    const n = new Notification({ title, body, silent: true }) // 提示音由渲染进程按设置播放
    n.on('click', () => {
      const win = BrowserWindow.fromWebContents(event.sender)
      if (win) {
        if (win.isMinimized()) win.restore()
        win.show()
        win.focus()
      }
    })
    n.show()
    return { ok: true }
  } catch {
    return { ok: false }
  }
})

// ---- 外部链接：用系统默认浏览器打开（仅限 http/https，防注入） ----
ipcMain.handle('shell:open-external', (_event, url) => {
  try {
    const u = String(url || '')
    if (!/^https?:\/\//.test(u)) return { ok: false }
    shell.openExternal(u)
    return { ok: true }
  } catch {
    return { ok: false }
  }
})

// ---- 自动更新：下载安装包 → 退出应用 → 延迟静默运行 NSIS 安装器 ----
// 进度通过 updater:progress 事件回传（{ percent, received, total }）。
ipcMain.handle('updater:download-and-install', async (event, arg) => {
  try {
    const url = String((arg && arg.url) || '')
    if (!/^https?:\/\//.test(url)) return { ok: false, error: '下载地址无效' }
    // 供应链安全（审计 P1）：必须携带 SHA-256 并在下载后校验，防安装包被篡改/投毒后静默执行；
    // 版本信息无摘要时拒绝自动安装（失败即关，用户可手动下载核实）。
    const expectedSha = String((arg && arg.sha256) || '').toLowerCase()
    if (!/^[0-9a-f]{64}$/.test(expectedSha)) return { ok: false, error: '该版本未提供 SHA-256 摘要，无法自动安装，请手动下载' }
    if (process.platform !== 'win32') return { ok: false, error: '当前系统请手动下载安装包安装' }

    const updatesDir = path.join(app.getPath('userData'), 'updates')
    fs.mkdirSync(updatesDir, { recursive: true })
    const dest = path.join(updatesDir, 'workchat-setup-latest.exe')
    const tmp = dest + '.part'

    // 1. 下载安装包（带进度）
    const res = await net.fetch(url)
    if (!res.ok || !res.body) return { ok: false, error: '下载失败：HTTP ' + res.status }
    const total = Number(res.headers.get('content-length')) || 0
    let received = 0
    let lastSent = -1
    const hash = crypto.createHash('sha256') // 边下载边计算摘要，不额外读盘
    const source = Readable.fromWeb(res.body)
    source.on('data', (chunk) => {
      received += chunk.length
      hash.update(chunk)
      if (total) {
        const percent = Math.min(100, Math.floor((received / total) * 100))
        // 每变化 1% 上报一次，避免 IPC 风暴
        if (percent !== lastSent) {
          lastSent = percent
          try {
            event.sender.send('updater:progress', { percent, received, total })
          } catch {}
        }
      }
    })
    await pipeline(source, fs.createWriteStream(tmp))

    // 2. 校验完整性：长度与 Content-Length 一致 + SHA-256 与发布记录一致（防篡改）
    const size = fs.statSync(tmp).size
    if (!size || (total && size !== total)) {
      fs.rmSync(tmp, { force: true })
      return { ok: false, error: '下载不完整，请重试' }
    }
    const actualSha = hash.digest('hex')
    if (actualSha !== expectedSha) {
      fs.rmSync(tmp, { force: true })
      return { ok: false, error: '安装包摘要校验失败，已中止安装（文件可能被篡改）' }
    }
    fs.rmSync(dest, { force: true })
    fs.renameSync(tmp, dest)

    // 3. 生成临时 .bat 延迟静默运行安装器，随后退出应用。
    // 路径一律经环境变量传入（env 块 Unicode 传输）：bat 以 UTF-8 写入而 cmd 默认按 ANSI
    // 解析，直接在 bat 里写含中文的路径（如 安装目录/检车家IM）会乱码导致 /D 错误、安装失败。
    // 用户可自定义安装目录（nsis oneClick=false）：静默安装必须显式指定原安装目录（/D），
    // 否则会装到默认位置。/D 为 NSIS 特殊参数：不能带引号且必须放最后。
    // 渲染进程传不到路径时，用主进程自身的可执行文件路径（打包后同位于安装目录）。
    const execPath = String((arg && arg.installDir) || '') || process.execPath
    const installDir = execPath ? path.dirname(execPath) : ''
    const bat = path.join(updatesDir, 'install-update.bat')
    const batContent = [
      '@echo off',
      'setlocal enabledelayedexpansion',
      'set "LOG=%TEMP%\\workchat-update.log"',
      'echo [%date% %time%] updater start installer="%WC_INSTALLER%" dir="%WC_DIR%" exe="%WC_EXE%" >> "%LOG%" 2>&1',
      'ping -n 6 127.0.0.1 >nul', // 等 ~5 秒，让本应用完全退出，避免文件占用
      'call "%WC_INSTALLER%" /S /D=%WC_DIR%', // call：兼容 exe/bat，保证执行完回到本脚本继续
      'set RC=%ERRORLEVEL%',
      'echo [%date% %time%] silent install rc=%RC% >> "%LOG%" 2>&1',
      'if not "%RC%"=="0" (',
      // exe 已被旧版卸载流程移除（罕见）：无需提权，直接交互安装
      '  if not exist "%WC_EXE%" goto interactive',
      // 静默安装失败（常见：目录需管理员权限）：提权重试，会弹一次 UAC 确认。
      // PowerShell 内用 exit $p.ExitCode 回传安装器退出码；用户拒绝 UAC 时返回非 0。
      '  echo [%date% %time%] retry with elevation >> "%LOG%" 2>&1',
      "  powershell -NoProfile -Command \"$p = Start-Process -FilePath $env:WC_INSTALLER -ArgumentList ('/S /D='+$env:WC_DIR) -Verb RunAs -Wait -PassThru; exit $p.ExitCode\" >> \"%LOG%\" 2>&1",
      '  set RC2=!ERRORLEVEL!',
      '  echo [%date% %time%] elevated install rc=!RC2! >> "%LOG%" 2>&1',
      '  if not "!RC2!"=="0" goto interactive',
      '  goto launchapp',
      ')',
      'goto launchapp', // 静默安装成功：跳过交互兜底（标签在块外，防止落入）
      // 仍未成功（rc=2 常见于安装包自身完整性校验失败，如发布包损坏）：
      // 降级弹出官方安装向导，用户点几下即可完成安装，避免静默失败后毫无反馈。
      ':interactive',
      'echo [%date% %time%] fallback to interactive installer >> "%LOG%" 2>&1',
      'call "%WC_INSTALLER%" /D=%WC_DIR%',
      'echo [%date% %time%] interactive install rc=%ERRORLEVEL% >> "%LOG%" 2>&1',
      ':launchapp',
      // 安装完成自动拉起新版本（安装失败时 exe 仍在原位，同样能恢复应用）
      'if exist "%WC_EXE%" (',
      '  start "" "%WC_EXE%"',
      ') else (',
      '  echo [%date% %time%] exe missing: %WC_EXE% >> "%LOG%" 2>&1',
      ')',
      'echo [%date% %time%] updater done >> "%LOG%" 2>&1',
      'endlocal',
      'del "%~f0"', // 最后自删除
    ].join('\r\n') + '\r\n'
    fs.writeFileSync(bat, batContent, 'utf8')
    // Windows 上 spawn .bat 必须带 shell:true（否则同步抛 EINVAL，安装器永远不会运行）；
    // detached + windowsHide + unref 保证安装脚本在本应用退出后继续静默执行。
    const child = spawn(bat, {
      shell: true,
      detached: true,
      stdio: 'ignore',
      windowsHide: true,
      // 中文路径安全：经环境变量传入，bat 内容保持纯 ASCII
      env: { ...process.env, WC_INSTALLER: dest, WC_DIR: installDir, WC_EXE: execPath },
    })
    child.on('error', (e) => console.warn('[updater] 启动安装脚本失败:', e?.message || e))
    child.unref()
    console.log('[updater] 安装脚本已启动，日志: %TEMP%\\workchat-update.log')
    setTimeout(() => app.quit(), 300)
    return { ok: true, path: dest }
  } catch (e) {
    return { ok: false, error: e?.message || String(e) }
  }
})

function createWindow() {
  const mainWindow = new BrowserWindow({
    width: 1280,
    height: 820,
    minWidth: 960,
    minHeight: 640,
    title: 'WorkChat',
    // 使用系统原生标题栏（不启用自定义标题栏/导航栏）
    frame: true,
    titleBarStyle: 'default',
    show: false,
    webPreferences: {
      // 预加载脚本始终以独立上下文运行，安全桥接 Node 能力与渲染进程
      preload: path.join(__dirname, 'preload.js'),
      contextIsolation: true,
      nodeIntegration: false,
      // 审计 M4：渲染进程沙箱化，预加载脚本仅可用受限 Electron API（本项目 preload 只用
      // contextBridge/ipcRenderer，满足沙箱要求），即使未来出现 XSS 也难以触及 Node 能力
      sandbox: true,
    },
  })

  // 审计 M4（官方安全检查清单）：阻断一切新窗口打开，外部链接一律走 shell:open-external
  // （主进程校验 http/https 后用系统浏览器打开），防恶意内容自行弹出 Electron 窗口
  mainWindow.webContents.setWindowOpenHandler(() => ({ action: 'deny' }))
  // 阻断页面内导航：仅允许应用自身页面（生产 file:// / 开发 Vite 地址），
  // 防止潜在 XSS 把主窗口导航到钓鱼页面或本地敏感文件
  mainWindow.webContents.on('will-navigate', (event, url) => {
    if (isDev && url.startsWith('http://localhost:5173')) return
    if (!isDev && url.startsWith('file://')) return
    event.preventDefault()
  })

  mainWindow.once('ready-to-show', () => {
    mainWindow.show()
    // 开发模式默认打开 DevTools 调试控制台（独立窗口，避免干扰主界面布局）。
    // 可通过 OPEN_DEVTOOLS=0 关闭，按 F12 / Ctrl+Shift+I 随时开关。
    if (isDev && process.env.OPEN_DEVTOOLS !== '0') {
      mainWindow.webContents.openDevTools({ mode: 'detach' })
    }
  })

  if (isDev) {
    // 开发模式：加载 Vite 开发服务器，享受 HMR 热更新
    mainWindow.loadURL('http://localhost:5173')
  } else {
    // 生产模式：加载构建后的静态文件
    mainWindow.loadFile(path.join(__dirname, '../dist/index.html'))
  }
}

app.whenReady().then(() => {
  // 重试删除上次存储路径迁移时因文件占用留下的残留目录
  try {
    require('./store/db').cleanupPendingDeletes()
  } catch {}

  // 注册文件缓存协议处理器（服务 wcfile:// 资源）
  protocol.handle(filecache.SCHEME, filecache.createProtocolHandler())

  createWindow()

  app.on('activate', () => {
    if (BrowserWindow.getAllWindows().length === 0) createWindow()
  })
})

app.on('window-all-closed', () => {
  if (process.platform !== 'darwin') app.quit()
})

// 退出前关闭当前账户的本地库句柄
app.on('will-quit', () => {
  try {
    require('./store/db').closeSession()
  } catch {}
})
