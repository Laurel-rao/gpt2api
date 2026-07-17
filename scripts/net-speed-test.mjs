#!/usr/bin/env node
/**
 * 网络/页面资源测速脚本
 *
 * 典型用法:
 *   node scripts/net-speed-test.mjs --page https://ai.reeko.net.cn:8081/admin/ops --rounds 3 --concurrency 4
 *   node scripts/net-speed-test.mjs --url https://ai.reeko.net.cn:8081/health --url https://ai.reeko.net.cn:8000/healthz
 *   node scripts/net-speed-test.mjs --preset sub2api --rounds 5 --json output/sub2api-speed.json
 *
 * 说明:
 *   - 底层调用 curl,用于拿到 DNS / TCP connect / TLS / TTFB / total / speed 等精确指标。
 *   - --page 会先拉取 HTML,自动发现 src/href 中的 JS/CSS/图片等资源并一起测速。
 *   - 默认使用 --compressed,测到的是浏览器更接近的 gzip/br 下载体积。
 */

import { argv, exit } from 'node:process'
import { writeFile } from 'node:fs/promises'
import { execFile } from 'node:child_process'
import { promisify } from 'node:util'

const execFileAsync = promisify(execFile)

const args = parseArgs(argv.slice(2))
const rounds = positiveInt(args.rounds, 3)
const concurrency = positiveInt(args.concurrency, 4)
const timeoutSec = positiveInt(args.timeout, 30)
const limitAssets = positiveInt(args['limit-assets'], 40)
const compressed = args.compressed !== 'false'
const insecure = args.insecure === 'true' || args.k === 'true'
const followRedirect = args.redirect !== 'false'
const includeAssets = args.assets !== 'false'
const httpMode = args.http || 'auto' // auto | 1.1 | 2
const outputJson = args.json || ''

const presetUrls = {
  sub2api: [
    'https://ai.reeko.net.cn:8081/health',
    'https://ai.reeko.net.cn:8081/admin/ops',
    'https://ai.reeko.net.cn:8000/healthz',
  ],
}

main().catch((err) => {
  console.error(`\n测速脚本异常: ${err.message}`)
  exit(2)
})

async function main() {
  await ensureCurl()

  const urls = new Set()
  for (const preset of asArray(args.preset)) {
    for (const url of presetUrls[preset] || []) urls.add(url)
    if (!presetUrls[preset]) console.warn(`未知 preset: ${preset}`)
  }
  for (const url of asArray(args.url)) urls.add(url)
  for (const page of asArray(args.page)) {
    urls.add(page)
    if (includeAssets) {
      const assets = await discoverAssets(page, limitAssets)
      for (const asset of assets) urls.add(asset)
    }
  }

  if (urls.size === 0) {
    printHelp()
    exit(1)
  }

  const jobs = []
  for (const url of urls) {
    for (let i = 1; i <= rounds; i++) jobs.push({ url, round: i })
  }

  console.log(`测速目标: ${urls.size} 个 URL, rounds=${rounds}, concurrency=${concurrency}, http=${httpMode}, compressed=${compressed}`)
  console.log(`curl timeout=${timeoutSec}s${insecure ? ', insecure=true' : ''}\n`)

  const results = await runPool(jobs, concurrency, testUrl)
  const summary = summarize(results)

  printSummary(summary)

  if (outputJson) {
    await writeFile(outputJson, JSON.stringify({ options: {
      rounds, concurrency, timeoutSec, compressed, insecure, followRedirect, httpMode,
    }, summary, results }, null, 2))
    console.log(`\nJSON 已写入: ${outputJson}`)
  }

  const failed = results.filter((r) => r.error || Number(r.http_code) >= 500).length
  exit(failed > 0 ? 1 : 0)
}

function parseArgs(items) {
  const out = {}
  for (let i = 0; i < items.length; i++) {
    const item = items[i]
    if (!item.startsWith('--')) continue
    const key = item.slice(2)
    const next = items[i + 1]
    const value = next && !next.startsWith('--') ? items[++i] : 'true'
    if (out[key] === undefined) out[key] = value
    else if (Array.isArray(out[key])) out[key].push(value)
    else out[key] = [out[key], value]
  }
  return out
}

function asArray(value) {
  if (value === undefined) return []
  return Array.isArray(value) ? value : [value]
}

function positiveInt(value, fallback) {
  const n = Number(value)
  return Number.isFinite(n) && n > 0 ? Math.floor(n) : fallback
}

async function ensureCurl() {
  try {
    await execFileAsync('curl', ['--version'], { timeout: 5000 })
  } catch {
    throw new Error('找不到 curl。请先安装 curl 后再运行。')
  }
}

