import { createServer } from 'node:http'
import { mkdtempSync, readFileSync, rmSync, statSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join, resolve } from 'node:path'
import { spawnSync } from 'node:child_process'
import { pathToFileURL } from 'node:url'
import { randomUUID } from 'node:crypto'
import { cloneFixture, createFixtureState } from './fixtures.mjs'

const DEMO_EMAIL = 'demo@lingjing.test'
const DEMO_PASSWORD = 'video-canvas'
const ACCESS_TOKEN = 'mock-video-canvas-access'
const REFRESH_TOKEN = 'mock-video-canvas-refresh'

const USER = {
  id: 1001,
  email: DEMO_EMAIL,
  nickname: '画布验收员',
  role: 'user',
  status: 'active',
  group_id: 1,
  credit_balance: 20000,
  credit_frozen: 0,
  created_at: '2026-07-11T08:00:00Z',
}
const PERMISSIONS = ['self:profile', 'self:video_workflow']

function generateVideo(target) {
  const result = spawnSync('ffmpeg', [
    '-hide_banner', '-loglevel', 'error', '-y',
    '-f', 'lavfi', '-i', 'testsrc2=size=360x640:rate=30',
    '-f', 'lavfi', '-i', 'sine=frequency=440:sample_rate=48000',
    '-t', '2', '-c:v', 'libx264', '-preset', 'veryfast', '-pix_fmt', 'yuv420p',
    '-c:a', 'aac', '-ar', '48000', '-ac', '2', '-movflags', '+faststart', target,
  ], { encoding: 'utf8' })
  if (result.status !== 0) {
    throw new Error(`ffmpeg 无法生成 E2E 视频：${result.stderr || `exit ${result.status}`}`)
  }
  if (statSync(target).size < 1024) throw new Error('ffmpeg 生成的视频为空')
}

function setCORS(req, res) {
  const origin = req.headers.origin || '*'
  res.setHeader('Access-Control-Allow-Origin', origin)
  res.setHeader('Access-Control-Allow-Credentials', 'true')
  res.setHeader('Access-Control-Allow-Methods', 'GET,HEAD,POST,PUT,DELETE,OPTIONS')
  res.setHeader('Access-Control-Allow-Headers', 'Authorization,Content-Type,Range,If-Range')
  res.setHeader('Access-Control-Expose-Headers', 'Accept-Ranges,Content-Length,Content-Range,ETag,Last-Modified')
  res.setHeader('Vary', 'Origin')
}

function sendJSON(res, status, data, message = status < 400 ? 'ok' : 'request failed', code = status < 400 ? 0 : status) {
  const payload = Buffer.from(JSON.stringify({ code, message, data }))
  res.writeHead(status, {
    'Content-Type': 'application/json; charset=utf-8',
    'Content-Length': payload.length,
    'Cache-Control': 'no-store',
  })
  res.end(payload)
}

async function readBody(req, limit = 20 * 1024 * 1024) {
  const chunks = []
  let size = 0
  for await (const chunk of req) {
    size += chunk.length
    if (size > limit) throw Object.assign(new Error('request body too large'), { status: 413 })
    chunks.push(chunk)
  }
  return Buffer.concat(chunks)
}

async function readJSON(req) {
  const body = await readBody(req)
  if (!body.length) return {}
  try {
    return JSON.parse(body.toString('utf8'))
  } catch {
    throw Object.assign(new Error('invalid json'), { status: 400 })
  }
}

function multipartField(body, name) {
  const source = body.toString('latin1')
  const pattern = new RegExp(`name="${name}"(?:;[^\\r\\n]*)?\\r\\n(?:[^\\r\\n]*\\r\\n)*\\r\\n([\\s\\S]*?)\\r\\n--`)
  const match = source.match(pattern)
  if (!match) return ''
  return Buffer.from(match[1], 'latin1').toString('utf8').trim()
}

function authenticated(req) {
  return req.headers.authorization === `Bearer ${ACCESS_TOKEN}`
}

function requireAuth(req, res) {
  if (authenticated(req)) return true
  sendJSON(res, 401, null, 'unauthorized')
  return false
}

function assetURL(baseURL, versionID, token = 'fixture-preview') {
  return `${baseURL}/p/vwf/${encodeURIComponent(versionID)}?token=${token}`
}

