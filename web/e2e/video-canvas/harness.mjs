import { spawn } from 'node:child_process'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { startVideoCanvasMockServer } from './mock-server.mjs'

const currentDirectory = dirname(fileURLToPath(import.meta.url))
const webRoot = resolve(currentDirectory, '../..')
const host = process.env.VIDEO_CANVAS_WEB_HOST || '127.0.0.1'
const webPort = Number(process.env.VIDEO_CANVAS_WEB_PORT || 4173)
const mockHost = process.env.VIDEO_CANVAS_MOCK_HOST || '127.0.0.1'
const mockPort = Number(process.env.VIDEO_CANVAS_MOCK_PORT || 18080)

async function waitFor(url, attempts = 80) {
  for (let attempt = 0; attempt < attempts; attempt += 1) {
    try {
      const response = await fetch(url)
      if (response.ok) return
    } catch {
      // Vite 尚未监听。
    }
    await new Promise((resolvePromise) => setTimeout(resolvePromise, 150))
  }
  throw new Error(`前端未在预期时间内就绪：${url}`)
}

const mock = await startVideoCanvasMockServer({ host: mockHost, port: mockPort })
const viteEntry = resolve(webRoot, 'node_modules/vite/bin/vite.js')
const vite = spawn(process.execPath, [viteEntry, '--host', host, '--port', String(webPort), '--strictPort'], {
  cwd: webRoot,
  env: { ...process.env, VITE_API_BASE: mock.url },
  stdio: 'inherit',
})

let shuttingDown = false
async function shutdown(exitCode = 0) {
  if (shuttingDown) return
  shuttingDown = true
  if (!vite.killed) vite.kill('SIGTERM')
  await mock.close().catch((error) => console.error('[video-canvas-e2e] mock 关闭失败', error))
  process.exit(exitCode)
}

vite.once('exit', (code, signal) => {
  if (!shuttingDown) {
    console.error(`[video-canvas-e2e] Vite 已退出：code=${code ?? 'null'} signal=${signal ?? 'null'}`)
    void shutdown(code || 1)
  }
})
process.once('SIGINT', () => { void shutdown(0) })
process.once('SIGTERM', () => { void shutdown(0) })

try {
  const loginURL = `http://${host}:${webPort}/login?redirect=%2Fpersonal%2Fvideo-workflows`
  await waitFor(loginURL)
  console.log('\n[video-canvas-e2e] 可复现浏览器环境已就绪')
  console.log(`[video-canvas-e2e] 页面: ${loginURL}`)
  console.log('[video-canvas-e2e] 登录: demo@lingjing.test / video-canvas')
  console.log(`[video-canvas-e2e] Mock API: ${mock.url}`)
  console.log('[video-canvas-e2e] Ctrl+C 同时停止前端与 Mock API\n')
} catch (error) {
  console.error(error)
  await shutdown(1)
}
