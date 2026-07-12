import assert from 'node:assert/strict'
import { spawnSync } from 'node:child_process'
import test from 'node:test'
import { startVideoCanvasMockServer } from './mock-server.mjs'

test('视频画布浏览器 Mock API 契约', async (t) => {
  const mock = await startVideoCanvasMockServer({ port: 0 })
  t.after(() => mock.close())

  let token = ''
  async function request(path, { method = 'GET', body, auth = true, headers = {} } = {}) {
    const response = await fetch(`${mock.url}${path}`, {
      method,
      headers: {
        ...(auth && token ? { Authorization: `Bearer ${token}` } : {}),
        ...(body !== undefined && !(body instanceof FormData) ? { 'Content-Type': 'application/json' } : {}),
        ...headers,
      },
      body: body === undefined ? undefined : body instanceof FormData ? body : JSON.stringify(body),
    })
    const payload = await response.json()
    return { response, payload, data: payload.data }
  }

  await t.test('真实登录、me 与初始 19 节点状态', async () => {
    const unauthorized = await request('/api/me', { auth: false })
    assert.equal(unauthorized.response.status, 401)

    const login = await request('/api/auth/login', {
      method: 'POST', auth: false, body: { email: 'demo@lingjing.test', password: 'video-canvas' },
    })
    assert.equal(login.response.status, 200)
    token = login.data.token.access_token

    const me = await request('/api/me')
    assert.equal(me.data.user.email, 'demo@lingjing.test')
    assert.ok(me.data.permissions.includes('self:video_workflow'))

    const templates = await request('/api/me/video-workflows/templates')
    const workflows = await request('/api/me/video-workflows')
    const detail = await request(`/api/me/video-workflows/${workflows.data.items[0].id}`)
    assert.equal(templates.data.items.length, 1)
    assert.match(templates.data.items[0].name, /Wan2\.7/)
    assert.equal(detail.data.revision, 12)
    assert.equal(detail.data.graph.nodes.length, 19)
    assert.equal(detail.data.graph.edges.length, 40)
    assert.equal(detail.data.graph.settings.video_model, 'wan2.7-r2v')
    assert.equal(detail.data.graph.nodes.find((node) => node.id === 'background_2').status, 'succeeded')
    assert.match(detail.data.graph.nodes.find((node) => node.id === 'background_2').config.preview_url, /img-s02-v2/)
    assert.equal(detail.data.graph.nodes.find((node) => node.id === 'video_2').status, 'stale')
    const clips = detail.data.graph.nodes.find((node) => node.id === 'timeline').config.clips
    assert.equal(clips.length, 4)
    assert.deepEqual([clips[1].trim_in_ms, clips[1].trim_out_ms], [1200, 12000])
    assert.equal(clips.reduce((total, clip) => total + clip.trim_out_ms - clip.trim_in_ms, 0), 55_800)

    const history = await request('/api/me/video-workflows/wf-rain-reunion/runs?limit=1&offset=0')
    assert.equal(history.data.total, 2)
    assert.equal(history.data.items.length, 1)
    assert.equal(history.data.items[0].id, 'run-history-success')
    assert.equal(history.data.items[0].graph_snapshot, undefined)
    const secondHistory = await request('/api/me/video-workflows/wf-rain-reunion/runs?limit=1&offset=1')
    assert.equal(secondHistory.data.items[0].id, 'run-history-failed')
  })

  await t.test('素材上传、图片变换、版本签名与修订冲突', async () => {
    const assets = await request('/api/me/video-assets?kind=image')
    const s02 = assets.data.items.find((asset) => asset.id === 'asset-s02-image')
    assert.equal(s02.current_version_id, 'img-s02-v2')
    assert.equal(s02.versions.length, 2)

    const form = new FormData()
    form.append('file', new Blob(['mock image'], { type: 'image/png' }), 'replacement.png')
    form.append('name', '上传替换图')
    form.append('kind', 'image')
    const upload = await request('/api/me/video-assets', { method: 'POST', body: form })
    assert.equal(upload.response.status, 201)
    assert.equal(upload.data.kind, 'image')

    const transform = {
      crop: { x: 0.1, y: 0.1, width: 0.8, height: 0.8 }, rotation: 90,
      flip_horizontal: true, flip_vertical: false,
    }
    const transformed = await request('/api/me/video-assets/asset-s02-image/versions/img-s02-v2/transform', { method: 'POST', body: transform })
    assert.equal(transformed.response.status, 201)
    assert.equal(transformed.data.source, 'transform')
    assert.deepEqual(transformed.data.image_transform, transform)

    const signed = await request(`/api/me/video-assets/asset-s02-image/versions/${transformed.data.id}/sign`, {
      method: 'POST', body: { purpose: 'preview' },
    })
    const preview = await fetch(signed.data.url)
    assert.equal(preview.status, 200)
    assert.match(preview.headers.get('content-type') || '', /image\/svg\+xml/)

    const detail = await request('/api/me/video-workflows/wf-rain-reunion')
    const saved = await request('/api/me/video-workflows/wf-rain-reunion', {
      method: 'PUT', body: { name: detail.data.name, revision: detail.data.revision, graph: detail.data.graph },
    })
    assert.equal(saved.data.revision, 13)
    const conflict = await request('/api/me/video-workflows/wf-rain-reunion', {
      method: 'PUT', body: { name: detail.data.name, revision: 12, graph: detail.data.graph },
    })
    assert.equal(conflict.response.status, 409)
  })

  await t.test('10 次幂等启动、轮询成功、取消与 Range 播放', async () => {
    const detail = await request('/api/me/video-workflows/wf-rain-reunion')
    const estimate = await request('/api/me/video-workflows/wf-rain-reunion/run-estimate', {
      method: 'POST', body: { revision: detail.data.revision, run_mode: 'full' },
    })
    const runBody = {
      revision: detail.data.revision, run_mode: 'full', estimate_token: estimate.data.token, request_id: 'contract-idempotency-001',
    }
    const submissions = await Promise.all(Array.from({ length: 10 }, () => request('/api/me/video-workflows/wf-rain-reunion/runs', { method: 'POST', body: runBody })))
    assert.equal(new Set(submissions.map((item) => item.data.id)).size, 1)
    const runID = submissions[0].data.id
    const diagnostic = await request('/__e2e/state', { auth: false })
    assert.equal(diagnostic.data.run_creation_count, 1)
    const historyAfterStart = await request('/api/me/video-workflows/wf-rain-reunion/runs')
    assert.equal(historyAfterStart.data.total, 3)

    let run
    for (let index = 0; index < 3; index += 1) run = await request(`/api/me/video-workflow-runs/${runID}`)
    assert.equal(run.data.status, 'succeeded')
    assert.equal(run.data.progress, 100)
    assert.equal(run.data.output.asset_id, 'asset-final')

    const signedVideo = await request('/api/me/video-assets/asset-final/versions/video-final-v1/sign', {
      method: 'POST', body: { purpose: 'preview' },
    })
    const head = await fetch(signedVideo.data.url, { method: 'HEAD' })
    assert.equal(head.status, 200)
    assert.equal(head.headers.get('accept-ranges'), 'bytes')
    assert.ok(Number(head.headers.get('content-length')) > 1024)
    const ranged = await fetch(signedVideo.data.url, { headers: { Range: 'bytes=0-1023' } })
    assert.equal(ranged.status, 206)
    assert.match(ranged.headers.get('content-range') || '', /^bytes 0-1023\//)
    assert.equal((await ranged.arrayBuffer()).byteLength, 1024)
    const ifRangeMiss = await fetch(signedVideo.data.url, { headers: { Range: 'bytes=0-99', 'If-Range': '"different"' } })
    assert.equal(ifRangeMiss.status, 200)

    const cancelEstimate = await request('/api/me/video-workflows/wf-rain-reunion/run-estimate', {
      method: 'POST', body: { revision: detail.data.revision, run_mode: 'node_only', start_node_id: 'video_2' },
    })
    const cancelRun = await request('/api/me/video-workflows/wf-rain-reunion/runs', {
      method: 'POST', body: {
        revision: detail.data.revision, run_mode: 'node_only', start_node_id: 'video_2',
        estimate_token: cancelEstimate.data.token, request_id: 'contract-cancel-001',
      },
    })
    const canceled = await request(`/api/me/video-workflow-runs/${cancelRun.data.id}/cancel`, { method: 'POST', body: {} })
    assert.equal(canceled.data.ok, true)
    const canceledDetail = await request(`/api/me/video-workflow-runs/${cancelRun.data.id}`)
    assert.equal(canceledDetail.data.status, 'canceled')
  })

  await t.test('媒体编码为 H.264 / AAC 48kHz 双声道', () => {
    const probe = spawnSync('ffprobe', [
      '-v', 'error', '-show_entries', 'stream=codec_type,codec_name,sample_rate,channels', '-of', 'json', mock.mediaPath,
    ], { encoding: 'utf8' })
    assert.equal(probe.status, 0, probe.stderr)
    const streams = JSON.parse(probe.stdout).streams
    const video = streams.find((stream) => stream.codec_type === 'video')
    const audio = streams.find((stream) => stream.codec_type === 'audio')
    assert.equal(video.codec_name, 'h264')
    assert.equal(audio.codec_name, 'aac')
    assert.equal(audio.sample_rate, '48000')
    assert.equal(audio.channels, 2)
  })
})