function svgForVersion(versionID) {
  const sceneMatch = versionID.match(/s0?([1-4])/i)
  const scene = sceneMatch ? Number(sceneMatch[1]) : 2
  const palettes = [
    ['#172554', '#2563eb', '#f8fafc'],
    ['#111827', '#0ea5e9', '#f59e0b'],
    ['#292524', '#64748b', '#f8fafc'],
    ['#431407', '#dc2626', '#fde68a'],
  ]
  const [dark, accent, light] = palettes[scene - 1]
  const label = `S0${scene} · ${scene === 2 && versionID.includes('v2') ? '雨巷对峙 · V2 已修改' : '场景图片'}`
  return `<?xml version="1.0" encoding="UTF-8"?>
<svg xmlns="http://www.w3.org/2000/svg" width="1080" height="1920" viewBox="0 0 1080 1920">
  <defs>
    <linearGradient id="night" x1="0" y1="0" x2="1" y2="1"><stop stop-color="${dark}"/><stop offset="1" stop-color="#070b12"/></linearGradient>
    <radialGradient id="lamp"><stop stop-color="${light}" stop-opacity=".92"/><stop offset="1" stop-color="${accent}" stop-opacity="0"/></radialGradient>
    <pattern id="rain" width="36" height="72" patternUnits="userSpaceOnUse" patternTransform="rotate(18)"><path d="M2 0v50" stroke="#bfdbfe" stroke-opacity=".24" stroke-width="3"/></pattern>
  </defs>
  <rect width="1080" height="1920" fill="url(#night)"/><rect width="1080" height="1920" fill="url(#rain)"/>
  <circle cx="790" cy="520" r="430" fill="url(#lamp)" opacity=".55"/>
  <path d="M0 1210L260 1040l180 80 190-210 450 250v760H0z" fill="#0f172a" opacity=".86"/>
  <path d="M390 1300c45-190 145-285 220-285s174 95 220 285v420H390z" fill="#05070a"/>
  <path d="M460 1080c35-78 90-120 150-120s116 42 150 120c-58-26-105-35-150-35s-92 9-150 35z" fill="${accent}" opacity=".72"/>
  <rect x="58" y="70" width="964" height="1780" rx="32" fill="none" stroke="#e2e8f0" stroke-opacity=".2" stroke-width="3"/>
  <text x="84" y="1620" fill="#f8fafc" font-size="54" font-family="PingFang SC, Microsoft YaHei, sans-serif" font-weight="700">${label}</text>
  <text x="84" y="1698" fill="#cbd5e1" font-size="30" font-family="PingFang SC, Microsoft YaHei, sans-serif">雨夜重逢 · 9:16 视觉预览</text>
</svg>`
}

function serveImage(req, res, versionID) {
  const payload = Buffer.from(svgForVersion(versionID))
  const headers = {
    'Content-Type': 'image/svg+xml; charset=utf-8',
    'Content-Length': payload.length,
    'Cache-Control': 'private, max-age=60',
    ETag: `"${versionID}"`,
  }
  res.writeHead(200, headers)
  if (req.method === 'HEAD') res.end()
  else res.end(payload)
}

function parseRange(value, size) {
  const match = /^bytes=(\d*)-(\d*)$/.exec(String(value || '').trim())
  if (!match) return null
  if (!match[1] && !match[2]) return null
  let start
  let end
  if (!match[1]) {
    const suffix = Number(match[2])
    if (!Number.isFinite(suffix) || suffix <= 0) return null
    start = Math.max(0, size - suffix)
    end = size - 1
  } else {
    start = Number(match[1])
    end = match[2] ? Number(match[2]) : size - 1
  }
  if (!Number.isInteger(start) || !Number.isInteger(end) || start < 0 || start >= size || end < start) return null
  return { start, end: Math.min(end, size - 1) }
}

