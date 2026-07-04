<script setup lang="ts">
import { ref, computed, reactive, onMounted, onUnmounted, watch } from 'vue'
import { ElMessage } from 'element-plus/es/components/message/index.mjs'
import { ElMessageBox } from 'element-plus/es/components/message-box/index.mjs'
import {
  Refresh,
  Check,
  Setting,
  Lock,
  User,
  Connection,
  Wallet,
  Message as MailIcon,
  Picture,
  ChatDotRound,
  VideoPlay,
  Upload,
  Delete,
} from '@element-plus/icons-vue'
import {
  listSettings,
  updateSettings,
  reloadSettings,
  sendTestEmail,
  testImageGen,
  testTextGen,
  testVideoGen,
  fetchVideoGenBalance,
  startVideoGenGenerateTest,
  getVideoGenGenerateTest,
  uploadSiteAsset,
  type SettingItem,
  type VideoGenBalance,
  type VideoGenGenerateTestState,
  type VideoGenProbeModel,
} from '@/api/settings'
import { useSiteStore } from '@/stores/site'

const loading = ref(false)
const saving = ref(false)
const items = ref<SettingItem[]>([])
// 本地编辑态,key -> value(string)
const draft = reactive<Record<string, string>>({})

const tabs = [
  { name: 'site', label: '通用设置', icon: Setting },
  { name: 'auth', label: '安全与认证', icon: Lock },
  { name: 'defaults', label: '用户默认值', icon: User },
  { name: 'gateway', label: '网关服务', icon: Connection },
  { name: 'imagegen', label: '生图网关', icon: Picture },
  { name: 'textgen', label: '文本网关', icon: ChatDotRound },
  { name: 'videogen', label: '视频网关', icon: VideoPlay },
  { name: 'billing', label: '计费与充值', icon: Wallet },
  { name: 'mail', label: '邮件设置', icon: MailIcon },
] as const
const activeTab = ref<(typeof tabs)[number]['name']>('site')

const grouped = computed(() => {
  const map: Record<string, SettingItem[]> = {
    site: [], auth: [], defaults: [], gateway: [], imagegen: [], textgen: [], videogen: [], billing: [], mail: [],
  }
  for (const it of items.value) {
    // 旧 category "limit" 归并到 defaults 显示
    const cat = it.category === 'limit' ? 'defaults' : it.category
    ;(map[cat] ||= []).push(it)
  }
  for (const k of Object.keys(map)) map[k].sort(settingSorter(k))
  return map
})

function settingSorter(category: string) {
  if (category !== 'videogen') return (a: SettingItem, b: SettingItem) => a.key.localeCompare(b.key)
  const order = new Map([
    ['videogen.enabled', 10],
    ['videogen.channel_type', 20],
    ['videogen.account', 110],
    ['videogen.api_key', 120],
    ['videogen.base_url', 130],
    ['videogen.model', 140],
    ['videogen.apiyi_seedance2.account', 210],
    ['videogen.apiyi_seedance2.api_key', 220],
    ['videogen.apiyi_seedance2.base_url', 230],
    ['videogen.apiyi_seedance2.model', 240],
    ['videogen.apiyi_wan27.account', 250],
    ['videogen.apiyi_wan27.api_key', 260],
    ['videogen.apiyi_wan27.base_url', 270],
    ['videogen.apiyi_wan27.model', 280],
    ['videogen.apiyi_happyhorse.account', 290],
    ['videogen.apiyi_happyhorse.api_key', 300],
    ['videogen.apiyi_happyhorse.base_url', 310],
    ['videogen.apiyi_happyhorse.model', 320],
    ['videogen.aspect_ratio', 410],
    ['videogen.resolution', 420],
    ['videogen.duration_sec', 430],
    ['videogen.timeout_sec', 440],
    ['videogen.generate_audio', 450],
    ['videogen.billing_ratio', 460],
  ])
  return (a: SettingItem, b: SettingItem) => (order.get(a.key) || 999) - (order.get(b.key) || 999) || a.key.localeCompare(b.key)
}

const dirtyCount = computed(() => {
  let n = 0
  for (const it of items.value) {
    if (String(draft[it.key] ?? '') !== String(it.value)) n++
  }
  return n
})

async function load() {
  loading.value = true
  try {
    const d = await listSettings()
    items.value = d.items
    for (const it of d.items) draft[it.key] = it.value
  } finally {
    loading.value = false
  }
}

function reset() {
  for (const it of items.value) draft[it.key] = it.value
  ElMessage.info('已重置为服务端当前值')
}

function isBool(it: SettingItem) { return it.type === 'bool' }
function isInt(it: SettingItem) { return it.type === 'int' }
function isFloat(it: SettingItem) { return it.type === 'float' }
function isPassword(it: SettingItem) { return it.type === 'password' }
function isFavicon(it: SettingItem) { return it.key === 'site.favicon_url' }
function isLogo(it: SettingItem) { return it.key === 'site.logo_url' }
function isSiteAsset(it: SettingItem) { return isFavicon(it) || isLogo(it) }
function siteAssetName(it: SettingItem) { return isLogo(it) ? 'Logo' : '图标' }
function inputType(it: SettingItem) {
  if (it.type === 'email') return 'email'
  if (it.type === 'url') return 'url'
  return 'text'
}
function floatInputProps(it: SettingItem) {
  if (it.key === 'gateway.daily_usage_ratio') {
    return { min: 0, max: 1, step: 0.05, precision: 2 }
  }
  if (it.key === 'videogen.billing_ratio') {
    return { min: 0.01, max: 1000, step: 1, precision: 2 }
  }
  return { min: 0, max: 1000, step: 0.1, precision: 2 }
}