async function discoverAssets(pageUrl, limit) {
  const curlArgs = baseCurlArgs().concat(['-o', '-', pageUrl])
  let stdout = ''
  try {
    ;({ stdout } = await execFileAsync('curl', curlArgs, {
      timeout: timeoutSec * 1000,
      maxBuffer: 10 * 1024 * 1024,
    }))
  } catch (err) {
    console.warn(`页面资源发现失败: ${pageUrl} (${err.message})`)
    return []
  }

  const assets = []
  const seen = new Set()
  const re = /\b(?:src|href)=["']([^"']+)["']/gi
  let match
  while ((match = re.exec(stdout))) {
    const raw = match[1]
    if (!isAssetLike(raw)) continue
    const full = absolutize(raw, pageUrl)
    if (!full || seen.has(full)) continue
    seen.add(full)
    assets.push(full)
    if (assets.length >= limit) break
  }
  console.log(`页面资源发现: ${pageUrl} -> ${assets.length} 个资源`)
  return assets
}

function isAssetLike(url) {
  if (url.startsWith('data:') || url.startsWith('blob:') || url.startsWith('#')) return false
  return /\.(js|css|png|jpg|jpeg|webp|svg|ico|woff2?|ttf|map)(\?|#|$)/i.test(url)
}

function absolutize(raw, base) {
  try {
    return new URL(raw, base).toString()
  } catch {
    return ''
  }
}

function baseCurlArgs() {
  const list = ['-sS']
  if (compressed) list.push('--compressed')
  if (insecure) list.push('-k')
  if (followRedirect) list.push('-L')
  if (httpMode === '1.1') list.push('--http1.1')
  if (httpMode === '2') list.push('--http2')
  list.push('--connect-timeout', String(timeoutSec), '--max-time', String(timeoutSec))
  return list
}

async function testUrl(job) {
  const writeOut = [
    'url_effective=%{url_effective}',
    'http_code=%{http_code}',
    'http_version=%{http_version}',
    'remote_ip=%{remote_ip}',
    'local_ip=%{local_ip}',
    'time_namelookup=%{time_namelookup}',
    'time_connect=%{time_connect}',
    'time_appconnect=%{time_appconnect}',
    'time_starttransfer=%{time_starttransfer}',
    'time_total=%{time_total}',
    'size_download=%{size_download}',
    'speed_download=%{speed_download}',
    'num_connects=%{num_connects}',
    'redirect_count=%{num_redirects}',
    'content_type=%{content_type}',
  ].join('\\n')

  const curlArgs = baseCurlArgs().concat(['-o', '/dev/null', '-w', writeOut, job.url])
  const startedAt = new Date().toISOString()
  try {
    const { stdout, stderr } = await execFileAsync('curl', curlArgs, {
      timeout: (timeoutSec + 5) * 1000,
      maxBuffer: 2 * 1024 * 1024,
    })
    const parsed = parseCurlWriteOut(stdout)
    const result = {
      ...job,
      started_at: startedAt,
      error: '',
      stderr: stderr.trim(),
      ...parsed,
    }
    logOne(result)
    return result
  } catch (err) {
    const result = {
      ...job,
      started_at: startedAt,
      error: err.stderr?.trim() || err.message,
    }
    logOne(result)
    return result
  }
}

function parseCurlWriteOut(text) {
  const out = {}
  for (const line of text.split(/\r?\n/)) {
    const idx = line.indexOf('=')
    if (idx <= 0) continue
    const key = line.slice(0, idx)
    const value = line.slice(idx + 1)
    out[key] = numericKeys.has(key) ? Number(value) : value
  }
  return out
}

const numericKeys = new Set([
  'http_code',
  'http_version',
  'time_namelookup',
  'time_connect',
  'time_appconnect',
  'time_starttransfer',
  'time_total',
  'size_download',
  'speed_download',
  'num_connects',
  'redirect_count',
])

async function runPool(jobs, size, worker) {
  const results = new Array(jobs.length)
  let next = 0
  const workers = Array.from({ length: Math.min(size, jobs.length) }, async () => {
    while (next < jobs.length) {
      const index = next++
      results[index] = await worker(jobs[index])
    }
  })
  await Promise.all(workers)
  return results
}

function summarize(results) {
  const groups = new Map()
  for (const r of results) {
    if (!groups.has(r.url)) groups.set(r.url, [])
    groups.get(r.url).push(r)
  }

  return [...groups.entries()].map(([url, items]) => {
    const ok = items.filter((x) => !x.error && x.http_code > 0)
    return {
      url,
      count: items.length,
      ok: ok.length,
      fail: items.length - ok.length,
      http_codes: [...new Set(ok.map((x) => x.http_code))].join(','),
      http_versions: [...new Set(ok.map((x) => x.http_version))].join(','),
      remote_ips: [...new Set(ok.map((x) => x.remote_ip).filter(Boolean))].join(','),
      total: stat(ok.map((x) => x.time_total)),
      ttfb: stat(ok.map((x) => x.time_starttransfer)),
      connect: stat(ok.map((x) => x.time_connect)),
      tls: stat(ok.map((x) => x.time_appconnect).filter((x) => x > 0)),
      speed: stat(ok.map((x) => x.speed_download)),
      size: stat(ok.map((x) => x.size_download)),
      errors: items.filter((x) => x.error).map((x) => x.error),
    }
  }).sort((a, b) => b.total.p95 - a.total.p95)
}

function stat(values) {
  const arr = values.filter((x) => Number.isFinite(x)).sort((a, b) => a - b)
  if (arr.length === 0) return { min: 0, avg: 0, p50: 0, p95: 0, max: 0 }
  return {
    min: arr[0],
    avg: arr.reduce((a, b) => a + b, 0) / arr.length,
    p50: percentile(arr, 0.5),
    p95: percentile(arr, 0.95),
    max: arr[arr.length - 1],
  }
}

function percentile(sorted, p) {
  if (sorted.length === 1) return sorted[0]
  const idx = Math.ceil(sorted.length * p) - 1
  return sorted[Math.max(0, Math.min(sorted.length - 1, idx))]
}

function logOne(r) {
  const name = trimUrl(r.url)
  if (r.error) {
    console.log(`FAIL r${r.round} ${name} :: ${r.error}`)
    return
  }
  console.log([
    `r${r.round}`,
    `http=${r.http_code}`,
    `v=${r.http_version}`,
    `total=${sec(r.time_total)}`,
    `ttfb=${sec(r.time_starttransfer)}`,
    `speed=${kbps(r.speed_download)}`,
    `size=${bytes(r.size_download)}`,
    name,
  ].join('  '))
}

function printSummary(rows) {
  console.log('\n汇总(按 total p95 从慢到快):')
  console.log([
    pad('OK', 7),
    pad('HTTP', 8),
    pad('VER', 6),
    pad('TOTAL avg/p95', 18),
    pad('TTFB avg/p95', 18),
    pad('SPEED avg', 12),
    pad('SIZE avg', 10),
    'URL',
  ].join('  '))
  for (const row of rows) {
    console.log([
      pad(`${row.ok}/${row.count}`, 7),
      pad(row.http_codes || '-', 8),
      pad(row.http_versions || '-', 6),
      pad(`${sec(row.total.avg)}/${sec(row.total.p95)}`, 18),
      pad(`${sec(row.ttfb.avg)}/${sec(row.ttfb.p95)}`, 18),
      pad(kbps(row.speed.avg), 12),
      pad(bytes(row.size.avg), 10),
      row.url,
    ].join('  '))
    if (row.errors.length) {
      for (const err of row.errors.slice(0, 3)) console.log(`  error: ${err}`)
    }
  }
}

function sec(n) {
  return `${Number(n || 0).toFixed(3)}s`
}

function kbps(bytesPerSec) {
  const n = Number(bytesPerSec || 0)
  if (n >= 1024 * 1024) return `${(n / 1024 / 1024).toFixed(2)}MB/s`
  return `${(n / 1024).toFixed(1)}KB/s`
}

function bytes(n) {
  const x = Number(n || 0)
  if (x >= 1024 * 1024) return `${(x / 1024 / 1024).toFixed(2)}MB`
  if (x >= 1024) return `${(x / 1024).toFixed(1)}KB`
  return `${Math.round(x)}B`
}

function pad(s, len) {
  const raw = String(s)
  return raw + ' '.repeat(Math.max(0, len - raw.length))
}

function trimUrl(url) {
  try {
    const u = new URL(url)
    const path = u.pathname.length > 80 ? `${u.pathname.slice(0, 77)}...` : u.pathname
    return `${u.origin}${path}${u.search ? '?' : ''}`
  } catch {
    return url
  }
}

function printHelp() {
  console.log(`用法:
  node scripts/net-speed-test.mjs --page <URL> [--rounds 3] [--concurrency 4]
  node scripts/net-speed-test.mjs --url <URL> --url <URL>
  node scripts/net-speed-test.mjs --preset sub2api --json output/speed.json

参数:
  --page URL           拉取页面并自动发现 JS/CSS/图片资源
  --url URL            指定单个测速 URL,可重复
  --preset sub2api     内置对比: 新机 health/admin + 旧机 admin
  --rounds N           每个 URL 测 N 次,默认 3
  --concurrency N      并发数,默认 4
  --timeout N          单请求超时秒数,默认 30
  --limit-assets N     --page 自动发现资源上限,默认 40
  --http auto|1.1|2    指定 HTTP 协议,默认 auto
  --compressed false   关闭 gzip/br 请求
  --insecure true      跳过证书校验,等同 curl -k
  --redirect false     不跟随跳转
  --assets false       --page 时不自动测速静态资源
  --json FILE          输出完整 JSON 结果
`)
}
