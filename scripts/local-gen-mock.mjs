#!/usr/bin/env node
/**
 * 本地上游 mock，协议对齐：
 * - gpt-image-2（OpenAI 兼容）:
 *     POST /v1/images/generations | /v1/images/edits
 *     POST /v1/chat/completions
 * - Seedance 2.0（API易 / apiyi_seedance2）:
 *     POST /seedance/api/v3/contents/generations/tasks
 *     GET  /seedance/api/v3/contents/generations/tasks/:id
 * - 兼容保留 Echoon: /api/v1/generate/ …
 *
 * 用法: node scripts/local-gen-mock.mjs [--port 8790]
 */
import { createServer } from 'node:http'
import { spawnSync } from 'node:child_process'
import { existsSync, mkdirSync, readFileSync, statSync, writeFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { randomUUID } from 'node:crypto'

const __dirname = dirname(fileURLToPath(import.meta.url))
const ROOT = join(__dirname, '..')
const DATA_DIR = join(ROOT, 'data', 'local-gen-mock')
const VIDEO_PATH = join(DATA_DIR, 'clip-15s.mp4')
const PORT = Number(process.argv.includes('--port') ? process.argv[process.argv.indexOf('--port') + 1] : process.env.LOCAL_GEN_MOCK_PORT || 8790)

const SEEDANCE_FAST = 'doubao-seedance-2-0-fast-260128'
const SEEDANCE_STD = 'doubao-seedance-2-0-260128'
const ECHOON_MODEL_ID = '00000000-0000-4000-8000-000000000001'

/** 1x1 PNG */
const TINY_PNG_B64 =
  'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=='

const tasks = new Map()

function ensureDir() {
  mkdirSync(DATA_DIR, { recursive: true })
}

function ensureVideo() {
  ensureDir()
  if (existsSync(VIDEO_PATH) && statSync(VIDEO_PATH).size > 1024) return
  console.log('[local-gen-mock] generating 15s placeholder mp4 via ffmpeg…')
  const result = spawnSync(
    'ffmpeg',
    [
      '-hide_banner', '-loglevel', 'error', '-y',
      '-f', 'lavfi', '-i', 'testsrc2=size=720x1280:rate=30',
      '-f', 'lavfi', '-i', 'sine=frequency=440:sample_rate=48000',
      '-t', '15',
      '-c:v', 'libx264', '-preset', 'ultrafast', '-pix_fmt', 'yuv420p',
      '-c:a', 'aac', '-ar', '48000', '-ac', '2',
      '-movflags', '+faststart',
      VIDEO_PATH,
    ],
    { encoding: 'utf8' },
  )
  if (result.status !== 0) {
    throw new Error(`ffmpeg failed: ${result.stderr || result.status}`)
  }
  console.log(`[local-gen-mock] wrote ${VIDEO_PATH} (${statSync(VIDEO_PATH).size} bytes)`)
}

function storyboardJSON() {
  const scenes = [1, 2, 3, 4].map((index) => ({
    index,
    title: `场景 ${String(index).padStart(2, '0')}`,
    duration_sec: 15,
    camera: '中景推进',
    action: `角色在场景 ${index} 中互动`,
    expression: '平静',
    lighting: '柔光',
    dialogue: `这是第 ${index} 场台词。`,
    sound: '环境音',
    prompt: `古风实景，第 ${index} 场，电影感构图`,
  }))
  return JSON.stringify({ title: '本地 mock 分镜', scenes })
}

async function readJSON(req) {
  const chunks = []
  for await (const chunk of req) chunks.push(chunk)
  if (!chunks.length) return {}
  try {
    return JSON.parse(Buffer.concat(chunks).toString('utf8'))
  } catch {
    return {}
  }
}

function sendJSON(res, status, body) {
  const raw = Buffer.from(JSON.stringify(body))
  res.writeHead(status, {
    'Content-Type': 'application/json; charset=utf-8',
    'Content-Length': raw.length,
    'Access-Control-Allow-Origin': '*',
  })
  res.end(raw)
}

function sendSSEChat(res, content) {
  res.writeHead(200, {
    'Content-Type': 'text/event-stream; charset=utf-8',
    'Cache-Control': 'no-cache',
    Connection: 'keep-alive',
    'Access-Control-Allow-Origin': '*',
  })
  const chunk = {
    id: `chatcmpl-${randomUUID()}`,
    object: 'chat.completion.chunk',
    choices: [{ index: 0, delta: { role: 'assistant', content }, finish_reason: null }],
  }
  res.write(`data: ${JSON.stringify(chunk)}\n\n`)
  const done = {
    id: chunk.id,
    object: 'chat.completion.chunk',
    choices: [{ index: 0, delta: {}, finish_reason: 'stop' }],
  }
  res.write(`data: ${JSON.stringify(done)}\n\n`)
  res.write('data: [DONE]\n\n')
  res.end()
}

function publicBase(req) {
  const host = req.headers.host || `127.0.0.1:${PORT}`
  return `http://${host}`
}

function createSeedanceTask(req, body = {}) {
  const id = `cgt-${randomUUID().replace(/-/g, '').slice(0, 16)}`
  const model = body.model || SEEDANCE_FAST
  const resultURL = `${publicBase(req)}/clip-15s.mp4`
  tasks.set(id, {
    kind: 'seedance',
    id,
    model,
    status: 'succeeded',
    content: { video_url: resultURL },
    usage: { completion_tokens: 108900 },
    created_at: Date.now(),
    prompt: Array.isArray(body.content)
      ? (body.content.find((c) => c?.type === 'text')?.text || '')
      : '',
  })
  console.log(`[local-gen-mock] seedance ${id} model=${model} → ${resultURL}`)
  return id
}

ensureVideo()

const server = createServer(async (req, res) => {
  const url = new URL(req.url || '/', `http://${req.headers.host || '127.0.0.1'}`)
  const path = url.pathname.replace(/\/+$/, '') || '/'

  if (req.method === 'OPTIONS') {
    res.writeHead(204, {
      'Access-Control-Allow-Origin': '*',
      'Access-Control-Allow-Methods': 'GET,POST,OPTIONS',
      'Access-Control-Allow-Headers': 'Authorization,Content-Type,Accept-Encoding',
    })
    res.end()
    return
  }

  if (req.method === 'GET' && path === '/healthz') {
    sendJSON(res, 200, {
      status: 'ok',
      video: existsSync(VIDEO_PATH),
      protocols: ['gpt-image-2', 'seedance-2.0', 'echoon'],
    })
    return
  }

  if (req.method === 'GET' && (path === '/clip-15s.mp4' || path === '/media/clip-15s.mp4')) {
    const buf = readFileSync(VIDEO_PATH)
    res.writeHead(200, {
      'Content-Type': 'video/mp4',
      'Content-Length': buf.length,
      'Accept-Ranges': 'bytes',
      'Access-Control-Allow-Origin': '*',
    })
    res.end(buf)
    return
  }

  // ---- gpt-image-2 / OpenAI text ----
  if (req.method === 'POST' && (path === '/v1/chat/completions' || path === '/chat/completions')) {
    const body = await readJSON(req)
    const content = storyboardJSON()
    if (body.stream) {
      sendSSEChat(res, content)
      return
    }
    sendJSON(res, 200, {
      id: `chatcmpl-${randomUUID()}`,
      object: 'chat.completion',
      model: body.model || 'gpt-5.4',
      choices: [{
        index: 0,
        message: { role: 'assistant', content },
        finish_reason: 'stop',
      }],
    })
    return
  }

  if (
    req.method === 'POST'
    && (path === '/v1/images/generations' || path === '/images/generations'
      || path === '/v1/images/edits' || path === '/images/edits')
  ) {
    const body = await readJSON(req)
    const n = Math.max(1, Number(body.n) || 1)
    const model = body.model || 'gpt-image-2'
    const data = Array.from({ length: Math.min(n, 4) }, () => ({
      b64_json: TINY_PNG_B64,
      revised_prompt: body.prompt || '',
    }))
    console.log(`[local-gen-mock] gpt-image-2 ${path} model=${model} n=${data.length} size=${body.size || '-'}`)
    sendJSON(res, 200, { created: Math.floor(Date.now() / 1000), data })
    return
  }

  if (req.method === 'GET' && (path === '/v1/models' || path === '/models')) {
    sendJSON(res, 200, {
      object: 'list',
      data: [
        { id: 'gpt-image-2', object: 'model', owned_by: 'local-mock' },
        { id: 'gpt-5.4', object: 'model', owned_by: 'local-mock' },
        { id: SEEDANCE_FAST, object: 'model', owned_by: 'local-mock' },
        { id: SEEDANCE_STD, object: 'model', owned_by: 'local-mock' },
      ],
    })
    return
  }

  // ---- Seedance 2.0 (API易) ----
  // POST /seedance/api/v3/contents/generations/tasks
  if (req.method === 'POST' && path === '/seedance/api/v3/contents/generations/tasks') {
    const body = await readJSON(req)
    const id = createSeedanceTask(req, body)
    sendJSON(res, 200, { id })
    return
  }

  // GET /seedance/api/v3/contents/generations/tasks/:id
  const seedanceMatch = path.match(/^\/seedance\/api\/v3\/contents\/generations\/tasks\/([^/]+)$/)
  if (req.method === 'GET' && seedanceMatch) {
    const task = tasks.get(seedanceMatch[1])
    if (!task || task.kind !== 'seedance') {
      sendJSON(res, 404, { error: { message: 'task not found', type: 'not_found' } })
      return
    }
    sendJSON(res, 200, {
      id: task.id,
      model: task.model,
      status: task.status,
      content: task.content,
      usage: task.usage,
    })
    return
  }

  // ---- Echoon（兼容）----
  if (req.method === 'GET' && (path === '/api/v1/models' || path === '/models/')) {
    sendJSON(res, 200, [
      { id: ECHOON_MODEL_ID, name: 'local-mock-15s', type: 'video' },
      { id: SEEDANCE_FAST, name: 'Seedance 2.0 Fast (mock)', type: 'video' },
      { id: SEEDANCE_STD, name: 'Seedance 2.0 (mock)', type: 'video' },
    ])
    return
  }

  if (req.method === 'POST' && (path === '/api/v1/generate' || path === '/generate')) {
    const body = await readJSON(req)
    const id = randomUUID()
    const resultURL = `${publicBase(req)}/clip-15s.mp4`
    tasks.set(id, {
      kind: 'echoon',
      id,
      model_id: body.model_id || ECHOON_MODEL_ID,
      status: 'completed',
      progress: 100,
      prompt: body.prompt || '',
      result_url: resultURL,
      created_at: Date.now(),
    })
    sendJSON(res, 200, { task_id: id, id, status: 'queued', progress: 0 })
    return
  }

  const echoonMatch = path.match(/^\/(?:api\/v1\/)?generate\/tasks\/([^/]+)$/)
  if (req.method === 'GET' && echoonMatch) {
    const task = tasks.get(echoonMatch[1])
    if (!task) {
      sendJSON(res, 404, { error: 'task not found' })
      return
    }
    sendJSON(res, 200, task)
    return
  }

  sendJSON(res, 404, { error: `no route ${req.method} ${path}` })
})

server.listen(PORT, '127.0.0.1', () => {
  writeFileSync(join(DATA_DIR, 'ready.json'), JSON.stringify({
    port: PORT,
    video: VIDEO_PATH,
    image_base: `http://127.0.0.1:${PORT}/v1`,
    seedance_base: `http://127.0.0.1:${PORT}`,
    seedance_model: SEEDANCE_FAST,
  }, null, 2))
  console.log(`[local-gen-mock] listening http://127.0.0.1:${PORT}`)
  console.log(`[local-gen-mock] gpt-image-2:  POST http://127.0.0.1:${PORT}/v1/images/generations`)
  console.log(`[local-gen-mock] seedance2.0:  POST http://127.0.0.1:${PORT}/seedance/api/v3/contents/generations/tasks`)
})