const imageGenTesting = ref(false)
const textGenTesting = ref(false)
const videoChannelTesting = reactive<Record<string, boolean>>({})
const videoChannelGenerating = reactive<Record<string, boolean>>({})
const videoChannelBalanceLoading = reactive<Record<string, boolean>>({})
const videoChannelBalances = reactive<Record<string, VideoGenBalance | null>>({})
const videoChannelModels = reactive<Record<string, VideoGenProbeModel[]>>({})
const videoGenerateTestStates = reactive<Record<string, VideoGenGenerateTestState | null>>({})
const videoGenerateDlg = ref(false)
const videoGenerateRow = ref<VideoChannelRow | null>(null)
const videoGeneratePrompt = ref('参考图片，生成一段 5 秒商品展示短视频，镜头缓慢推进，主体清晰，光线自然。')
const videoGenerateMode = ref<'text' | 'image' | 'video'>('text')
const videoGenerateImage = ref<File | null>(null)
const videoGeneratePreview = ref('')
const videoGenerateVideo = ref<File | null>(null)
const videoGenerateVideoPreview = ref('')
const videoGeneratePolling = reactive<Record<string, number>>({})
const echoonDefaultBaseURL = 'http://app.echoon.top/api/v1'
const echoonDefaultModel = '0e37fa2d-72b3-483a-81b4-ad595cd147c7'
const apiyiDefaultBaseURL = 'https://api.apiyi.com'
const apiyiDefaultModel = 'doubao-seedance-2-0-fast-260128'
const apiyiStandardModel = 'doubao-seedance-2-0-260128'
const apiyiWan27DefaultModel = 'wan2.7-r2v'
const apiyiWan27TextModel = 'wan2.7-t2v'
const apiyiWan27ImageModel = 'wan2.7-i2v'
const apiyiHappyHorseDefaultModel = 'happyhorse-1.0-r2v'
const apiyiHappyHorseTextModel = 'happyhorse-1.0-t2v'
const apiyiHappyHorseImageModel = 'happyhorse-1.0-i2v'
const apiyiBuiltInModels = [
  { id: apiyiDefaultModel, name: 'API易 Seedance 2.0 Fast', type: 'video', label: 'API易 Seedance 2.0 Fast (video)', value: apiyiDefaultModel },
  { id: apiyiStandardModel, name: 'API易 Seedance 2.0 Standard', type: 'video', label: 'API易 Seedance 2.0 Standard (video)', value: apiyiStandardModel },
]
const apiyiWan27BuiltInModels = [
  { id: apiyiWan27DefaultModel, name: 'API易 Wan2.7 参考图生视频', type: 'video', label: 'API易 Wan2.7 参考图生视频 (video)', value: apiyiWan27DefaultModel },
  { id: apiyiWan27TextModel, name: 'API易 Wan2.7 文生视频', type: 'video', label: 'API易 Wan2.7 文生视频 (video)', value: apiyiWan27TextModel },
  { id: apiyiWan27ImageModel, name: 'API易 Wan2.7 图生视频', type: 'video', label: 'API易 Wan2.7 图生视频 (video)', value: apiyiWan27ImageModel },
]
const apiyiHappyHorseBuiltInModels = [
  { id: apiyiHappyHorseDefaultModel, name: 'API易 HappyHorse 参考图生视频', type: 'video', label: 'API易 HappyHorse 参考图生视频 (video)', value: apiyiHappyHorseDefaultModel },
  { id: apiyiHappyHorseTextModel, name: 'API易 HappyHorse 文生视频', type: 'video', label: 'API易 HappyHorse 文生视频 (video)', value: apiyiHappyHorseTextModel },
  { id: apiyiHappyHorseImageModel, name: 'API易 HappyHorse 图生视频', type: 'video', label: 'API易 HappyHorse 图生视频 (video)', value: apiyiHappyHorseImageModel },
]
type VideoChannelType = 'echoon' | 'apiyi_seedance2' | 'apiyi_wan27' | 'apiyi_happyhorse'

interface VideoChannelRow {
  type: VideoChannelType
  name: string
  desc: string
  accountKey: string
  apiKeyKey: string
  baseURLKey: string
  modelKey: string
}

const videoChannelRows: VideoChannelRow[] = [
  {
    type: 'echoon',
    name: 'Echoon / AI Gen Platform',
    desc: '使用现有 /models、/generate、/generate/tasks 接口，余额查询受支持',
    accountKey: 'videogen.account',
    apiKeyKey: 'videogen.api_key',
    baseURLKey: 'videogen.base_url',
    modelKey: 'videogen.model',
  },
  {
    type: 'apiyi_seedance2',
    name: 'API易 Seedance 2.0',
    desc: '使用 API易 Seedance 2.0 异步任务接口，余额查询不支持',
    accountKey: 'videogen.apiyi_seedance2.account',
    apiKeyKey: 'videogen.apiyi_seedance2.api_key',
    baseURLKey: 'videogen.apiyi_seedance2.base_url',
    modelKey: 'videogen.apiyi_seedance2.model',
  },
  {
    type: 'apiyi_wan27',
    name: 'API易 Wan2.7',
    desc: '使用 API易 Wan2.7 DashScope 异步任务接口，余额查询不支持',
    accountKey: 'videogen.apiyi_wan27.account',
    apiKeyKey: 'videogen.apiyi_wan27.api_key',
    baseURLKey: 'videogen.apiyi_wan27.base_url',
    modelKey: 'videogen.apiyi_wan27.model',
  },
  {
    type: 'apiyi_happyhorse',
    name: 'API易 HappyHorse',
    desc: '使用 API易 HappyHorse DashScope 异步任务接口，余额查询不支持',
    accountKey: 'videogen.apiyi_happyhorse.account',
    apiKeyKey: 'videogen.apiyi_happyhorse.api_key',
    baseURLKey: 'videogen.apiyi_happyhorse.base_url',
    modelKey: 'videogen.apiyi_happyhorse.model',
  },
]

const commonVideoSettings = computed(() => (
  grouped.value.videogen || []
).filter((it) => videoSettingSection(it) === 'common'))

function formatNumber(n: number | undefined | null) {
  return Number(n || 0).toLocaleString('zh-CN')
}

async function doTestImageGen() {
  if (dirtyCount.value > 0) {
    await ElMessageBox.confirm('当前有未保存修改。是否先保存后再探测?', '确认', {
      type: 'warning',
    })
    await save()
  }
  imageGenTesting.value = true
  try {
    const res = await testImageGen()
    ElMessage.success(`探测成功: ${res.image_count} 张图, ${res.duration_ms}ms`)
  } catch {
    // 拦截器已处理
  } finally {
    imageGenTesting.value = false
  }
}