function serveVideo(req, res, mediaPath, purpose) {
  const payload = readFileSync(mediaPath)
  const size = payload.length
  const etag = '"video-canvas-mock-v1"'
  const common = {
    'Content-Type': 'video/mp4',
    'Accept-Ranges': 'bytes',
    ETag: etag,
    'Last-Modified': 'Sat, 11 Jul 2026 00:00:00 GMT',
    'Cache-Control': 'private, max-age=60',
    ...(purpose === 'download' ? { 'Content-Disposition': 'attachment; filename="rain-reunion-mock.mp4"' } : {}),
  }
  const requestedRange = req.headers.range
  const ifRange = req.headers['if-range']
  const useRange = requestedRange && (!ifRange || ifRange === etag)
  if (useRange) {
    const range = parseRange(requestedRange, size)
    if (!range) {
      res.writeHead(416, { ...common, 'Content-Range': `bytes */${size}`, 'Content-Length': 0 })
      return res.end()
    }
    const chunk = payload.subarray(range.start, range.end + 1)
    res.writeHead(206, {
      ...common,
      'Content-Range': `bytes ${range.start}-${range.end}/${size}`,
      'Content-Length': chunk.length,
    })
    return req.method === 'HEAD' ? res.end() : res.end(chunk)
  }
  res.writeHead(200, { ...common, 'Content-Length': size })
  return req.method === 'HEAD' ? res.end() : res.end(payload)
}

function exposeRun(run) {
  const result = cloneFixture(run)
  delete result._poll_count
  delete result._canceled
  return result
}

function ensureFinalAsset(state, baseURL) {
  if (state.assets.some((asset) => asset.id === 'asset-final')) return
  state.assets.push({
    id: 'asset-final', name: '雨夜重逢 · 最终成片', kind: 'video', status: 'ready', current_version_id: 'video-final-v1', current_version: 1,
    preview_url: assetURL(baseURL, 'video-final-v1'), versions: [{
      id: 'video-final-v1', version: 1, asset_id: 'asset-final', status: 'ready', mime: 'video/mp4', size_bytes: 1,
      width: 1080, height: 1920, duration_ms: 2000, source: 'generated', preview_url: assetURL(baseURL, 'video-final-v1'),
    }],
  })
}

function advanceRun(state, run, baseURL) {
  if (!['queued', 'running'].includes(run.status)) return
  run._poll_count += 1
  if (run._poll_count === 1) {
    run.status = 'running'
    run.progress = 38
    run.started_at = new Date().toISOString()
    run.node_runs.forEach((nodeRun, index) => {
      nodeRun.status = index < 7 ? 'succeeded' : 'running'
      nodeRun.progress = nodeRun.status === 'succeeded' ? 100 : 35
    })
    return
  }
  if (run._poll_count === 2) {
    run.progress = 76
    run.node_runs.forEach((nodeRun, index) => {
      nodeRun.status = index < 15 ? 'succeeded' : 'running'
      nodeRun.progress = nodeRun.status === 'succeeded' ? 100 : 70
    })
    return
  }
  run.status = 'succeeded'
  run.progress = 100
  run.actual_credits = 1280
  run.output_version_id = 'video-final-v1'
  run.output_asset_version_id = 'video-final-v1'
  run.output = {
    id: 'video-final-v1', version: 1, asset_id: 'asset-final', status: 'ready', mime: 'video/mp4', size_bytes: statSync(state.mediaPath).size,
    width: 1080, height: 1920, duration_ms: 2000, source: 'generated', preview_url: assetURL(baseURL, 'video-final-v1'),
  }
  run.node_runs.forEach((nodeRun) => {
    nodeRun.status = 'succeeded'
    nodeRun.progress = 100
    if (nodeRun.node_id === 'compose') {
      nodeRun.output_version_id = 'video-final-v1'
      nodeRun.output = { asset_id: 'asset-final', asset_version_id: 'video-final-v1', preview_url: assetURL(baseURL, 'video-final-v1') }
    }
  })
  run.finished_at = new Date().toISOString()
  run.updated_at = run.finished_at
  ensureFinalAsset(state, baseURL)
}

function makeRun(state, workflow, body) {
  const now = new Date().toISOString()
  const runID = `run-${String(state.runCreationCount + 1).padStart(3, '0')}`
  state.runCreationCount += 1
  return {
    id: runID,
    workflow_id: workflow.id,
    workflow_revision: workflow.revision,
    status: 'queued',
    progress: 0,
    run_mode: body.run_mode || 'full',
    start_node_id: body.start_node_id || body.node_id || undefined,
    request_id: body.request_id,
    estimated_credits: 1360,
    actual_credits: 0,
    graph_snapshot: cloneFixture(workflow.graph),
    node_runs: workflow.graph.nodes.map((node, index) => ({
      id: `${runID}-node-${String(index + 1).padStart(2, '0')}`, node_id: node.id, node_type: node.type,
      status: 'queued', progress: 0, input_hash: `mock-hash-${node.id}`, credit_cost: 0, cache_hit: false,
    })),
    created_at: now,
    updated_at: now,
    _poll_count: 0,
  }
}

