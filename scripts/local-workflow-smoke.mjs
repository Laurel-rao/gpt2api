#!/usr/bin/env node
/**
 * 用本地 mock 上游跑一轮视频工作流：创建 → 估价 → 运行 →（必要时）审批 → 等到成片。
 *
 *   node scripts/local-workflow-smoke.mjs \
 *     --base http://127.0.0.1:8080 \
 *     --email sidebar-test-xxx@local.test \
 *     --pass Test123456!
 */
import { randomUUID } from 'node:crypto'

const BASE = arg('--base', process.env.GPT2API_BASE || 'http://127.0.0.1:8080')
const EMAIL = arg('--email', process.env.SMOKE_EMAIL || '')
const PASS = arg('--pass', process.env.SMOKE_PASS || 'Test123456!')

function arg(name, fallback = '') {
  const i = process.argv.indexOf(name)
  return i >= 0 ? process.argv[i + 1] : fallback
}

async function call(method, path, { token, body } = {}) {
  const headers = { Accept: 'application/json' }
  if (token) headers.Authorization = `Bearer ${token}`
  if (body !== undefined) headers['Content-Type'] = 'application/json'
  const res = await fetch(`${BASE}${path}`, {
    method,
    headers,
    body: body === undefined ? undefined : JSON.stringify(body),
  })
  const text = await res.text()
  let json
  try { json = JSON.parse(text) } catch { json = { raw: text } }
  if (!res.ok || (json.code !== undefined && json.code !== 0)) {
    throw new Error(`${method} ${path} → ${res.status} ${text.slice(0, 600)}`)
  }
  return json.data
}

function sleep(ms) { return new Promise((r) => setTimeout(r, ms)) }

async function main() {
  let email = EMAIL
  let pass = PASS
  if (!email) {
    email = `local-wf-${Date.now()}@local.test`
    pass = 'Test123456!'
    await call('POST', '/api/auth/register', { body: { email, password: pass, nickname: 'local-wf' } }).catch(() => {})
    console.log(`[smoke] ensure account ${email}`)
  }

  const login = await call('POST', '/api/auth/login', { body: { email, password: pass } })
  const token = login.token.access_token
  console.log(`[smoke] logged in as ${email} role=${login.user.role}`)

  const templates = await call('GET', '/api/me/video-workflows/templates', { token })
  const list = templates.items || templates || []
  const template = list.find((t) => String(t.code || '').includes('ancient')) || list[0]
  if (!template) throw new Error('no video workflow template')
  console.log(`[smoke] template id=${template.id} code=${template.code}`)

  let wf = await call('POST', '/api/me/video-workflows', {
    token,
    body: { name: `本地 mock 验收 ${new Date().toISOString().slice(11, 19)}`, template_id: template.id },
  })
  console.log(`[smoke] workflow ${wf.id} rev=${wf.revision}`)

  // character: auto_first; storyboard: auto; video model → local echoon mock
  if (wf.graph?.settings) {
    wf.graph.settings.character_approval_policy = 'auto_first'
    wf.graph.settings.storyboard_approval_policy = 'auto'
    wf.graph.settings.video_model = 'doubao-seedance-2-0-fast-260128'
    wf = await call('PUT', `/api/me/video-workflows/${wf.id}`, {
      token,
      body: { name: wf.name, graph: wf.graph, revision: wf.revision },
    })
    console.log(`[smoke] approval/video model updated, rev=${wf.revision}`)
  }

  const estimate = await call('POST', `/api/me/video-workflows/${wf.id}/run-estimate`, {
    token,
    body: { run_mode: 'full' },
  })
  console.log(`[smoke] estimate credits=${estimate.total_credits} token=${String(estimate.token || '').slice(0, 16)}…`)

  const run = await call('POST', `/api/me/video-workflows/${wf.id}/runs`, {
    token,
    body: {
      revision: wf.revision,
      run_mode: 'full',
      estimate_token: estimate.token,
      request_id: `local-smoke-${randomUUID()}`,
    },
  })
  console.log(`[smoke] run ${run.id} status=${run.status}`)

  const started = Date.now()
  while (Date.now() - started < 12 * 60 * 1000) {
    const cur = await call('GET', `/api/me/video-workflow-runs/${run.id}`, { token })
    const nodes = cur.node_runs || []
    const brief = nodes.map((n) => `${n.node_id}:${n.status}`).join(',')
    process.stdout.write(`\r[smoke] ${cur.status} p=${cur.progress ?? '-'} [${brief.slice(0, 120)}]   `)

    if (cur.status === 'awaiting_character_approval') {
      const pending = nodes.filter((n) => n.node_type === 'character' && n.status === 'awaiting_approval')
      const selections = []
      for (const node of pending) {
        let output = node.output
        if (typeof output === 'string') {
          try { output = JSON.parse(output) } catch { output = {} }
        }
        const candidates = output?.candidates || output?.versions || []
        const selected = candidates[0]?.id || candidates[0]?.version_id || candidates[0]
        if (!selected) {
          console.warn(`\n[smoke] character node ${node.node_id} has no candidates`, output)
          continue
        }
        selections.push({
          node_run_id: node.id,
          input_hash: node.input_hash,
          selected_version_id: String(selected),
        })
      }
      if (selections.length) {
        await call('POST', `/api/me/video-workflow-runs/${run.id}/approve-characters`, {
          token,
          body: { selections },
        })
        console.log(`\n[smoke] approved ${selections.length} characters`)
      }
    }

    if (cur.status === 'awaiting_storyboard_approval') {
      const script = nodes.find((n) => n.node_type === 'script')
      let output = script?.output
      if (typeof output === 'string') {
        try { output = JSON.parse(output) } catch { output = { scenes: [] } }
      }
      await call('POST', `/api/me/video-workflow-runs/${run.id}/approve-storyboard`, {
        token,
        body: {
          node_run_id: script?.id,
          input_hash: script?.input_hash,
          script: output || { scenes: [] },
        },
      })
      console.log('\n[smoke] approved storyboard')
    }

    if (['succeeded', 'failed', 'canceled'].includes(cur.status)) {
      console.log(`\n[smoke] finished: ${cur.status}`)
      if (cur.status !== 'succeeded') {
        const failed = nodes.filter((n) => n.status === 'failed')
        for (const n of failed) {
          console.error(`  node ${n.node_id}: ${n.error_message || n.error || JSON.stringify(n).slice(0, 300)}`)
        }
        process.exit(1)
      }
      process.exit(0)
    }
    await sleep(2000)
  }
  console.error('\n[smoke] timeout')
  process.exit(1)
}

main().catch((err) => {
  console.error('[smoke] fatal', err)
  process.exit(2)
})