async function doTestTextGen() {
  if (dirtyCount.value > 0) {
    await ElMessageBox.confirm('当前有未保存修改。是否先保存后再探测?', '确认', {
      type: 'warning',
    })
    await save()
  }
  textGenTesting.value = true
  try {
    const res = await testTextGen()
    ElMessage.success(`探测成功: ${res.content || 'OK'}, ${res.duration_ms}ms`)
  } catch {
    // 拦截器已处理
  } finally {
    textGenTesting.value = false
  }
}

async function saveIfDirty() {
  if (dirtyCount.value > 0) await save()
}

function isChannelEnabled(row: VideoChannelRow) {
  return draft['videogen.enabled'] === 'true' && draft['videogen.channel_type'] === row.type
}

function channelBalanceText(row: VideoChannelRow) {
  const balance = videoChannelBalances[row.type]
  if (!balance) return '未刷新'
  if (balance.supported === false) return balance.message || '不支持余额查询'
  return `积分 ${formatNumber(balance.credits)} / 免费 ${formatNumber((balance.free_quotas || []).reduce((sum, quota) => sum + Number(quota.remaining_count || 0), 0))}`
}

function channelModelOptions(row: VideoChannelRow) {
  if (row.type === 'apiyi_seedance2') return apiyiModelOptions(draft[row.modelKey])
  if (row.type === 'apiyi_wan27') return apiyiWan27ModelOptions(draft[row.modelKey])
  if (row.type === 'apiyi_happyhorse') return apiyiHappyHorseModelOptions(draft[row.modelKey])
  return videoModelOptionsForRow(row, draft[row.modelKey])
}

function videoModelOptionsForRow(row: VideoChannelRow, current: string) {
  const opts = [...(videoChannelModels[row.type] || [])]
  const value = String(current || '').trim()
  if (value && !opts.some((item) => item.value === value || item.id === value || item.name === value)) {
    opts.unshift({ id: value, name: value, type: '', label: `${value}（当前配置）`, value })
  }
  return opts
}

async function toggleVideoChannel(row: VideoChannelRow, enabled: boolean) {
  if (enabled) {
    draft['videogen.enabled'] = 'true'
    draft['videogen.channel_type'] = row.type
    ensureVideoChannelDefaults(row)
  } else if (draft['videogen.channel_type'] === row.type) {
    draft['videogen.enabled'] = 'false'
  }
}

function ensureVideoChannelDefaults(row: VideoChannelRow) {
  if (row.type === 'apiyi_seedance2') {
    if (shouldReplaceWithAPIYIDefault(draft[row.baseURLKey], echoonDefaultBaseURL)) draft[row.baseURLKey] = apiyiDefaultBaseURL
    if (shouldReplaceWithAPIYIDefault(draft[row.modelKey], echoonDefaultModel)) draft[row.modelKey] = apiyiDefaultModel
    videoChannelModels[row.type] = normalizeVideoModels(apiyiBuiltInModels)
  } else if (row.type === 'apiyi_wan27') {
    if (shouldReplaceWithAPIYIDefault(draft[row.baseURLKey], echoonDefaultBaseURL)) draft[row.baseURLKey] = apiyiDefaultBaseURL
    if (shouldReplaceWithAPIYIDefault(draft[row.modelKey], echoonDefaultModel)) draft[row.modelKey] = apiyiWan27DefaultModel
    videoChannelModels[row.type] = normalizeVideoModels(apiyiWan27BuiltInModels)
  } else if (row.type === 'apiyi_happyhorse') {
    if (shouldReplaceWithAPIYIDefault(draft[row.baseURLKey], echoonDefaultBaseURL)) draft[row.baseURLKey] = apiyiDefaultBaseURL
    if (shouldReplaceWithAPIYIDefault(draft[row.modelKey], echoonDefaultModel)) draft[row.modelKey] = apiyiHappyHorseDefaultModel
    videoChannelModels[row.type] = normalizeVideoModels(apiyiHappyHorseBuiltInModels)
  } else {
    if (!String(draft[row.baseURLKey] || '').trim()) draft[row.baseURLKey] = echoonDefaultBaseURL
    if (!String(draft[row.modelKey] || '').trim()) draft[row.modelKey] = echoonDefaultModel
  }
}

function shouldReplaceWithAPIYIDefault(value: string | undefined, echoonDefault: string) {
  const text = String(value || '').trim()
  return !text || text === echoonDefault
}

async function probeVideoChannel(row: VideoChannelRow) {
  await saveIfDirty()
  videoChannelTesting[row.type] = true
  try {
    const res = await testVideoGen(row.type)
    const models = normalizeVideoModels(res.models || [])
    videoChannelModels[row.type] = models
    const current = String(draft[row.modelKey] || '').trim()
    const matched = models.find((model) => model.value === current || model.name === current)
    if (matched) {
      draft[row.modelKey] = matched.value
    } else if (!current && models[0]) {
      draft[row.modelKey] = models[0].value
    }
    ElMessage.success(`${row.name} 探测成功：${res.model_name || '已连接'}，可选模型 ${models.length} 个`)
  } catch {
    // 拦截器已处理
  } finally {
    videoChannelTesting[row.type] = false
  }
}

async function refreshVideoChannelBalance(row: VideoChannelRow, silent = false) {
  videoChannelBalanceLoading[row.type] = true
  try {
    const balance = await fetchVideoGenBalance(silent, row.type)
    videoChannelBalances[row.type] = balance
    if (!silent) {
      if (balance.supported === false) {
        ElMessage.info(balance.message || '当前渠道不支持余额查询')
      } else {
        ElMessage.success(`${row.name} 余额已刷新`)
      }
    }
  } catch {
    videoChannelBalances[row.type] = null
  } finally {
    videoChannelBalanceLoading[row.type] = false
  }
}

function openVideoGenerateTest(row: VideoChannelRow) {
  videoGenerateRow.value = row
  videoGenerateDlg.value = true
  ensureVideoChannelDefaults(row)
}