async function handleAPI(req, res, context, url) {
  const { state, baseURL } = context
  const { pathname } = url

  if (pathname === '/api/public/site-info' && req.method === 'GET') {
    return sendJSON(res, 200, {
      'site.name': '灵境智创', 'site.description': 'AI 视频工作流验收环境', 'site.logo_url': '',
      'site.favicon_url': '', 'site.footer': '本地可复现 E2E Mock', 'auth.allow_register': 'false',
    })
  }
  if (pathname === '/api/auth/login' && req.method === 'POST') {
    const body = await readJSON(req)
    if (body.email !== DEMO_EMAIL || body.password !== DEMO_PASSWORD) {
      return sendJSON(res, 401, null, 'invalid email or password')
    }
    return sendJSON(res, 200, {
      user: USER,
      token: { access_token: ACCESS_TOKEN, refresh_token: REFRESH_TOKEN, expires_in: 3600 },
    })
  }
  if (!requireAuth(req, res)) return
  if (pathname === '/api/auth/logout' && req.method === 'POST') return sendJSON(res, 200, { ok: true })
  if (pathname === '/api/me' && req.method === 'GET') return sendJSON(res, 200, { user: USER, role: 'user', permissions: PERMISSIONS })
  if (pathname === '/api/me/menu' && req.method === 'GET') {
    return sendJSON(res, 200, {
      role: 'user', permissions: PERMISSIONS,
      menu: [{ key: 'personal.video-workflows', title: '视频工作流', icon: 'VideoPlay', path: '/personal/video-workflows' }],
    })
  }

  if (pathname === '/api/me/video-workflows/templates' && req.method === 'GET') {
    return sendJSON(res, 200, { items: cloneFixture(state.templates) })
  }
  if (pathname === '/api/me/video-workflows' && req.method === 'GET') {
    return sendJSON(res, 200, { items: cloneFixture(state.workflows) })
  }
  if (pathname === '/api/me/video-workflows' && req.method === 'POST') {
    const body = await readJSON(req)
    const template = state.templates.find((item) => String(item.id) === String(body.template_id)) || state.templates[0]
    const workflow = {
      id: `wf-${randomUUID().slice(0, 8)}`, name: String(body.name || '未命名视频工作流'), template_id: template.id,
      template_version: template.version, revision: 1, graph: cloneFixture(template.graph), latest_run: null,
      created_at: new Date().toISOString(), updated_at: new Date().toISOString(),
    }
    state.workflows.unshift(workflow)
    return sendJSON(res, 201, cloneFixture(workflow))
  }

  let match = pathname.match(/^\/api\/me\/video-workflows\/([^/]+)$/)
  if (match && req.method === 'GET') {
    const workflow = state.workflows.find((item) => item.id === decodeURIComponent(match[1]))
    return workflow ? sendJSON(res, 200, cloneFixture(workflow)) : sendJSON(res, 404, null, 'workflow not found')
  }
  if (match && req.method === 'PUT') {
    const workflow = state.workflows.find((item) => item.id === decodeURIComponent(match[1]))
    if (!workflow) return sendJSON(res, 404, null, 'workflow not found')
    const body = await readJSON(req)
    if (Number(body.revision) !== workflow.revision) return sendJSON(res, 409, null, 'revision conflict')
    workflow.name = String(body.name || workflow.name)
    workflow.graph = cloneFixture(body.graph || workflow.graph)
    workflow.revision += 1
    workflow.updated_at = new Date().toISOString()
    return sendJSON(res, 200, cloneFixture(workflow))
  }
  if (match && req.method === 'DELETE') {
    const index = state.workflows.findIndex((item) => item.id === decodeURIComponent(match[1]))
    if (index < 0) return sendJSON(res, 404, null, 'workflow not found')
    state.workflows.splice(index, 1)
    return sendJSON(res, 200, { ok: true })
  }

  match = pathname.match(/^\/api\/me\/video-workflows\/([^/]+)\/validate$/)
  if (match && req.method === 'POST') {
    const workflow = state.workflows.find((item) => item.id === decodeURIComponent(match[1]))
    if (!workflow) return sendJSON(res, 404, null, 'workflow not found')
    await readJSON(req)
    return sendJSON(res, 200, { valid: true, issues: [] })
  }
  match = pathname.match(/^\/api\/me\/video-workflows\/([^/]+)\/run-estimate$/)
  if (match && req.method === 'POST') {
    const workflow = state.workflows.find((item) => item.id === decodeURIComponent(match[1]))
    if (!workflow) return sendJSON(res, 404, null, 'workflow not found')
    const target = await readJSON(req)
    if (target.revision && Number(target.revision) !== workflow.revision) return sendJSON(res, 409, null, 'revision conflict')
    const token = `estimate-${randomUUID()}`
    state.estimates.set(token, { workflow_id: workflow.id, revision: workflow.revision, target })
    return sendJSON(res, 200, {
      token, expires_at: new Date(Date.now() + 10 * 60_000).toISOString(), total_credits: 1360,
      character_credits: 0, scene_credits: 420, production_credits: 940, cached_credits: 180, balance: USER.credit_balance,
    })
  }
  match = pathname.match(/^\/api\/me\/video-workflows\/([^/]+)\/runs$/)
  if (match && req.method === 'GET') {
    const workflowID = decodeURIComponent(match[1])
    const workflow = state.workflows.find((item) => item.id === workflowID)
    if (!workflow) return sendJSON(res, 404, null, 'workflow not found')
    const limitValue = Number(url.searchParams.get('limit') || 20)
    const offsetValue = Number(url.searchParams.get('offset') || 0)
    const limit = Number.isInteger(limitValue) && limitValue > 0 && limitValue <= 100 ? limitValue : 20
    const offset = Number.isInteger(offsetValue) && offsetValue >= 0 ? offsetValue : 0
    const history = [...state.runs.values()]
      .filter((run) => run.workflow_id === workflowID)
      .sort((a, b) => String(b.created_at).localeCompare(String(a.created_at)) || String(b.id).localeCompare(String(a.id)))
    const items = history.slice(offset, offset + limit).map((run) => {
      const item = exposeRun(run)
      delete item.graph_snapshot
      delete item.node_runs
      delete item.output
      delete item.output_url
      return item
    })
    return sendJSON(res, 200, { items, total: history.length, limit, offset })
  }
  if (match && req.method === 'POST') {
    const workflow = state.workflows.find((item) => item.id === decodeURIComponent(match[1]))
    if (!workflow) return sendJSON(res, 404, null, 'workflow not found')
    const body = await readJSON(req)
    if (!body.request_id) return sendJSON(res, 400, null, 'request_id is required')
    const previousRunID = state.requestRuns.get(body.request_id)
    if (previousRunID) return sendJSON(res, 200, exposeRun(state.runs.get(previousRunID)))
    const estimate = state.estimates.get(body.estimate_token)
    if (!estimate || estimate.workflow_id !== workflow.id || estimate.revision !== workflow.revision) {
      return sendJSON(res, 400, null, 'invalid estimate token')
    }
    if (Number(body.revision) !== workflow.revision) return sendJSON(res, 409, null, 'revision conflict')
    const run = makeRun(state, workflow, body)
    state.runs.set(run.id, run)
    state.requestRuns.set(body.request_id, run.id)
    workflow.latest_run = exposeRun(run)
    return sendJSON(res, 201, exposeRun(run))
  }

  match = pathname.match(/^\/api\/me\/video-workflow-runs\/([^/]+)$/)
  if (match && req.method === 'GET') {
    const run = state.runs.get(decodeURIComponent(match[1]))
    if (!run) return sendJSON(res, 404, null, 'run not found')
    advanceRun(state, run, baseURL)
    return sendJSON(res, 200, exposeRun(run))
  }
  match = pathname.match(/^\/api\/me\/video-workflow-runs\/([^/]+)\/cancel$/)
  if (match && req.method === 'POST') {
    const run = state.runs.get(decodeURIComponent(match[1]))
    if (!run) return sendJSON(res, 404, null, 'run not found')
    await readJSON(req)
    if (['queued', 'running', 'awaiting_character_approval', 'awaiting_storyboard_approval'].includes(run.status)) {
      run.status = 'canceled'
      run.progress = Math.min(run.progress, 99)
      run.finished_at = new Date().toISOString()
      run.node_runs.filter((nodeRun) => !['succeeded', 'failed'].includes(nodeRun.status)).forEach((nodeRun) => { nodeRun.status = 'canceled' })
    }
    return sendJSON(res, 200, { ok: true })
  }
  match = pathname.match(/^\/api\/me\/video-workflow-runs\/([^/]+)\/(approve-characters|approve-storyboard)$/)
  if (match && req.method === 'POST') {
    const run = state.runs.get(decodeURIComponent(match[1]))
    if (!run) return sendJSON(res, 404, null, 'run not found')
    await readJSON(req)
    run.status = 'running'
    return sendJSON(res, 200, { ok: true })
  }

  if (pathname === '/api/me/video-assets' && req.method === 'GET') {
    const kind = url.searchParams.get('kind')
    const keyword = (url.searchParams.get('keyword') || '').toLowerCase()
    const items = state.assets.filter((asset) => (!kind || asset.kind === kind) && (!keyword || asset.name.toLowerCase().includes(keyword)))
    return sendJSON(res, 200, { items: cloneFixture(items), total: items.length })
  }
  if (pathname === '/api/me/video-assets' && req.method === 'POST') {
    const body = await readBody(req)
    state.uploadCount += 1
    const kind = multipartField(body, 'kind') || 'image'
    const name = multipartField(body, 'name') || `上传素材 ${state.uploadCount}`
    const assetID = `asset-upload-${state.uploadCount}`
    const versionID = kind === 'video' ? `video-upload-${state.uploadCount}-v1` : `${assetID}-v1`
    const previewURL = assetURL(baseURL, versionID)
    const version = {
      id: versionID, version: 1, asset_id: assetID, status: 'ready', mime: kind === 'video' ? 'video/mp4' : 'image/svg+xml',
      size_bytes: body.length, width: kind === 'video' ? 360 : 1080, height: kind === 'video' ? 640 : 1920,
      duration_ms: kind === 'video' ? 2000 : undefined, source: 'upload', preview_url: previewURL, created_at: new Date().toISOString(),
    }
    const asset = { id: assetID, name, kind, status: 'ready', current_version_id: versionID, current_version: 1, versions: [version], preview_url: previewURL }
    state.assets.unshift(asset)
    return sendJSON(res, 201, cloneFixture(asset))
  }
  match = pathname.match(/^\/api\/me\/video-assets\/([^/]+)$/)
  if (match && req.method === 'DELETE') {
    const index = state.assets.findIndex((asset) => asset.id === decodeURIComponent(match[1]))
    if (index < 0) return sendJSON(res, 404, null, 'asset not found')
    state.assets.splice(index, 1)
    return sendJSON(res, 200, { ok: true })
  }
  match = pathname.match(/^\/api\/me\/video-assets\/([^/]+)\/versions\/([^/]+)\/transform$/)
  if (match && req.method === 'POST') {
    const assetID = decodeURIComponent(match[1])
    const parentVersionID = decodeURIComponent(match[2])
    const asset = state.assets.find((item) => item.id === assetID)
    if (!asset || asset.kind !== 'image') return sendJSON(res, 404, null, 'image asset not found')
    const transform = await readJSON(req)
    state.transformCount += 1
    const versionID = `${assetID}-transform-${state.transformCount}`
    const version = {
      id: versionID, version: asset.versions.length + 1, asset_id: assetID, status: 'ready', mime: 'image/svg+xml', size_bytes: 4300,
      width: 1080, height: 1920, source: 'transform', parent_version_id: parentVersionID,
      image_transform: transform, metadata: { image_transform: transform }, preview_url: assetURL(baseURL, versionID), created_at: new Date().toISOString(),
    }
    asset.versions.push(version)
    asset.current_version_id = versionID
    asset.current_version = version.version
    asset.preview_url = version.preview_url
    return sendJSON(res, 201, cloneFixture(version))
  }
  match = pathname.match(/^\/api\/me\/video-assets\/([^/]+)\/versions\/([^/]+)\/sign$/)
  if (match && req.method === 'POST') {
    const assetID = decodeURIComponent(match[1])
    const versionID = decodeURIComponent(match[2])
    const body = await readJSON(req)
    const purpose = ['preview', 'download', 'seedance'].includes(body.purpose) ? body.purpose : 'preview'
    const token = `signed-${randomUUID()}`
    state.mediaTokens.set(token, { assetID, versionID, purpose, expiresAt: Date.now() + 10 * 60_000 })
    return sendJSON(res, 200, { url: assetURL(baseURL, versionID, token), expires_at: new Date(Date.now() + 10 * 60_000).toISOString() })
  }

  return sendJSON(res, 404, null, `mock route not found: ${req.method} ${pathname}`)
}