function onVideoGenerateImageChange(uploadFile: any) {
  const raw = uploadFile?.raw as File | undefined
  if (!raw) return
  videoGenerateImage.value = raw
  if (videoGeneratePreview.value) URL.revokeObjectURL(videoGeneratePreview.value)
  videoGeneratePreview.value = URL.createObjectURL(raw)
}

function clearVideoGenerateImage() {
  videoGenerateImage.value = null
  if (videoGeneratePreview.value) URL.revokeObjectURL(videoGeneratePreview.value)
  videoGeneratePreview.value = ''
}

function onVideoGenerateVideoChange(uploadFile: any) {
  const raw = uploadFile?.raw as File | undefined
  if (!raw) return
  videoGenerateVideo.value = raw
  if (videoGenerateVideoPreview.value) URL.revokeObjectURL(videoGenerateVideoPreview.value)
  videoGenerateVideoPreview.value = URL.createObjectURL(raw)
}

function clearVideoGenerateVideo() {
  videoGenerateVideo.value = null
  if (videoGenerateVideoPreview.value) URL.revokeObjectURL(videoGenerateVideoPreview.value)
  videoGenerateVideoPreview.value = ''
}

function videoGenerateSupportsReferenceVideo(row: VideoChannelRow | null) {
  return row?.type === 'apiyi_wan27' || row?.type === 'apiyi_happyhorse'
}

async function submitVideoGenerateTest() {
  const row = videoGenerateRow.value
  if (!row) return
  const prompt = videoGeneratePrompt.value.trim()
  if (!prompt) {
    ElMessage.warning('请输入测试提示词')
    return
  }
  if (videoGenerateMode.value === 'image' && !videoGenerateImage.value) {
    ElMessage.warning('请上传参考图')
    return
  }
  if (videoGenerateMode.value === 'video' && !videoGenerateVideo.value) {
    ElMessage.warning('请上传参考视频')
    return
  }
  await saveIfDirty()
  videoChannelGenerating[row.type] = true
  try {
    const state = await startVideoGenGenerateTest({
      channelType: row.type,
      prompt,
      model: String(draft[row.modelKey] || '').trim(),
      image: videoGenerateMode.value === 'image' ? videoGenerateImage.value : null,
      video: videoGenerateMode.value === 'video' ? videoGenerateVideo.value : null,
    })
    videoGenerateTestStates[row.type] = state
    ElMessage.success(`${row.name} 已开始生成测试`)
    pollVideoGenerateTest(row.type, state.id)
    videoGenerateDlg.value = false
  } catch {
    videoChannelGenerating[row.type] = false
  }
}

function pollVideoGenerateTest(channelType: string, id: string) {
  if (videoGeneratePolling[channelType]) window.clearTimeout(videoGeneratePolling[channelType])
  const tick = async () => {
    try {
      const state = await getVideoGenGenerateTest(id)
      videoGenerateTestStates[channelType] = state
      const done = ['completed', 'failed'].includes(String(state.status || '').toLowerCase())
      if (done) {
        videoChannelGenerating[channelType] = false
        if (state.status === 'completed') {
          ElMessage.success('视频测试生成完成')
        } else if (state.error) {
          ElMessage.error(state.error)
        }
        return
      }
      videoGeneratePolling[channelType] = window.setTimeout(tick, 3000)
    } catch {
      videoChannelGenerating[channelType] = false
    }
  }
  videoGeneratePolling[channelType] = window.setTimeout(tick, 1200)
}

function videoGenerateStatusText(row: VideoChannelRow) {
  const state = videoGenerateTestStates[row.type]
  if (!state) return '未测试'
  const progress = Number(state.progress || 0)
  if (state.status === 'completed') return `测试完成 ${progress || 100}%`
  if (state.status === 'failed') return `测试失败`
  return `测试中 ${progress}%`
}

function videoGenerateProgress(row: VideoChannelRow) {
  const state = videoGenerateTestStates[row.type]
  if (!state) return 0
  if (state.status === 'completed') return 100
  return Number(state.progress || 0)
}

function videoGenerateProgressStatus(row: VideoChannelRow) {
  const status = String(videoGenerateTestStates[row.type]?.status || '').toLowerCase()
  if (status === 'completed') return 'success'
  if (status === 'failed') return 'exception'
  return undefined
}

function isVideoGenerateTesting(row: VideoChannelRow) {
  const status = String(videoGenerateTestStates[row.type]?.status || '').toLowerCase()
  return videoChannelGenerating[row.type] || status === 'queued' || status === 'running'
}

function videoGenerateTaskMeta(row: VideoChannelRow) {
  const state = videoGenerateTestStates[row.type]
  if (!state) return ''
  const parts = []
  if (state.task_id) parts.push(`任务 ${state.task_id}`)
  if (state.duration_ms) parts.push(`${Math.round(state.duration_ms / 1000)}s`)
  return parts.join(' / ')
}

function currentVideoGenerateModelOptions() {
  const row = videoGenerateRow.value
  if (!row) return []
  return channelModelOptions(row)
}

function currentVideoGenerateModelKey() {
  return videoGenerateRow.value?.modelKey || ''
}

watch(videoGenerateMode, (mode) => {
  if (mode !== 'image') clearVideoGenerateImage()
  if (mode !== 'video') clearVideoGenerateVideo()
})

function normalizeVideoModels(models: VideoGenProbeModel[]) {
  const seen = new Set<string>()
  const out: VideoGenProbeModel[] = []
  for (const item of models) {
    const id = String(item.id || '').trim()
    const name = String(item.name || '').trim()
    const value = String(id || item.value || name).trim()
    if (!value || seen.has(value)) continue
    seen.add(value)
    out.push({
      id,
      name: String(name || value),
      type: String(item.type || ''),
      label: String(item.label || name || value),
      value,
    })
  }
  return out
}

function apiyiModelOptions(current: string) {
  const builtIns = normalizeVideoModels(apiyiBuiltInModels)
  const value = String(current || '').trim()
  if (value && !builtIns.some((item) => item.value === value || item.id === value || item.name === value)) {
    builtIns.unshift({ id: value, name: value, type: '', label: `${value}（当前配置）`, value })
  }
  return builtIns
}

function apiyiWan27ModelOptions(current: string) {
  const builtIns = normalizeVideoModels(apiyiWan27BuiltInModels)
  const value = String(current || '').trim()
  if (value && !builtIns.some((item) => item.value === value || item.id === value || item.name === value)) {
    builtIns.unshift({ id: value, name: value, type: '', label: `${value}（当前配置）`, value })
  }
  return builtIns
}

function apiyiHappyHorseModelOptions(current: string) {
  const builtIns = normalizeVideoModels(apiyiHappyHorseBuiltInModels)
  const value = String(current || '').trim()
  if (value && !builtIns.some((item) => item.value === value || item.id === value || item.name === value)) {
    builtIns.unshift({ id: value, name: value, type: '', label: `${value}（当前配置）`, value })
  }
  return builtIns
}

function videoSettingSection(it: SettingItem) {
  if (it.key === 'videogen.channel_type' || it.key === 'videogen.enabled') return 'current'
  if (it.key.startsWith('videogen.apiyi_seedance2.')) return 'apiyi'
  if (it.key.startsWith('videogen.apiyi_wan27.')) return 'apiyi'
  if (it.key.startsWith('videogen.apiyi_happyhorse.')) return 'apiyi'
  if (['videogen.account', 'videogen.api_key', 'videogen.base_url', 'videogen.model'].includes(it.key)) return 'echoon'
  return 'common'
}

async function onSiteAssetChange(it: SettingItem, uploadFile: any) {
  const raw = uploadFile?.raw as File | undefined
  if (!raw) return
  try {
    const res = await uploadSiteAsset(it.key, raw)
    draft[it.key] = res.url
    ElMessage.success(`${siteAssetName(it)}上传成功`)
  } catch {
    // 错误由拦截器处理
  }
}

async function save() {
  const diff: Record<string, string> = {}
  for (const it of items.value) {
    const v = draft[it.key] ?? ''
    if (String(v) !== String(it.value)) diff[it.key] = String(v)
  }
  if (Object.keys(diff).length === 0) {
    ElMessage.info('没有需要保存的修改')
    return
  }
  saving.value = true
  try {
    await updateSettings(diff)
    ElMessage.success(`已保存 ${Object.keys(diff).length} 项`)
    await load()
    useSiteStore().refresh()
  } finally {
    saving.value = false
  }
}

async function doReload() {
  await ElMessageBox.confirm('从数据库强制重载最新值到内存缓存?', '确认', {
    type: 'warning',
  }).catch(() => 'cancel')
  try {
    await reloadSettings()
    ElMessage.success('已重载')
    await load()
  } catch { /* 拦截器已处理 */ }
}

// ---- 邮件测试 ----
const mailDlg = ref(false)
const mailTo = ref('')
const mailSending = ref(false)
async function submitTestMail() {
  if (!mailTo.value) {
    ElMessage.warning('请输入收件邮箱')
    return
  }
  mailSending.value = true
  try {
    await sendTestEmail(mailTo.value)
    ElMessage.success('测试邮件已发出')
    mailDlg.value = false
  } catch { /* 拦截器已处理 */ } finally {
    mailSending.value = false
  }
}

onMounted(async () => {
  await load()
})

onUnmounted(() => {
  Object.values(videoGeneratePolling).forEach((timer) => window.clearTimeout(timer))
  if (videoGeneratePreview.value) URL.revokeObjectURL(videoGeneratePreview.value)
  if (videoGenerateVideoPreview.value) URL.revokeObjectURL(videoGenerateVideoPreview.value)
})
</script>