export async function startVideoCanvasMockServer({ host = '127.0.0.1', port = 18080 } = {}) {
  const tempDirectory = mkdtempSync(join(tmpdir(), 'gpt2api-video-canvas-e2e-'))
  const mediaPath = join(tempDirectory, 'video-canvas-2s-h264-aac.mp4')
  generateVideo(mediaPath)
  const context = { state: null, baseURL: '', mediaPath }
  const server = createServer(async (req, res) => {
    setCORS(req, res)
    if (req.method === 'OPTIONS') {
      res.writeHead(204, { 'Content-Length': 0 })
      return res.end()
    }
    const url = new URL(req.url || '/', context.baseURL || 'http://127.0.0.1')
    try {
      if (url.pathname === '/healthz' || url.pathname === '/readyz') {
        return sendJSON(res, 200, { ok: true, service: 'video-canvas-e2e-mock' })
      }
      if (url.pathname === '/__e2e/state' && req.method === 'GET') {
        return sendJSON(res, 200, {
          run_creation_count: context.state.runCreationCount,
          run_ids: [...context.state.runs.keys()],
          request_ids: [...context.state.requestRuns.keys()],
          upload_count: context.state.uploadCount,
          transform_count: context.state.transformCount,
          media_size: statSync(mediaPath).size,
        })
      }
      const mediaMatch = url.pathname.match(/^\/p\/vwf\/([^/]+)$/)
      if (mediaMatch && ['GET', 'HEAD'].includes(req.method || '')) {
        const token = url.searchParams.get('token') || ''
        const grant = context.state.mediaTokens.get(token)
        if (!grant || (grant.expiresAt && grant.expiresAt < Date.now())) return sendJSON(res, 403, null, 'invalid or expired media token')
        const versionID = decodeURIComponent(mediaMatch[1])
        if (/video|final/i.test(versionID)) return serveVideo(req, res, mediaPath, grant.purpose)
        return serveImage(req, res, versionID)
      }
      if (url.pathname.startsWith('/api/')) return await handleAPI(req, res, context, url)
      return sendJSON(res, 404, null, `mock route not found: ${req.method} ${url.pathname}`)
    } catch (error) {
      const status = Number(error?.status || 500)
      return sendJSON(res, status, null, error?.message || 'mock server error')
    }
  })

  await new Promise((resolvePromise, rejectPromise) => {
    server.once('error', rejectPromise)
    server.listen(port, host, resolvePromise)
  })
  const address = server.address()
  const actualPort = typeof address === 'object' && address ? address.port : port
  context.baseURL = `http://${host}:${actualPort}`
  context.state = createFixtureState(context.baseURL)
  context.state.mediaPath = mediaPath

  let closed = false
  return {
    url: context.baseURL,
    state: context.state,
    mediaPath,
    async close() {
      if (closed) return
      closed = true
      await new Promise((resolvePromise, rejectPromise) => server.close((error) => error ? rejectPromise(error) : resolvePromise()))
      rmSync(tempDirectory, { recursive: true, force: true })
    },
  }
}

async function runStandalone() {
  const host = process.env.VIDEO_CANVAS_MOCK_HOST || '127.0.0.1'
  const port = Number(process.env.VIDEO_CANVAS_MOCK_PORT || 18080)
  const mock = await startVideoCanvasMockServer({ host, port })
  console.log(`[video-canvas-mock] API: ${mock.url}`)
  console.log(`[video-canvas-mock] 登录: ${DEMO_EMAIL} / ${DEMO_PASSWORD}`)
  const shutdown = async () => {
    await mock.close()
    process.exit(0)
  }
  process.once('SIGINT', shutdown)
  process.once('SIGTERM', shutdown)
}

const entryURL = process.argv[1] ? pathToFileURL(resolve(process.argv[1])).href : ''
if (entryURL === import.meta.url) {
  runStandalone().catch((error) => {
    console.error(error)
    process.exit(1)
  })
}