<template>
  <div class="page-container">
    <div class="card-block" v-loading="loading">
      <!-- 顶部:标题 + 操作栏(始终可见) -->
      <div class="flex-between settings-head">
        <div>
          <div class="page-title" style="margin:0">系统设置</div>
          <div class="settings-subtitle">
            所有修改在点击"保存修改"后立即生效,无需重启服务
          </div>
        </div>
        <div class="flex-wrap-gap">
          <el-button :icon="Refresh" @click="doReload">强制重载</el-button>
          <el-button :icon="MailIcon" @click="mailDlg = true">发测试邮件</el-button>
          <el-button
            v-if="activeTab === 'imagegen'"
            :icon="Picture"
            :loading="imageGenTesting"
            @click="doTestImageGen"
          >探测生图</el-button>
          <el-button
            v-if="activeTab === 'textgen'"
            :icon="ChatDotRound"
            :loading="textGenTesting"
            @click="doTestTextGen"
          >探测文本</el-button>
          <el-button :disabled="dirtyCount === 0" @click="reset">重置</el-button>
          <el-button
            type="primary"
            :icon="Check"
            :loading="saving"
            @click="save"
          >
            保存修改<span v-if="dirtyCount > 0"> ({{ dirtyCount }})</span>
          </el-button>
        </div>
      </div>

      <el-tabs v-model="activeTab" class="settings-tabs">
        <el-tab-pane v-for="t in tabs" :key="t.name" :name="t.name">
          <template #label>
            <span class="tab-label">
              <el-icon><component :is="t.icon" /></el-icon>
              <span>{{ t.label }}</span>
            </span>
          </template>

          <div class="tab-body">
            <el-empty
              v-if="!grouped[t.name] || grouped[t.name].length === 0"
              description="暂无可配置项"
            />
            <template v-else>
              <template v-if="t.name === 'videogen'">
                <el-table
                  :data="videoChannelRows"
                  border
                  row-key="type"
                  class="video-channel-table"
                >
                  <el-table-column type="expand">
                    <template #default="{ row }">
                      <el-form label-width="140px" label-position="right" class="channel-config-form">
                        <el-form-item label="账号">
                          <el-input v-model="draft[row.accountKey]" clearable style="max-width: 520px" />
                        </el-form-item>
                        <el-form-item label="密钥">
                          <el-input
                            v-model="draft[row.apiKeyKey]"
                            type="password"
                            show-password
                            clearable
                            autocomplete="new-password"
                            style="max-width: 520px"
                          />
                        </el-form-item>
                        <el-form-item label="Base URL">
                          <el-input v-model="draft[row.baseURLKey]" clearable style="max-width: 520px" />
                        </el-form-item>
                        <el-form-item label="默认视频模型">
                          <el-select
                            v-model="draft[row.modelKey]"
                            filterable
                            allow-create
                            default-first-option
                            clearable
                            placeholder="探测后可选择模型，也可手动输入"
                            style="max-width: 520px; width: 100%"
                          >
                            <el-option
                              v-for="model in channelModelOptions(row)"
                              :key="model.value"
                              :label="model.label"
                              :value="model.value"
                            >
                              <div class="model-option">
                                <span>{{ model.name || model.value }}</span>
                                <small v-if="model.type">{{ model.type }}</small>
                              </div>
                            </el-option>
                          </el-select>
                        </el-form-item>
                      </el-form>
                    </template>
                  </el-table-column>
                  <el-table-column label="状态" width="100">
                    <template #default="{ row }">
                      <el-switch
                        :model-value="isChannelEnabled(row)"
                        @update:model-value="(v) => toggleVideoChannel(row, Boolean(v))"
                      />
                    </template>
                  </el-table-column>
                  <el-table-column label="渠道">
                    <template #default="{ row }">
                      <div class="channel-name">
                        <strong>{{ row.name }}</strong>
                        <span>{{ row.desc }}</span>
                      </div>
                    </template>
                  </el-table-column>
                  <el-table-column label="当前模型" min-width="220">
                    <template #default="{ row }">
                      <span class="mono-cell">{{ draft[row.modelKey] || '-' }}</span>
                    </template>
                  </el-table-column>
                  <el-table-column label="余额" min-width="220">
                    <template #default="{ row }">
                      <span>{{ channelBalanceText(row) }}</span>
                    </template>
                  </el-table-column>
                  <el-table-column label="生成测试" min-width="260">
                    <template #default="{ row }">
                      <div class="generate-test-cell">
                        <div class="generate-test-main">
                          <span>{{ videoGenerateStatusText(row) }}</span>
                          <el-link
                            v-if="videoGenerateTestStates[row.type]?.result_url"
                            type="primary"
                            :href="videoGenerateTestStates[row.type]?.result_url"
                            target="_blank"
                          >查看结果</el-link>
                        </div>
                        <el-progress
                          v-if="videoGenerateTestStates[row.type]"
                          :percentage="videoGenerateProgress(row)"
                          :status="videoGenerateProgressStatus(row)"
                          :stroke-width="6"
                        />
                        <div
                          v-if="videoGenerateTestStates[row.type]?.error"
                          class="generate-test-error"
                        >
                          {{ videoGenerateTestStates[row.type]?.error }}
                        </div>
                        <div v-else-if="videoGenerateTaskMeta(row)" class="generate-test-meta">
                          {{ videoGenerateTaskMeta(row) }}
                        </div>
                      </div>
                    </template>
                  </el-table-column>
                  <el-table-column label="操作" width="320" fixed="right">
                    <template #default="{ row }">
                      <el-button
                        size="small"
                        :icon="VideoPlay"
                        :loading="videoChannelTesting[row.type]"
                        @click="probeVideoChannel(row)"
                      >探测</el-button>
                      <el-button
                        size="small"
                        :icon="Refresh"
                        :loading="videoChannelBalanceLoading[row.type]"
                        @click="refreshVideoChannelBalance(row)"
                      >余额</el-button>
                      <el-button
                        size="small"
                        type="primary"
                        plain
                        :icon="Upload"
                        :loading="isVideoGenerateTesting(row)"
                        @click="openVideoGenerateTest(row)"
                      >生成测试</el-button>
                    </template>
                  </el-table-column>
                </el-table>

                <div class="settings-section-title settings-section-title--table">通用生成与计费</div>
                <el-form label-width="170px" label-position="right" class="setting-form">
                  <el-form-item
                    v-for="it in commonVideoSettings"
                    :key="it.key"
                    :label="it.label || it.key"
                  >
                    <div class="field-wrap">
                      <el-switch
                        v-if="isBool(it)"
                        :model-value="draft[it.key] === 'true'"
                        @update:model-value="(v) => (draft[it.key] = v ? 'true' : 'false')"
                      />
                      <el-input-number
                        v-else-if="isInt(it)"
                        :model-value="Number(draft[it.key] || 0)"
                        :min="0"
                        :controls-position="'right'"
                        style="width: 240px"
                        @update:model-value="(v) => (draft[it.key] = String(v ?? 0))"
                      />
                      <el-input-number
                        v-else-if="isFloat(it)"
                        :model-value="Number(draft[it.key] || 0)"
                        :min="floatInputProps(it).min"
                        :max="floatInputProps(it).max"
                        :step="floatInputProps(it).step"
                        :precision="floatInputProps(it).precision"
                        :controls-position="'right'"
                        style="width: 240px"
                        @update:model-value="(v) => (draft[it.key] = String(v ?? 0))"
                      />
                      <el-input
                        v-else
                        v-model="draft[it.key]"
                        :placeholder="it.desc || it.label"
                        :type="inputType(it)"
                        clearable
                        style="max-width: 520px"
                      />
                      <div v-if="it.desc" class="hint">{{ it.desc }}</div>
                    </div>
                  </el-form-item>
                </el-form>
              </template>
              <el-form
                v-else
                label-width="170px"
                label-position="right"
                class="setting-form"
              >
              <el-form-item
                v-for="it in grouped[t.name]"
                :key="it.key"
                :label="it.label || it.key"
              >
                <div class="field-wrap">
                  <el-switch
                    v-if="isBool(it)"
                    :model-value="draft[it.key] === 'true'"
                    @update:model-value="(v) => (draft[it.key] = v ? 'true' : 'false')"
                  />
                  <el-input-number
                    v-else-if="isInt(it)"
                    :model-value="Number(draft[it.key] || 0)"
                    :min="0"
                    :controls-position="'right'"
                    style="width: 240px"
                    @update:model-value="(v) => (draft[it.key] = String(v ?? 0))"
                  />
                  <el-input-number
                    v-else-if="isFloat(it)"
                    :model-value="Number(draft[it.key] || 0)"
                    :min="floatInputProps(it).min"
                    :max="floatInputProps(it).max"
                    :step="floatInputProps(it).step"
                    :precision="floatInputProps(it).precision"
                    :controls-position="'right'"
                    style="width: 240px"
                    @update:model-value="(v) => (draft[it.key] = String(v ?? 0))"
                  />
                  <div v-else-if="isSiteAsset(it)" class="asset-upload">
                    <div :class="['asset-preview', { 'asset-preview--logo': isLogo(it) }]">
                      <img v-if="draft[it.key]" :src="draft[it.key]" :alt="siteAssetName(it)" />
                      <div v-else class="asset-preview__empty">暂无{{ siteAssetName(it) }}</div>
                    </div>
                    <div class="asset-actions">
                      <el-upload
                        :auto-upload="false"
                        :show-file-list="false"
                        accept=".ico,.png,.jpg,.jpeg,.svg,image/*"
                        :on-change="(file) => onSiteAssetChange(it, file)"
                      >
                        <template #trigger>
                          <el-button type="primary" plain>上传{{ siteAssetName(it) }}</el-button>
                        </template>
                      </el-upload>
                      <el-button v-if="draft[it.key]" @click="draft[it.key] = ''">清空</el-button>
                      <div class="hint">上传后保存到服务器本地静态目录，并自动写回当前设置值。</div>
                    </div>
                  </div>
                  <el-input
                    v-else-if="isPassword(it)"
                    v-model="draft[it.key]"
                    :placeholder="it.desc || it.label"
                    type="password"
                    show-password
                    clearable
                    autocomplete="new-password"
                    style="max-width: 520px"
                  />
                  <el-input
                    v-else
                    v-model="draft[it.key]"
                    :placeholder="it.desc || it.label"
                    :type="inputType(it)"
                    clearable
                    style="max-width: 520px"
                  />
                  <div v-if="it.desc" class="hint">{{ it.desc }}</div>
                </div>
              </el-form-item>
              </el-form>
            </template>
          </div>
        </el-tab-pane>
      </el-tabs>
    </div>

    <!-- 测试邮件 -->
    <el-dialog v-model="mailDlg" title="发送 SMTP 测试邮件" width="420px">
      <el-form label-width="80px">
        <el-form-item label="收件人">
          <el-input v-model="mailTo" placeholder="your@mail.com" type="email" clearable />
        </el-form-item>
        <div style="font-size:12px;color:var(--el-text-color-secondary)">
          使用 <code>configs/config.yaml</code> 的 SMTP 配置发送;未配置时会直接失败。
        </div>
      </el-form>
      <template #footer>
        <el-button @click="mailDlg = false">取消</el-button>
        <el-button type="primary" :loading="mailSending" @click="submitTestMail">发送</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="videoGenerateDlg" title="视频生成测试" width="560px" @closed="clearVideoGenerateImage">
      <el-form label-width="88px" class="video-generate-form">
        <el-form-item label="渠道">
          <div class="dialog-channel-name">{{ videoGenerateRow?.name || '-' }}</div>
        </el-form-item>
        <el-form-item label="模型">
          <el-select
            v-if="currentVideoGenerateModelKey()"
            v-model="draft[currentVideoGenerateModelKey()]"
            filterable
            allow-create
            default-first-option
            clearable
            style="width: 100%"
            placeholder="选择或输入测试模型"
          >
            <el-option
              v-for="model in currentVideoGenerateModelOptions()"
              :key="model.value"
              :label="model.label"
              :value="model.value"
            />
          </el-select>
        </el-form-item>
        <el-form-item label="方式">
          <el-radio-group v-model="videoGenerateMode" size="small">
            <el-radio-button label="text">文生视频</el-radio-button>
            <el-radio-button label="image">图生视频</el-radio-button>
            <el-radio-button label="video" :disabled="!videoGenerateSupportsReferenceVideo(videoGenerateRow)">参考视频</el-radio-button>
          </el-radio-group>
          <div v-if="!videoGenerateSupportsReferenceVideo(videoGenerateRow)" class="hint">
            参考视频仅支持 API易 Wan2.7 / HappyHorse。
          </div>
        </el-form-item>
        <el-form-item label="提示词">
          <el-input
            v-model="videoGeneratePrompt"
            type="textarea"
            :rows="4"
            maxlength="1000"
            show-word-limit
            placeholder="输入用于真实生成测试的视频提示词"
          />
        </el-form-item>
        <el-form-item v-if="videoGenerateMode === 'image'" label="参考图">
          <div class="video-test-upload">
            <el-upload
              :auto-upload="false"
              :show-file-list="false"
              accept="image/*"
              :on-change="onVideoGenerateImageChange"
            >
              <template #trigger>
                <el-button :icon="Upload">上传图片</el-button>
              </template>
            </el-upload>
            <el-button
              v-if="videoGenerateImage"
              :icon="Delete"
              @click="clearVideoGenerateImage"
            >清空</el-button>
            <span class="hint">不上传图片时按文生视频测试；上传后按图生/参考图链路测试。</span>
            <div v-if="videoGeneratePreview" class="video-test-preview">
              <img :src="videoGeneratePreview" alt="视频生成测试参考图" />
            </div>
          </div>
        </el-form-item>
        <el-form-item v-if="videoGenerateMode === 'video'" label="参考视频">
          <div class="video-test-upload">
            <el-upload
              :auto-upload="false"
              :show-file-list="false"
              accept="video/mp4,video/quicktime,video/webm,video/*"
              :on-change="onVideoGenerateVideoChange"
            >
              <template #trigger>
                <el-button :icon="Upload">上传视频</el-button>
              </template>
            </el-upload>
            <el-button
              v-if="videoGenerateVideo"
              :icon="Delete"
              @click="clearVideoGenerateVideo"
            >清空</el-button>
            <span class="hint">用于 Wan2.7 / HappyHorse 参考视频链路，最大 200MB。</span>
            <div v-if="videoGenerateVideoPreview" class="video-test-preview video-test-preview--video">
              <video :src="videoGenerateVideoPreview" controls muted playsinline />
            </div>
          </div>
        </el-form-item>
        <div class="video-test-warning">
          该操作会调用真实上游生成任务，可能消耗渠道额度；创建后可在列表行查看进度和结果。
        </div>
      </el-form>
      <template #footer>
        <el-button @click="videoGenerateDlg = false">取消</el-button>
        <el-button
          type="primary"
          :loading="videoGenerateRow ? videoChannelGenerating[videoGenerateRow.type] : false"
          @click="submitVideoGenerateTest"
        >开始生成</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<style scoped>
.settings-head {
  margin-bottom: 4px;
}
.settings-subtitle {
  font-size: 12px;
  color: var(--el-text-color-secondary);
  margin-top: 4px;
}

.settings-tabs {
  margin-top: 8px;
}
.settings-tabs :deep(.el-tabs__header) {
  margin-bottom: 16px;
}
.tab-label {
  display: inline-flex;
  align-items: center;
  gap: 6px;
}

.tab-body {
  padding-top: 4px;
}
.setting-form .el-form-item {
  margin-bottom: 18px;
}
.settings-section-title {
  margin: 22px 0 12px 170px;
  max-width: 920px;
  padding: 8px 12px;
  border-left: 3px solid var(--el-color-primary);
  background: var(--el-fill-color-extra-light);
  color: var(--el-text-color-primary);
  font-size: 13px;
  font-weight: 700;
}
.field-wrap {
  width: 100%;
}
.video-channel-table {
  max-width: 1180px;
  margin-bottom: 18px;
}
.channel-config-form {
  padding: 16px 18px 2px 8px;
  background: var(--el-fill-color-extra-light);
}
.channel-config-form .el-form-item {
  margin-bottom: 14px;
}
.channel-name {
  display: flex;
  flex-direction: column;
  gap: 4px;
}
.channel-name strong {
  font-size: 14px;
  color: var(--el-text-color-primary);
}
.channel-name span {
  font-size: 12px;
  color: var(--el-text-color-secondary);
  line-height: 1.4;
}
.mono-cell {
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, "Liberation Mono", monospace;
  font-size: 12px;
  overflow-wrap: anywhere;
}
.gateway-balance {
  max-width: 920px;
  margin: 0 0 18px 170px;
  padding: 14px 16px;
  border: 1px solid var(--el-border-color-lighter);
  border-radius: 8px;
  background: var(--el-fill-color-extra-light);
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
}
.balance-main {
  display: grid;
  grid-template-columns: repeat(3, minmax(96px, 1fr));
  gap: 12px;
  min-width: 360px;
}
.balance-main div {
  display: flex;
  flex-direction: column;
  gap: 4px;
}
.balance-main span,
.balance-side small,
.balance-empty {
  font-size: 12px;
  color: var(--el-text-color-secondary);
}
.balance-main strong {
  font-size: 18px;
  line-height: 1.25;
  color: var(--el-text-color-primary);
  font-weight: 700;
}
.balance-side {
  min-width: 0;
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 8px;
  flex-wrap: wrap;
}
.hint {
  margin-top: 4px;
  font-size: 12px;
  color: var(--el-text-color-secondary);
  line-height: 1.5;
}
.asset-upload {
  display: flex;
  align-items: center;
  gap: 14px;
  flex-wrap: wrap;
}
.asset-preview {
  width: 56px;
  height: 56px;
  border-radius: 12px;
  border: 1px solid var(--el-border-color);
  background: var(--el-fill-color-light);
  display: flex;
  align-items: center;
  justify-content: center;
  overflow: hidden;
}
.asset-preview--logo {
  width: 156px;
  height: 56px;
}
.asset-preview img {
  width: 100%;
  height: 100%;
  object-fit: contain;
}
.asset-preview__empty {
  font-size: 12px;
  color: var(--el-text-color-secondary);
}
.asset-actions {
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.model-option {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}
.model-option small {
  color: var(--el-text-color-secondary);
}
.generate-test-cell {
  display: flex;
  flex-direction: column;
  gap: 6px;
  min-width: 0;
}
.generate-test-main {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  font-size: 12px;
}
.generate-test-error {
  color: var(--el-color-danger);
  font-size: 12px;
  line-height: 1.4;
  overflow-wrap: anywhere;
}
.generate-test-meta {
  color: var(--el-text-color-secondary);
  font-size: 12px;
  line-height: 1.4;
  overflow-wrap: anywhere;
}
.video-generate-form {
  padding-top: 4px;
}
.dialog-channel-name {
  color: var(--el-text-color-primary);
  font-weight: 600;
}
.video-test-upload {
  width: 100%;
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
}
.video-test-upload .hint {
  margin-top: 0;
}
.video-test-preview {
  width: 100%;
  max-width: 240px;
  aspect-ratio: 16 / 9;
  border: 1px solid var(--el-border-color);
  border-radius: 8px;
  background: var(--el-fill-color-light);
  overflow: hidden;
}
.video-test-preview img {
  width: 100%;
  height: 100%;
  object-fit: contain;
  display: block;
}
.video-test-preview--video video {
  width: 100%;
  height: 100%;
  object-fit: contain;
  display: block;
  background: #000;
}
.video-test-warning {
  margin-left: 88px;
  padding: 10px 12px;
  border-radius: 8px;
  background: var(--el-color-warning-light-9);
  color: var(--el-text-color-regular);
  font-size: 12px;
  line-height: 1.5;
}

@media (max-width: 640px) {
  .gateway-balance {
    margin-left: 0;
    flex-direction: column;
    align-items: stretch;
  }
  .balance-main {
    min-width: 0;
    grid-template-columns: 1fr;
  }
  .balance-side {
    justify-content: flex-start;
  }
  .settings-section-title {
    margin-left: 0;
  }
  .video-test-warning {
    margin-left: 0;
  }
  .setting-form :deep(.el-form-item__label) {
    width: auto !important;
    padding-right: 8px !important;
    line-height: 1.5;
  }
}
</style>
