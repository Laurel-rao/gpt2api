<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { Clock } from '@element-plus/icons-vue'
import { storeToRefs } from 'pinia'
import { useUserStore } from '@/stores/user'
import { formatCredit, formatDateShort } from '@/utils/format'
import {
  listMyModels,
  listMyImageTasks,
  streamPlayChat,
  playGenerateImage,
  listPlayVideoChannels,
  startPlayVideo,
  getPlayVideo,
  type ImageTask,
  type PlayChatMessage,
  type PlayImageData,
  type PlayVideoChannel,
  type PlayVideoState,
} from '@/api/me'
import { ENABLE_CHAT_MODEL } from '@/config/feature'

// ----------------------------------------------------
// 用户 / 默认生成能力
// ----------------------------------------------------
const userStore = useUserStore()
const { user } = storeToRefs(userStore)

const balance = computed(() => formatCredit(user.value?.credit_balance))

const selectedChatModel = ref('')
const selectedImageModel = ref('')

onMounted(async () => {
  loadVideoHistory()
  try {
    await userStore.fetchMe()
  } catch {
    /* ignore */
  }
  try {
    const { items } = await listMyModels()
    const firstChat = items.find((x) => x.type === 'chat')
    const firstImage = items.find((x) => x.type === 'image')
    if (firstChat) selectedChatModel.value = firstChat.slug
    if (firstImage) selectedImageModel.value = firstImage.slug
  } catch {
    // 静默;错误拦截器已提示
  }
  loadVideoChannels()
  loadImageHistory()
})

// ----------------------------------------------------
// Tabs
// ----------------------------------------------------
const activeTab = ref<'chat' | 'text2img' | 'img2img' | 'video'>(
  ENABLE_CHAT_MODEL ? 'chat' : 'text2img',
)

// ====================================================
// 对话(Chat)
// ====================================================
interface UIMessage {
  id: number
  role: 'user' | 'assistant' | 'system'
  content: string
  pending?: boolean
  error?: boolean
  at: number
}

let uid = 0

const systemPrompt = ref('你是一个友好、博学、回答精准的中文助手。回答中若涉及代码请使用 Markdown 代码块。')
const temperature = ref(0.7)
const chatInput = ref('')
const chatMsgs = ref<UIMessage[]>([])
const chatSending = ref(false)
const chatAbort = ref<AbortController | null>(null)
const chatScroll = ref<HTMLElement | null>(null)
const inputRef = ref<any>(null)

const suggestions = [
  { icon: '💡', title: '向我解释', sub: '量子纠缠到底是什么?' },
  { icon: '✍️', title: '帮我写作', sub: '一段 200 字的产品发布文案' },
  { icon: '🧑‍💻', title: '写段代码', sub: 'Go 实现令牌桶限流器' },
  { icon: '🌏', title: '中英互译', sub: '把上面这段翻译为英文' },
]

function useSuggestion(s: typeof suggestions[number]) {
  chatInput.value = `${s.title}:${s.sub}`
  nextTick(() => inputRef.value?.focus?.())
}

async function scrollChat(force = false) {
  await nextTick()
  const el = chatScroll.value
  if (!el) return
  if (force) {
    el.scrollTop = el.scrollHeight
    return
  }
  const gap = el.scrollHeight - el.scrollTop - el.clientHeight
  if (gap < 180) el.scrollTop = el.scrollHeight
}

async function sendChat() {
  if (chatSending.value) return
  const text = chatInput.value.trim()
  if (!text) return
  if (!selectedChatModel.value) {
    ElMessage.warning('服务暂不可用')
    return
  }
  const now = Date.now()
  chatMsgs.value.push({ id: ++uid, role: 'user', content: text, at: now })
  chatInput.value = ''
  const assistant: UIMessage = { id: ++uid, role: 'assistant', content: '', pending: true, at: now }
  chatMsgs.value.push(assistant)
  await scrollChat(true)

  const history: PlayChatMessage[] = []
  if (systemPrompt.value.trim()) {
    history.push({ role: 'system', content: systemPrompt.value.trim() })
  }
  for (const m of chatMsgs.value.slice(0, -1)) {
    if (m.error) continue
    history.push({ role: m.role as 'user' | 'assistant' | 'system', content: m.content })
  }

  chatSending.value = true
  chatAbort.value = new AbortController()
  try {
    await streamPlayChat(
      { model: selectedChatModel.value, messages: history, temperature: temperature.value },
      (delta) => {
        assistant.content += delta
        assistant.pending = false
        scrollChat()
      },
      chatAbort.value.signal,
    )
    assistant.pending = false
    if (!assistant.content) assistant.content = '(无输出)'
  } catch (err: unknown) {
    assistant.pending = false
    assistant.error = true
    const msg = err instanceof Error ? err.message : String(err)
    assistant.content = assistant.content || `请求失败:${msg}`
    ElMessage.error(msg)
  } finally {
    chatSending.value = false
    chatAbort.value = null
    scrollChat()
    userStore.fetchMe().catch(() => {})
  }
}

function stopChat() {
  chatAbort.value?.abort()
}

function resetChat() {
  if (chatSending.value) stopChat()
  chatMsgs.value = []
}

async function regenerate() {
  if (chatSending.value) return
  // 去掉最后一条 assistant,把最后一条 user 重发
  let lastUserIdx = -1
  for (let i = chatMsgs.value.length - 1; i >= 0; i--) {
    if (chatMsgs.value[i].role === 'user') { lastUserIdx = i; break }
  }
  if (lastUserIdx < 0) return
  const lastUserText = chatMsgs.value[lastUserIdx].content
  chatMsgs.value = chatMsgs.value.slice(0, lastUserIdx)
  chatInput.value = lastUserText
  await sendChat()
}

function copyText(s: string) {
  try {
    navigator.clipboard.writeText(s)
    ElMessage.success('已复制')
  } catch {
    ElMessage.warning('复制失败')
  }
}

onBeforeUnmount(() => {
  chatAbort.value?.abort()
  if (videoPollTimer) window.clearTimeout(videoPollTimer)
  clearVideoImage()
  clearVideoReferenceVideo()
})

// ---------- 轻量 markdown 渲染(代码块 / 行内代码 / 粗体 / 链接) ----------
function escapeHtml(s: string) {
  return s
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#39;')
}

function renderMarkdown(raw: string): string {
  if (!raw) return ''
  const parts: string[] = []
  const blocks = raw.split(/```/g) // ``` 成对切分
  for (let i = 0; i < blocks.length; i++) {
    const chunk = blocks[i]
    if (i % 2 === 1) {
      // 代码块:首行可能是 lang
      const nl = chunk.indexOf('\n')
      let lang = ''
      let code = chunk
      if (nl >= 0) {
        const head = chunk.slice(0, nl).trim()
        if (/^[a-zA-Z0-9+_\-]{1,20}$/.test(head)) {
          lang = head
          code = chunk.slice(nl + 1)
        }
      }
      parts.push(
        `<pre class="mdk-pre" data-lang="${escapeHtml(lang || '')}"><code>${escapeHtml(
          code.replace(/\n$/, ''),
        )}</code></pre>`,
      )
    } else {
      // 行内元素
      let html = escapeHtml(chunk)
      // 行内代码 `xxx`
      html = html.replace(/`([^`\n]+)`/g, '<code class="mdk-ic">$1</code>')
      // 粗体 **xxx**
      html = html.replace(/\*\*([^*\n]+)\*\*/g, '<strong>$1</strong>')
      // 自动链接
      html = html.replace(
        /(https?:\/\/[\w\-._~:/?#\[\]@!$&'()*+,;=%]+)/g,
        '<a href="$1" target="_blank" rel="noopener">$1</a>',
      )
      // 换行
      html = html.replace(/\n/g, '<br />')
      parts.push(html)
    }
  }
  return parts.join('')
}

// ====================================================
// 文生图(Text2Img)
// ====================================================

// 10 档比例:对应上游 chatgpt.com 实际靠 prompt 第一行 "Make the aspect ratio X:Y , "
// 控制画面比例。OpenAI 兼容 size 仅作占位,按宽高比就近映射到官方支持的三档。
interface RatioOpt {
  label: string      // 中文名:方形 / 宽屏 / 竖版 …
  ratio: string      // 比例文本:1:1 / 21:9 …
  w: number          // 宽
  h: number          // 高
  size: string       // 发给后端的 OpenAI size
}
const RATIOS: readonly RatioOpt[] = [
  { label: '方形',   ratio: '1:1',  w: 1,  h: 1,  size: '1024x1024' },
  { label: '横屏',   ratio: '5:4',  w: 5,  h: 4,  size: '1792x1024' },
  { label: '故事',   ratio: '9:16', w: 9,  h: 16, size: '1024x1792' },
  { label: '超宽屏', ratio: '21:9', w: 21, h: 9,  size: '1792x1024' },
  { label: '宽屏',   ratio: '16:9', w: 16, h: 9,  size: '1792x1024' },
  { label: '横屏',   ratio: '4:3',  w: 4,  h: 3,  size: '1792x1024' },
  { label: '宽幅',   ratio: '3:2',  w: 3,  h: 2,  size: '1792x1024' },
  { label: '标准',   ratio: '4:5',  w: 4,  h: 5,  size: '1024x1792' },
  { label: '竖版',   ratio: '3:4',  w: 3,  h: 4,  size: '1024x1792' },
  { label: '竖版',   ratio: '2:3',  w: 2,  h: 3,  size: '1024x1792' },
] as const

// 预览小框的尺寸(按比例缩放后的 CSS px),保证所有档都落在 ≤36x36 的方格内。
function ratioBoxStyle(r: RatioOpt) {
  const MAX = 36
  const ar = r.w / r.h
  const bw = ar >= 1 ? MAX : Math.round(MAX * ar)
  const bh = ar >= 1 ? Math.round(MAX / ar) : MAX
  return { width: `${bw}px`, height: `${bh}px` }
}

// 统一的 prompt 前缀同步工具:
// - 若第一行已经是 "Make the aspect ratio X:Y ,",就把 X:Y 换成新的 ratio
// - 否则把 "Make the aspect ratio {ratio} , " 插到最前面
// - 用户手动删掉这行后不会再自动补回(只有再次切换比例时才重新插入)
const RATIO_PREFIX_RE = /^\s*Make the aspect ratio\s+\S+\s*,\s*/i
function applyRatioPrefix(prompt: string, ratio: string): string {
  const prefix = `Make the aspect ratio ${ratio} , `
  const lines = prompt.split(/\r?\n/)
  if (lines.length > 0 && RATIO_PREFIX_RE.test(lines[0])) {
    lines[0] = lines[0].replace(RATIO_PREFIX_RE, prefix)
    return lines.join('\n')
  }
  return prefix + prompt
}

const t2iPrompt = ref('')
const t2iRatio = ref<string>('1:1')
const t2iSize = computed(() =>
  RATIOS.find((r) => r.ratio === t2iRatio.value)?.size ?? '1024x1024',
)
const t2iN = ref(1)
// 本地高清放大档位(空=原图 / '2k' / '4k')。
// 仅在图片代理 URL 首次请求时触发 decode + Catmull-Rom + PNG 编码,
// 进程内 LRU 缓存命中后毫秒级返回。
type UpscaleLevel = '' | '2k' | '4k'
const t2iUpscale = ref<UpscaleLevel>('')

// 切换比例时,实时把 prompt 第一行同步成新的 "Make the aspect ratio X:Y , "
watch(t2iRatio, (nv) => {
  t2iPrompt.value = applyRatioPrefix(t2iPrompt.value, nv)
})
const t2iSending = ref(false)
const t2iResult = ref<PlayImageData[]>([])
const t2iError = ref('')
const t2iAbort = ref<AbortController | null>(null)

type ImageHistoryMode = 'text2img' | 'img2img'
interface ImageHistoryItem {
  id: string
  mode: ImageHistoryMode
  prompt: string
  size: string
  upscale?: UpscaleLevel
  createdAt: string
  status: string
  creditCost?: number
  data: PlayImageData[]
}

const imageHistory = ref<ImageHistoryItem[]>([])
const imageHistoryLoading = ref(false)
const t2iHistoryVisible = ref(false)
const i2iHistoryVisible = ref(false)
const IMAGE_HISTORY_LIMIT = 20

function imageDataFromTask(task: ImageTask): PlayImageData[] {
  return (task.image_urls || []).map((url, idx) => ({
    url,
    file_id: task.file_ids?.[idx],
  }))
}

function imageHistoryModeFromPrompt(prompt: string): ImageHistoryMode {
  return /参考图|reference|保持|改动|换成|基于|根据/i.test(prompt) ? 'img2img' : 'text2img'
}

function imageHistoryFromTask(task: ImageTask): ImageHistoryItem {
  return {
    id: task.task_id,
    mode: imageHistoryModeFromPrompt(task.prompt || ''),
    prompt: task.prompt || '',
    size: task.size || '1024x1024',
    upscale: (task.upscale || '') as UpscaleLevel,
    createdAt: task.created_at,
    status: task.status,
    creditCost: task.credit_cost,
    data: imageDataFromTask(task),
  }
}

function upsertImageHistory(item: ImageHistoryItem) {
  const next = [item, ...imageHistory.value.filter((old) => old.id !== item.id)]
  imageHistory.value = next.slice(0, IMAGE_HISTORY_LIMIT)
}

async function loadImageHistory(force = false) {
  if (imageHistoryLoading.value) return
  if (!force && imageHistory.value.length > 0) return
  imageHistoryLoading.value = true
  try {
    const res = await listMyImageTasks({ limit: IMAGE_HISTORY_LIMIT, offset: 0 })
    imageHistory.value = (res.items || []).map(imageHistoryFromTask)
  } catch {
    // 静默;列表只是辅助入口
  } finally {
    imageHistoryLoading.value = false
  }
}

function pushPlayImageHistory(mode: ImageHistoryMode, prompt: string, resp: { task_id?: string; data?: PlayImageData[] }, extra: { size: string; upscale?: UpscaleLevel }) {
  const data = resp.data || []
  if (!resp.task_id || data.length === 0) return
  upsertImageHistory({
    id: resp.task_id,
    mode,
    prompt,
    size: extra.size,
    upscale: extra.upscale || '',
    createdAt: new Date().toISOString(),
    status: 'success',
    data,
  })
}

function openImageHistoryItem(item: ImageHistoryItem) {
  t2iHistoryVisible.value = false
  i2iHistoryVisible.value = false
  if (item.mode === 'img2img') {
    activeTab.value = 'img2img'
    i2iPrompt.value = item.prompt
    i2iResult.value = item.data
    i2iError.value = ''
    i2iSending.value = false
  } else {
    activeTab.value = 'text2img'
    t2iPrompt.value = item.prompt
    t2iResult.value = item.data
    t2iError.value = ''
    t2iSending.value = false
  }
}

const imgExamples = [
  '赛博朋克城市夜景,霓虹雨夜,电影感光影,8k',
  '一只金色胖柴犬穿西装坐在办公桌前,油画质感',
  '极简几何海报,蓝橙配色,主体是一只展翅的鹤',
  '童话风格蘑菇屋,黄昏光线,柔和景深',
]

// 点击示例 prompt 时,自动把当前比例的前缀拼到最前面,保持和 ratio 同步
function useT2iExample(p: string) {
  t2iPrompt.value = applyRatioPrefix(p, t2iRatio.value)
}

async function sendText2Img() {
  const prompt = t2iPrompt.value.trim()
  if (!prompt) {
    ElMessage.warning('请输入描述词 prompt')
    return
  }
  if (!selectedImageModel.value) {
    ElMessage.warning('服务暂不可用')
    return
  }
  t2iSending.value = true
  t2iError.value = ''
  t2iResult.value = []
  t2iAbort.value = new AbortController()
  try {
    const resp = await playGenerateImage(
      {
        model: selectedImageModel.value,
        prompt,
        n: t2iN.value,
        size: t2iSize.value,
        upscale: t2iUpscale.value || undefined,
      },
      t2iAbort.value.signal,
    )
    t2iResult.value = resp.data || []
    if (t2iResult.value.length === 0) {
      t2iError.value = '未产出图片,请重试或更换描述'
    } else {
      pushPlayImageHistory('text2img', prompt, resp, {
        size: t2iSize.value,
        upscale: t2iUpscale.value,
      })
      ElMessage.success(`生成成功,共 ${t2iResult.value.length} 张`)
    }
  } catch (err: unknown) {
    const msg = err instanceof Error ? err.message : String(err)
    t2iError.value = msg
    ElMessage.error(msg)
  } finally {
    t2iSending.value = false
    t2iAbort.value = null
    userStore.fetchMe().catch(() => {})
  }
}

function stopText2Img() {
  t2iAbort.value?.abort()
}

function openInNewWindow(url: string) {
  window.open(url, '_blank', 'noopener,noreferrer')
}

function downloadUrl(url: string) {
  const a = document.createElement('a')
  a.href = url
  a.target = '_blank'
  a.rel = 'noopener'
  a.download = ''
  document.body.appendChild(a)
  a.click()
  document.body.removeChild(a)
}

// ====================================================
// 图生图(Img2Img)
// ====================================================
interface RefImage {
  name: string
  dataUrl: string
  size: number
}
const refImages = ref<RefImage[]>([])
const i2iPrompt = ref('')
const i2iRatio = ref<string>('1:1')
const i2iSize = computed(() =>
  RATIOS.find((r) => r.ratio === i2iRatio.value)?.size ?? '1024x1024',
)
const i2iUpscale = ref<UpscaleLevel>('')
watch(i2iRatio, (nv) => {
  i2iPrompt.value = applyRatioPrefix(i2iPrompt.value, nv)
})
const i2iSending = ref(false)
const i2iResult = ref<PlayImageData[]>([])
const i2iError = ref('')
const i2iAbort = ref<AbortController | null>(null)
const MAX_REF_BYTES = 4 * 1024 * 1024 // 4MB

function handleFilePick(e: Event) {
  const input = e.target as HTMLInputElement
  const files = input.files
  if (!files) return
  for (const file of Array.from(files)) {
    if (file.size > MAX_REF_BYTES) {
      ElMessage.warning(`${file.name} 超过 4MB 限制`)
      continue
    }
    const reader = new FileReader()
    reader.onload = () => {
      refImages.value.push({
        name: file.name,
        dataUrl: String(reader.result || ''),
        size: file.size,
      })
    }
    reader.readAsDataURL(file)
  }
  input.value = ''
}

function removeRefImage(idx: number) {
  refImages.value.splice(idx, 1)
}

async function sendImg2Img() {
  if (refImages.value.length === 0) {
    ElMessage.warning('请先上传至少一张参考图')
    return
  }
  if (!i2iPrompt.value.trim()) {
    ElMessage.warning('请描述希望的改动')
    return
  }
  if (!selectedImageModel.value) {
    ElMessage.warning('服务暂不可用')
    return
  }
  i2iSending.value = true
  i2iError.value = ''
  i2iResult.value = []
  i2iAbort.value = new AbortController()
  try {
    const resp = await playGenerateImage(
      {
        model: selectedImageModel.value,
        prompt: i2iPrompt.value.trim(),
        n: 1,
        size: i2iSize.value,
        reference_images: refImages.value.map((r) => r.dataUrl),
        upscale: i2iUpscale.value || undefined,
      },
      i2iAbort.value.signal,
    )
    i2iResult.value = resp.data || []
    if (i2iResult.value.length > 0) {
      pushPlayImageHistory('img2img', i2iPrompt.value.trim(), resp, {
        size: i2iSize.value,
        upscale: i2iUpscale.value,
      })
      ElMessage.success(`生成成功,共 ${i2iResult.value.length} 张`)
    }
  } catch (err: unknown) {
    const msg = err instanceof Error ? err.message : String(err)
    i2iError.value = msg
    ElMessage.error(msg)
  } finally {
    i2iSending.value = false
    i2iAbort.value = null
  }
}

// ====================================================
// 视频生成(Video)
// ====================================================
type VideoMode = 'text' | 'image' | 'video'

const videoChannels = ref<PlayVideoChannel[]>([])
const selectedVideoChannel = ref('apiyi_wan27')
const videoMode = ref<VideoMode>('text')
const videoPrompt = ref('生成一段 5 秒商品展示短视频，镜头缓慢推进，主体清晰，光线自然。')
const videoRatio = ref('16:9')
const videoResolution = ref('720p')
const videoDuration = ref(5)
const videoImage = ref<File | null>(null)
const videoImagePreview = ref('')
const videoRefVideo = ref<File | null>(null)
const videoRefVideoPreview = ref('')
const videoSending = ref(false)
const videoTask = ref<PlayVideoState | null>(null)
const videoError = ref('')
let videoPollTimer = 0
interface VideoHistoryItem extends PlayVideoState {
  prompt: string
  mode: VideoMode
  ratio: string
  resolution: string
  duration: number
  channel_name?: string
}

const VIDEO_HISTORY_STORAGE_KEY = 'gpt2api.online-play.video-history.v1'
const VIDEO_HISTORY_LIMIT = 20
const videoHistory = ref<VideoHistoryItem[]>([])
const videoHistoryVisible = ref(false)

const videoRatios = ['16:9', '9:16', '1:1', '4:3', '3:4']
const videoResolutions = ['720p', '1080p']

const selectedVideoChannelMeta = computed(() =>
  videoChannels.value.find((item) => item.type === selectedVideoChannel.value),
)

const videoStatusText = computed(() => {
  const state = videoTask.value
  if (!state) return '等待生成'
  if (state.status === 'completed') return '生成完成'
  if (state.status === 'failed') return '生成失败'
  if (state.status === 'queued') return '排队中'
  return '生成中'
})

const videoProgressStatus = computed(() => {
  const status = String(videoTask.value?.status || '').toLowerCase()
  if (status === 'completed') return 'success'
  if (status === 'failed') return 'exception'
  return undefined
})

const videoCanUseReferenceVideo = computed(() =>
  selectedVideoChannel.value === 'apiyi_wan27' || selectedVideoChannel.value === 'apiyi_happyhorse',
)

watch(videoMode, (mode) => {
  if (mode !== 'image') clearVideoImage()
  if (mode !== 'video') clearVideoReferenceVideo()
})

watch(selectedVideoChannel, () => {
  if (!videoCanUseReferenceVideo.value && videoMode.value === 'video') {
    videoMode.value = 'text'
    clearVideoReferenceVideo()
  }
})

async function loadVideoChannels() {
  try {
    const res = await listPlayVideoChannels()
    videoChannels.value = res.items || []
    selectedVideoChannel.value = res.default_channel_type || videoChannels.value[0]?.type || 'apiyi_wan27'
  } catch {
    // 错误由拦截器处理
  }
}

function loadVideoHistory() {
  try {
    const raw = localStorage.getItem(VIDEO_HISTORY_STORAGE_KEY)
    if (!raw) return
    const parsed = JSON.parse(raw)
    if (Array.isArray(parsed)) {
      videoHistory.value = parsed.slice(0, VIDEO_HISTORY_LIMIT)
    }
  } catch {
    videoHistory.value = []
  }
}

function persistVideoHistory() {
  try {
    localStorage.setItem(VIDEO_HISTORY_STORAGE_KEY, JSON.stringify(videoHistory.value.slice(0, VIDEO_HISTORY_LIMIT)))
  } catch {
    // ignore quota errors
  }
}

function videoHistoryItemFromState(state: PlayVideoState, prompt = videoPrompt.value.trim()): VideoHistoryItem {
  const channelName = videoChannels.value.find((item) => item.type === state.channel_type)?.name
  return {
    ...state,
    prompt,
    mode: videoMode.value,
    ratio: videoRatio.value,
    resolution: videoResolution.value,
    duration: videoDuration.value,
    channel_name: channelName,
  }
}

function upsertVideoHistory(item: VideoHistoryItem) {
  const next = [item, ...videoHistory.value.filter((old) => old.id !== item.id)]
  videoHistory.value = next.slice(0, VIDEO_HISTORY_LIMIT)
  persistVideoHistory()
}

function syncVideoHistoryState(state: PlayVideoState) {
  const old = videoHistory.value.find((item) => item.id === state.id)
  if (!old) return
  upsertVideoHistory({ ...old, ...state })
}

function openVideoHistoryItem(item: VideoHistoryItem) {
  videoHistoryVisible.value = false
  activeTab.value = 'video'
  selectedVideoChannel.value = item.channel_type || selectedVideoChannel.value
  videoMode.value = item.mode || 'text'
  videoPrompt.value = item.prompt || videoPrompt.value
  videoRatio.value = item.ratio || videoRatio.value
  videoResolution.value = item.resolution || videoResolution.value
  videoDuration.value = item.duration || videoDuration.value
  videoTask.value = item
  videoError.value = item.error || ''
  const status = String(item.status || '').toLowerCase()
  videoSending.value = status !== 'completed' && status !== 'failed'
  if (videoSending.value) pollVideo(item.id)
}

function handleVideoImagePick(e: Event) {
  const input = e.target as HTMLInputElement
  const file = input.files?.[0]
  input.value = ''
  if (!file) return
  if (file.size > 10 * 1024 * 1024) {
    ElMessage.warning('参考图最大 10MB')
    return
  }
  clearVideoImage()
  videoImage.value = file
  videoImagePreview.value = URL.createObjectURL(file)
}

function handleVideoReferencePick(e: Event) {
  const input = e.target as HTMLInputElement
  const file = input.files?.[0]
  input.value = ''
  if (!file) return
  if (file.size > 200 * 1024 * 1024) {
    ElMessage.warning('参考视频最大 200MB')
    return
  }
  clearVideoReferenceVideo()
  videoRefVideo.value = file
  videoRefVideoPreview.value = URL.createObjectURL(file)
}

function clearVideoImage() {
  videoImage.value = null
  if (videoImagePreview.value) URL.revokeObjectURL(videoImagePreview.value)
  videoImagePreview.value = ''
}

function clearVideoReferenceVideo() {
  videoRefVideo.value = null
  if (videoRefVideoPreview.value) URL.revokeObjectURL(videoRefVideoPreview.value)
  videoRefVideoPreview.value = ''
}

async function sendVideo() {
  const prompt = videoPrompt.value.trim()
  if (!prompt) {
    ElMessage.warning('请输入视频提示词')
    return
  }
  if (videoMode.value === 'image' && !videoImage.value) {
    ElMessage.warning('请先上传参考图')
    return
  }
  if (videoMode.value === 'video' && !videoRefVideo.value) {
    ElMessage.warning('请先上传参考视频')
    return
  }
  videoSending.value = true
  videoError.value = ''
  videoTask.value = null
  try {
    const state = await startPlayVideo({
      channelType: selectedVideoChannel.value,
      prompt,
      ratio: videoRatio.value,
      resolution: videoResolution.value,
      duration: videoDuration.value,
      image: videoMode.value === 'image' ? videoImage.value : null,
      video: videoMode.value === 'video' ? videoRefVideo.value : null,
    })
    videoTask.value = state
    upsertVideoHistory(videoHistoryItemFromState(state, prompt))
    pollVideo(state.id)
    ElMessage.success('视频任务已创建')
  } catch (err: unknown) {
    const msg = err instanceof Error ? err.message : String(err)
    videoError.value = msg
    videoSending.value = false
    ElMessage.error(msg)
  }
}

function pollVideo(id: string) {
  if (videoPollTimer) window.clearTimeout(videoPollTimer)
  const tick = async () => {
    try {
      const state = await getPlayVideo(id)
      videoTask.value = state
      syncVideoHistoryState(state)
      const status = String(state.status || '').toLowerCase()
      if (status === 'completed' || status === 'failed') {
        videoSending.value = false
        if (status === 'completed') {
          ElMessage.success('视频生成完成')
          userStore.fetchMe().catch(() => {})
        } else {
          videoError.value = state.error || '视频生成失败'
        }
        return
      }
      videoPollTimer = window.setTimeout(tick, 3000)
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : String(err)
      videoError.value = msg
      videoSending.value = false
    }
  }
  videoPollTimer = window.setTimeout(tick, 1000)
}

function resetVideo() {
  if (videoPollTimer) window.clearTimeout(videoPollTimer)
  videoSending.value = false
  videoTask.value = null
  videoError.value = ''
}

// 代码块内的 "复制" 按钮(通过事件委托,避免每次重渲染都重新绑定)
function onMsgClick(e: MouseEvent) {
  const t = e.target as HTMLElement | null
  if (!t) return
  const btn = t.closest('.mdk-copy') as HTMLElement | null
  if (!btn) return
  const pre = btn.parentElement?.querySelector('code')
  if (!pre) return
  copyText(pre.textContent || '')
}

// input 自动聚焦(tab 切换后)
watch(activeTab, (v) => {
  if (v === 'chat') nextTick(() => inputRef.value?.focus?.())
  if (v === 'video' && videoChannels.value.length === 0) loadVideoChannels()
})
</script>

<template>
  <div class="page-container playground">
    <!-- ============ Hero(紧凑条) ============ -->
    <div class="hero card-block">
      <div class="hero-left">
        <el-icon class="hero-ic"><MagicStick /></el-icon>
        <div class="hero-txt">
          <h2 class="hero-title">在线体验</h2>
          <span class="hero-sub">
            浏览器中直接体验 {{ ENABLE_CHAT_MODEL ? '对话 / ' : '' }}图像生成 · 视频生成 · 同一账号池、同一套计费,记录同步到「使用记录」
          </span>
        </div>
      </div>
      <div class="hero-stats">
        <div class="mini-stat">
            <span class="mini-num">{{ balance }}</span>
            <span class="mini-lbl">积分</span>
          </div>
      </div>
    </div>

    <!-- ============ Tabs ============ -->
    <el-tabs v-model="activeTab" class="pg-tabs">
      <!-- =================================================== -->
      <!--                          Chat                         -->
      <!-- =================================================== -->
      <el-tab-pane v-if="ENABLE_CHAT_MODEL" name="chat">
        <template #label>
          <span class="tab-lbl"><el-icon><ChatDotRound /></el-icon> 对话</span>
        </template>

        <div class="chat-grid">
          <!-- 左侧:System + 温度 -->
          <aside class="card-block side">
            <div class="side-row">
              <label class="side-lbl">
                Temperature
                <span class="side-val">{{ temperature.toFixed(1) }}</span>
              </label>
              <el-slider v-model="temperature" :min="0" :max="2" :step="0.1" show-stops />
              <div class="side-hint">越低越保守、越高越发散。默认 0.7</div>
            </div>

            <div class="side-row">
              <label class="side-lbl">System Prompt</label>
              <el-input
                v-model="systemPrompt"
                type="textarea"
                :rows="6"
                resize="none"
                placeholder="为助手设定人格与风格"
              />
            </div>

            <el-button :disabled="chatMsgs.length === 0" @click="resetChat" class="side-btn">
              <el-icon><Delete /></el-icon> 清空会话
            </el-button>
          </aside>

          <!-- 右侧:聊天主区 -->
          <section class="card-block chat-main">
            <header class="chat-header">
              <div class="chat-title">
                <el-avatar :size="32" class="avatar-bot">
                  <el-icon><Cpu /></el-icon>
                </el-avatar>
                <div>
                  <div class="chat-title-text">智能对话</div>
                  <div class="chat-sub">
                    {{ chatSending ? '正在回复…' : (chatMsgs.length ? `${chatMsgs.length} 条消息` : '准备就绪') }}
                  </div>
                </div>
              </div>
              <div class="chat-tools">
                <el-tooltip content="重试上一个问题" placement="top">
                  <el-button
                    :disabled="chatSending || chatMsgs.length === 0"
                    circle
                    @click="regenerate"
                  >
                    <el-icon><RefreshRight /></el-icon>
                  </el-button>
                </el-tooltip>
              </div>
            </header>

            <div ref="chatScroll" class="chat-scroll" @click="onMsgClick">
              <!-- 空态:建议卡 -->
              <div v-if="chatMsgs.length === 0" class="welcome">
                <div class="welcome-hi">
                  👋 你好{{ user?.nickname ? ',' + user.nickname : '' }}
                </div>
                <div class="welcome-sub">选一个话题开始,或者直接在下方输入。</div>
                <div class="suggest-grid">
                  <div
                    v-for="(s, i) in suggestions"
                    :key="i"
                    class="suggest-card"
                    @click="useSuggestion(s)"
                  >
                    <div class="s-ic">{{ s.icon }}</div>
                    <div class="s-t">{{ s.title }}</div>
                    <div class="s-s">{{ s.sub }}</div>
                  </div>
                </div>
              </div>

              <!-- 消息列表 -->
              <article
                v-for="m in chatMsgs"
                :key="m.id"
                :class="['msg', m.role, m.error ? 'err' : '']"
              >
                <el-avatar :size="34" :class="m.role === 'user' ? 'avatar-user' : 'avatar-bot'">
                  <el-icon v-if="m.role === 'user'"><User /></el-icon>
                  <el-icon v-else><MagicStick /></el-icon>
                </el-avatar>
                <div class="msg-body">
                  <div class="msg-head">
                    <span class="who">{{ m.role === 'user' ? '我' : '助手' }}</span>
                    <span v-if="!m.pending && m.content" class="copy-btn" @click="copyText(m.content)">
                      <el-icon><CopyDocument /></el-icon> 复制
                    </span>
                  </div>
                  <div class="msg-content">
                    <div v-if="m.pending && !m.content" class="typing">
                      <span></span><span></span><span></span>
                    </div>
                    <div
                      v-else
                      class="md"
                      v-html="renderMarkdown(m.content)"
                    />
                  </div>
                </div>
              </article>
            </div>

            <!-- 输入条 -->
            <div class="composer" :class="{ focused: !!chatInput }">
              <el-input
                ref="inputRef"
                v-model="chatInput"
                type="textarea"
                :rows="1"
                :autosize="{ minRows: 1, maxRows: 6 }"
                resize="none"
                placeholder="给助手发消息…  Enter 发送,Shift+Enter 换行"
                @keydown.enter.exact.prevent="sendChat"
              />
              <div class="composer-tools">
                <span class="hint">
                  <el-icon><InfoFilled /></el-icon>
                  按 Enter 发送
                </span>
                <div style="flex:1" />
                <el-button v-if="chatSending" type="danger" @click="stopChat" round>
                  <el-icon><VideoPause /></el-icon> 停止
                </el-button>
                <el-button
                  v-else
                  type="primary"
                  :disabled="!chatInput.trim() || !selectedChatModel"
                  @click="sendChat"
                  round
                >
                  发送
                  <el-icon style="margin-left:4px"><Promotion /></el-icon>
                </el-button>
              </div>
            </div>
          </section>
        </div>
      </el-tab-pane>

      <!-- =================================================== -->
      <!--                        文生图                         -->
      <!-- =================================================== -->
      <el-tab-pane name="text2img">
        <template #label>
          <span class="tab-lbl"><el-icon><Picture /></el-icon> 文生图</span>
        </template>

        <div class="img-grid">
          <aside class="card-block side">
            <div class="side-row">
              <label class="side-lbl">
                画面比例
                <span class="side-val">{{ t2iRatio }}</span>
              </label>
              <div class="ratio-row">
                <button
                  v-for="r in RATIOS"
                  :key="r.ratio"
                  :class="['ratio-btn', { active: t2iRatio === r.ratio }]"
                  :title="`${r.label} · ${r.ratio}`"
                  @click="t2iRatio = r.ratio"
                >
                  <div class="ratio-box" :style="ratioBoxStyle(r)" />
                  <span class="ratio-name">{{ r.label }}</span>
                  <span class="ratio-val-sm">{{ r.ratio }}</span>
                </button>
              </div>
              <div class="side-hint">
                选中后会把 <code class="hint-code">Make the aspect ratio {{ t2iRatio }} ,</code>
                作为 prompt 第一行传给上游
              </div>
            </div>

            <div class="side-row">
              <label class="side-lbl">张数 <span class="side-val">{{ t2iN }}</span></label>
              <el-slider v-model="t2iN" :min="1" :max="4" show-stops />
            </div>

            <div class="side-row">
              <label class="side-lbl">
                输出尺寸
                <el-tooltip placement="top" effect="light">
                  <template #content>
                    <div style="max-width:260px;line-height:1.55;">
                      上游原生出图为 1024 或 1792 px;选择 2K/4K 会在图片加载时用本地
                      <b>Catmull-Rom 插值</b>放大并以 PNG 输出。<br>
                      <span style="color:#a16207;">注意:这是传统算法放大,不是 AI 超分,</span>不会补出新的纹理或毛发,只会让画面更大更平滑。4K 首次加载约 +0.5~1.5s,之后命中缓存。
                    </div>
                  </template>
                  <el-icon style="margin-left:4px;color:#94a3b8;cursor:help;"><InfoFilled /></el-icon>
                </el-tooltip>
              </label>
              <el-radio-group v-model="t2iUpscale" size="small" class="upscale-group">
                <el-radio-button label="">原图</el-radio-button>
                <el-radio-button label="2k">2K 高清</el-radio-button>
                <el-radio-button label="4k">4K 高清</el-radio-button>
              </el-radio-group>
            </div>

            <div class="side-row">
              <label class="side-lbl">Prompt</label>
              <el-input
                v-model="t2iPrompt"
                type="textarea"
                :rows="5"
                resize="none"
                placeholder="描述画面的主体、风格、光线、构图…越具体效果越好"
              />
              <div class="chips">
                <el-tag
                  v-for="(p, i) in imgExamples"
                  :key="i"
                  effect="plain"
                  round
                  class="chip"
                  @click="useT2iExample(p)"
                >{{ p }}</el-tag>
              </div>
            </div>

            <el-button v-if="t2iSending" type="danger" @click="stopText2Img" round class="side-btn">
              <el-icon><VideoPause /></el-icon> 停止
            </el-button>
            <el-button
              v-else
              type="primary"
              round
              size="large"
              :disabled="!t2iPrompt.trim() || !selectedImageModel"
              @click="sendText2Img"
              class="side-btn gen-btn"
            >
              <el-icon><MagicStick /></el-icon> 生成图片
            </el-button>
          </aside>

          <section class="card-block img-main">
            <div class="result-toolbar">
              <div>
                <strong>图片结果</strong>
                <span>{{ t2iResult.length ? `${t2iResult.length} 张` : '文生图' }}</span>
              </div>
              <el-popover
                v-model:visible="t2iHistoryVisible"
                placement="bottom-end"
                trigger="click"
                popper-class="play-history-popover"
                :width="380"
                @show="loadImageHistory(true)"
              >
                <template #reference>
                  <el-button :icon="Clock" :loading="imageHistoryLoading" aria-label="图片生成历史">生成历史</el-button>
                </template>
                <div class="play-history-panel">
                  <div class="play-history-head">
                    <div>
                      <span>图片历史</span>
                      <b>{{ imageHistory.length }} 条</b>
                    </div>
                    <el-button text size="small" :loading="imageHistoryLoading" @click="loadImageHistory(true)">刷新</el-button>
                  </div>
                  <div v-if="imageHistory.length" class="play-history-list">
                    <button
                      v-for="item in imageHistory"
                      :key="item.id"
                      type="button"
                      class="play-history-item"
                      @click="openImageHistoryItem(item)"
                    >
                      <span class="play-history-thumb">
                        <img v-if="item.data[0]?.url" :src="item.data[0].url" alt="历史图片" loading="lazy" />
                        <el-icon v-else><Picture /></el-icon>
                      </span>
                      <span class="play-history-copy">
                        <b>{{ item.prompt || '未命名图片任务' }}</b>
                        <small>{{ item.mode === 'img2img' ? '图生图' : '文生图' }} · {{ item.size }} · {{ formatDateShort(item.createdAt) }}</small>
                      </span>
                      <em :class="['play-history-status', item.status === 'success' ? 'success' : item.status === 'failed' ? 'danger' : 'muted']">
                        {{ item.status === 'success' ? '完成' : item.status === 'failed' ? '失败' : item.status }}
                      </em>
                    </button>
                  </div>
                  <el-empty v-else :description="imageHistoryLoading ? '加载中' : '暂无图片历史'" :image-size="54" />
                </div>
              </el-popover>
            </div>
            <div v-if="t2iSending" class="stage loading">
              <div class="orb"><el-icon class="spin"><Loading /></el-icon></div>
              <div class="stage-title">正在为你绘制…</div>
              <div class="stage-sub">上游渲染通常需要 1-2 分钟,请保持页面打开</div>
            </div>
            <div v-else-if="t2iError" class="err-block">
              <el-icon><WarningFilled /></el-icon>
              {{ t2iError }}
            </div>
            <div v-else-if="t2iResult.length === 0" class="stage">
              <div class="stage-art">🖼️</div>
              <div class="stage-title">还没有图片</div>
              <div class="stage-sub">在左侧填好 prompt 和参数,点击「生成图片」</div>
            </div>
            <div v-else class="result-wrap">
              <div class="result-grid">
                <div v-for="(img, idx) in t2iResult" :key="idx" class="img-cell">
                  <img
                    :src="img.url"
                    :alt="`result-${idx}`"
                    loading="lazy"
                    class="img-thumb"
                    @click="openInNewWindow(img.url)"
                  />
                  <div class="img-btns">
                    <button class="img-btn" @click="openInNewWindow(img.url)">
                      <el-icon><ZoomIn /></el-icon> 预览
                    </button>
                    <button class="img-btn" @click="downloadUrl(img.url)">
                      <el-icon><Download /></el-icon> 下载
                    </button>
                  </div>
                </div>
              </div>
            </div>
          </section>
        </div>
      </el-tab-pane>

      <!-- =================================================== -->
      <!--                        图生图                         -->
      <!-- =================================================== -->
      <el-tab-pane name="img2img">
        <template #label>
          <span class="tab-lbl"><el-icon><PictureFilled /></el-icon> 图生图</span>
        </template>

        <div class="img-grid">
          <aside class="card-block side">
            <div class="side-row">
              <label class="side-lbl">参考图 <span class="side-val">{{ refImages.length }}/多</span></label>
              <label class="upload-zone">
                <el-icon class="up-ic"><UploadFilled /></el-icon>
                <div class="up-t">点击选择 / 拖拽图片到这里</div>
                <div class="up-s">最多多张,每张 ≤ 4MB</div>
                <input type="file" accept="image/*" multiple @change="handleFilePick" />
              </label>

              <div v-if="refImages.length" class="ref-grid">
                <div v-for="(r, idx) in refImages" :key="idx" class="ref-thumb">
                  <img :src="r.dataUrl" :alt="r.name" />
                  <div class="ref-x" @click="removeRefImage(idx)">
                    <el-icon><Close /></el-icon>
                  </div>
                  <div class="ref-meta">{{ (r.size / 1024).toFixed(0) }} KB</div>
                </div>
              </div>
            </div>

            <div class="side-row">
              <label class="side-lbl">
                输出比例
                <span class="side-val">{{ i2iRatio }}</span>
              </label>
              <div class="ratio-row">
                <button
                  v-for="r in RATIOS"
                  :key="r.ratio"
                  :class="['ratio-btn', { active: i2iRatio === r.ratio }]"
                  :title="`${r.label} · ${r.ratio}`"
                  @click="i2iRatio = r.ratio"
                >
                  <div class="ratio-box" :style="ratioBoxStyle(r)" />
                  <span class="ratio-name">{{ r.label }}</span>
                  <span class="ratio-val-sm">{{ r.ratio }}</span>
                </button>
              </div>
              <div class="side-hint">
                切换后会把 <code class="hint-code">Make the aspect ratio {{ i2iRatio }} ,</code>
                作为 prompt 第一行
              </div>
            </div>

            <div class="side-row">
              <label class="side-lbl">
                输出尺寸
                <el-tooltip placement="top" effect="light">
                  <template #content>
                    <div style="max-width:260px;line-height:1.55;">
                      上游原生出图为 1024 或 1792 px;选择 2K/4K 会在图片加载时用本地
                      <b>Catmull-Rom 插值</b>放大并以 PNG 输出。<br>
                      <span style="color:#a16207;">注意:这是传统算法放大,不是 AI 超分,</span>不会补出新的纹理或毛发,只会让画面更大更平滑。4K 首次加载约 +0.5~1.5s,之后命中缓存。
                    </div>
                  </template>
                  <el-icon style="margin-left:4px;color:#94a3b8;cursor:help;"><InfoFilled /></el-icon>
                </el-tooltip>
              </label>
              <el-radio-group v-model="i2iUpscale" size="small" class="upscale-group">
                <el-radio-button label="">原图</el-radio-button>
                <el-radio-button label="2k">2K 高清</el-radio-button>
                <el-radio-button label="4k">4K 高清</el-radio-button>
              </el-radio-group>
            </div>

            <div class="side-row">
              <label class="side-lbl">希望如何改动</label>
              <el-input
                v-model="i2iPrompt"
                type="textarea"
                :rows="4"
                resize="none"
                placeholder="例:保持人物姿态,把背景换成赛博朋克夜景"
              />
            </div>

            <el-button
              type="primary"
              round
              size="large"
              :loading="i2iSending"
              :disabled="refImages.length === 0 || !i2iPrompt.trim()"
              @click="sendImg2Img"
              class="side-btn gen-btn"
            >
              <el-icon><MagicStick /></el-icon> 生成
            </el-button>
          </aside>

          <section class="card-block img-main">
            <div class="result-toolbar">
              <div>
                <strong>图片结果</strong>
                <span>{{ i2iResult.length ? `${i2iResult.length} 张` : '图生图' }}</span>
              </div>
              <el-popover
                v-model:visible="i2iHistoryVisible"
                placement="bottom-end"
                trigger="click"
                popper-class="play-history-popover"
                :width="380"
                @show="loadImageHistory(true)"
              >
                <template #reference>
                  <el-button :icon="Clock" :loading="imageHistoryLoading" aria-label="图片生成历史">生成历史</el-button>
                </template>
                <div class="play-history-panel">
                  <div class="play-history-head">
                    <div>
                      <span>图片历史</span>
                      <b>{{ imageHistory.length }} 条</b>
                    </div>
                    <el-button text size="small" :loading="imageHistoryLoading" @click="loadImageHistory(true)">刷新</el-button>
                  </div>
                  <div v-if="imageHistory.length" class="play-history-list">
                    <button
                      v-for="item in imageHistory"
                      :key="item.id"
                      type="button"
                      class="play-history-item"
                      @click="openImageHistoryItem(item)"
                    >
                      <span class="play-history-thumb">
                        <img v-if="item.data[0]?.url" :src="item.data[0].url" alt="历史图片" loading="lazy" />
                        <el-icon v-else><Picture /></el-icon>
                      </span>
                      <span class="play-history-copy">
                        <b>{{ item.prompt || '未命名图片任务' }}</b>
                        <small>{{ item.mode === 'img2img' ? '图生图' : '文生图' }} · {{ item.size }} · {{ formatDateShort(item.createdAt) }}</small>
                      </span>
                      <em :class="['play-history-status', item.status === 'success' ? 'success' : item.status === 'failed' ? 'danger' : 'muted']">
                        {{ item.status === 'success' ? '完成' : item.status === 'failed' ? '失败' : item.status }}
                      </em>
                    </button>
                  </div>
                  <el-empty v-else :description="imageHistoryLoading ? '加载中' : '暂无图片历史'" :image-size="54" />
                </div>
              </el-popover>
            </div>
            <div v-if="i2iError" class="err-block">
              <el-icon><WarningFilled /></el-icon>
              {{ i2iError }}
            </div>
            <div v-else-if="i2iSending" class="stage loading">
              <div class="orb"><el-icon class="spin"><Loading /></el-icon></div>
              <div class="stage-title">正在生成…</div>
            </div>
            <div v-else-if="i2iResult.length === 0" class="stage">
              <div class="stage-art">🎨</div>
              <div class="stage-title">还没有结果</div>
              <div class="stage-sub">上传参考图 + 描述改动,然后点击「生成」</div>
            </div>
            <div v-else class="result-wrap">
              <div class="result-grid">
                <div v-for="(img, idx) in i2iResult" :key="idx" class="img-cell">
                  <img
                    :src="img.url"
                    :alt="`result-${idx}`"
                    class="img-thumb"
                    @click="openInNewWindow(img.url)"
                  />
                  <div class="img-btns">
                    <button class="img-btn" @click="openInNewWindow(img.url)">
                      <el-icon><ZoomIn /></el-icon> 预览
                    </button>
                    <button class="img-btn" @click="downloadUrl(img.url)">
                      <el-icon><Download /></el-icon> 下载
                    </button>
                  </div>
                </div>
              </div>
            </div>
          </section>
        </div>
      </el-tab-pane>

      <!-- =================================================== -->
      <!--                        视频生成                       -->
      <!-- =================================================== -->
      <el-tab-pane name="video">
        <template #label>
          <span class="tab-lbl"><el-icon><VideoPlay /></el-icon> 视频生成</span>
        </template>

        <div class="img-grid">
          <aside class="card-block side">
            <div class="side-row">
              <label class="side-lbl">渠道 <span class="side-val">{{ selectedVideoChannelMeta?.name || selectedVideoChannel }}</span></label>
              <el-select v-model="selectedVideoChannel" filterable style="width:100%">
                <el-option
                  v-for="ch in videoChannels"
                  :key="ch.type"
                  :label="ch.name"
                  :value="ch.type"
                />
              </el-select>
            </div>

            <div class="side-row">
              <label class="side-lbl">生成方式</label>
              <el-radio-group v-model="videoMode" size="small" class="upscale-group">
                <el-radio-button label="text">文生视频</el-radio-button>
                <el-radio-button label="image">图生视频</el-radio-button>
                <el-radio-button label="video" :disabled="!videoCanUseReferenceVideo">参考视频</el-radio-button>
              </el-radio-group>
              <div v-if="!videoCanUseReferenceVideo" class="side-hint">
                参考视频目前仅支持 API易 Wan2.7 / HappyHorse 渠道。
              </div>
            </div>

            <div class="side-row">
              <label class="side-lbl">画幅 <span class="side-val">{{ videoRatio }}</span></label>
              <div class="video-ratio-row">
                <button
                  v-for="ratio in videoRatios"
                  :key="ratio"
                  :class="['video-ratio-btn', { active: videoRatio === ratio }]"
                  @click="videoRatio = ratio"
                >{{ ratio }}</button>
              </div>
            </div>

            <div class="side-row video-param-row">
              <div>
                <label class="side-lbl">分辨率</label>
                <el-select v-model="videoResolution" size="small" style="width:100%">
                  <el-option v-for="item in videoResolutions" :key="item" :label="item" :value="item" />
                </el-select>
              </div>
              <div>
                <label class="side-lbl">时长</label>
                <el-input-number v-model="videoDuration" :min="3" :max="10" size="small" controls-position="right" style="width:100%" />
              </div>
            </div>

            <div v-if="videoMode === 'image'" class="side-row">
              <label class="side-lbl">参考图</label>
              <label class="upload-zone video-upload-zone">
                <el-icon class="up-ic"><UploadFilled /></el-icon>
                <div class="up-t">{{ videoImage ? videoImage.name : '上传一张参考图' }}</div>
                <div class="up-s">PNG / JPG / WebP, ≤ 10MB</div>
                <input type="file" accept="image/*" @change="handleVideoImagePick" />
              </label>
              <div v-if="videoImagePreview" class="video-ref-preview">
                <img :src="videoImagePreview" alt="参考图" />
                <button @click="clearVideoImage"><el-icon><Close /></el-icon></button>
              </div>
            </div>

            <div v-if="videoMode === 'video'" class="side-row">
              <label class="side-lbl">参考视频</label>
              <label class="upload-zone video-upload-zone">
                <el-icon class="up-ic"><UploadFilled /></el-icon>
                <div class="up-t">{{ videoRefVideo ? videoRefVideo.name : '上传一段参考视频' }}</div>
                <div class="up-s">MP4 / MOV / WebM, ≤ 200MB</div>
                <input type="file" accept="video/mp4,video/quicktime,video/webm,video/*" @change="handleVideoReferencePick" />
              </label>
              <div v-if="videoRefVideoPreview" class="video-ref-preview">
                <video :src="videoRefVideoPreview" muted playsinline controls />
                <button @click="clearVideoReferenceVideo"><el-icon><Close /></el-icon></button>
              </div>
            </div>

            <div class="side-row">
              <label class="side-lbl">Prompt</label>
              <el-input
                v-model="videoPrompt"
                type="textarea"
                :rows="5"
                resize="none"
                placeholder="描述镜头、主体、动作、风格、光线和节奏"
              />
            </div>

            <div class="video-actions">
              <el-button :disabled="!videoTask && !videoError" @click="resetVideo">清空</el-button>
              <el-button
                type="primary"
                round
                size="large"
                :loading="videoSending"
                :disabled="!videoPrompt.trim() || !selectedVideoChannel"
                @click="sendVideo"
                class="gen-btn"
              >
                <el-icon><MagicStick /></el-icon> 生成视频
              </el-button>
            </div>
          </aside>

          <section class="card-block img-main video-main">
            <div class="result-toolbar">
              <div>
                <strong>视频结果</strong>
                <span>{{ videoTask ? videoStatusText : '视频生成' }}</span>
              </div>
              <el-popover
                v-model:visible="videoHistoryVisible"
                placement="bottom-end"
                trigger="click"
                popper-class="play-history-popover"
                :width="420"
              >
                <template #reference>
                  <el-button :icon="Clock" aria-label="视频生成历史">生成历史</el-button>
                </template>
                <div class="play-history-panel">
                  <div class="play-history-head">
                    <div>
                      <span>视频历史</span>
                      <b>{{ videoHistory.length }} 条</b>
                    </div>
                  </div>
                  <div v-if="videoHistory.length" class="play-history-list">
                    <button
                      v-for="item in videoHistory"
                      :key="item.id"
                      type="button"
                      class="play-history-item video"
                      @click="openVideoHistoryItem(item)"
                    >
                      <span class="play-history-thumb video">
                        <video v-if="item.result_url" :src="item.result_url" muted playsinline preload="metadata" />
                        <img v-else-if="item.image_url" :src="item.image_url" alt="视频参考图" loading="lazy" />
                        <el-icon v-else><VideoPlay /></el-icon>
                      </span>
                      <span class="play-history-copy">
                        <b>{{ item.prompt || '未命名视频任务' }}</b>
                        <small>{{ item.channel_name || item.channel_type }} · {{ item.mode === 'image' ? '图生视频' : item.mode === 'video' ? '参考视频' : '文生视频' }} · {{ formatDateShort(item.created_at) }}</small>
                      </span>
                      <em :class="['play-history-status', item.status === 'completed' ? 'success' : item.status === 'failed' ? 'danger' : 'muted']">
                        {{ item.status === 'completed' ? '完成' : item.status === 'failed' ? '失败' : `${item.progress || 0}%` }}
                      </em>
                    </button>
                  </div>
                  <el-empty v-else description="暂无视频历史" :image-size="54" />
                </div>
              </el-popover>
            </div>
            <div v-if="videoTask?.result_url" class="video-result">
              <video :src="videoTask.result_url" controls playsinline preload="metadata" />
              <div class="video-result-bar">
                <div>
                  <strong>{{ videoStatusText }}</strong>
                  <span v-if="videoTask.credit_cost">扣费 {{ formatCredit(videoTask.credit_cost) }}</span>
                </div>
                <div class="img-btns video-result-actions">
                  <button class="img-btn" @click="openInNewWindow(videoTask.result_url!)">
                    <el-icon><ZoomIn /></el-icon> 预览
                  </button>
                  <button class="img-btn" @click="downloadUrl(videoTask.result_url!)">
                    <el-icon><Download /></el-icon> 下载
                  </button>
                </div>
              </div>
            </div>
            <div v-else-if="videoTask" class="stage loading video-stage">
              <div class="orb"><el-icon class="spin"><Loading /></el-icon></div>
              <div class="stage-title">{{ videoStatusText }}</div>
              <div class="stage-sub">
                {{ videoTask.task_id ? `上游任务 ${videoTask.task_id}` : '任务已提交，正在等待上游返回进度' }}
              </div>
              <el-progress
                class="video-progress"
                :percentage="videoTask.status === 'completed' ? 100 : (videoTask.progress || 0)"
                :status="videoProgressStatus"
              />
              <div v-if="videoTask.error" class="err-block video-error">
                <el-icon><WarningFilled /></el-icon>
                {{ videoTask.error }}
              </div>
            </div>
            <div v-else-if="videoError" class="err-block">
              <el-icon><WarningFilled /></el-icon>
              {{ videoError }}
            </div>
            <div v-else class="stage">
              <div class="stage-art video-empty-art"><el-icon><VideoPlay /></el-icon></div>
              <div class="stage-title">还没有视频</div>
              <div class="stage-sub">选择渠道和生成方式，在左侧输入 prompt 后开始生成。</div>
            </div>
          </section>
        </div>
      </el-tab-pane>
    </el-tabs>

  </div>
</template>

<style scoped lang="scss">
.playground { padding-bottom: 24px; }

/* ====================== Hero(紧凑条) ====================== */
.hero {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  padding: 12px 18px !important;
  margin-bottom: 14px !important;
}
.hero-left {
  display: flex;
  align-items: center;
  gap: 12px;
  min-width: 0;
  flex: 1;
}
.hero-ic {
  width: 36px;
  height: 36px;
  border-radius: 10px;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 18px;
  color: var(--el-color-primary);
  background: var(--el-color-primary-light-9);
  flex-shrink: 0;
}
.hero-txt {
  display: flex;
  align-items: baseline;
  gap: 10px;
  min-width: 0;
  flex-wrap: wrap;
}
.hero-title {
  margin: 0;
  font-size: 16px;
  font-weight: 600;
  color: var(--el-text-color-primary);
  white-space: nowrap;
}
.hero-sub {
  font-size: 12.5px;
  color: var(--el-text-color-secondary);
  line-height: 1.5;
}
.hero-stats {
  display: flex;
  align-items: center;
  gap: 12px;
  flex-shrink: 0;
}
.mini-stat {
  display: inline-flex;
  align-items: baseline;
  gap: 4px;
  .mini-num {
    font-size: 14px;
    font-weight: 600;
    color: var(--el-color-primary);
  }
  .mini-lbl {
    font-size: 12px;
    color: var(--el-text-color-secondary);
  }
}
/* ====================== Tabs ====================== */
.pg-tabs {
  :deep(.el-tabs__header) { margin-bottom: 16px; }
  :deep(.el-tabs__nav-wrap::after) { background: var(--el-border-color-lighter); }
  :deep(.el-tabs__item) {
    font-size: 14px;
    font-weight: 500;
    padding: 0 18px;
  }
  :deep(.el-tabs__item.is-active) { font-weight: 600; }
}
.tab-lbl { display: inline-flex; align-items: center; gap: 6px; }

/* ====================== Side ====================== */
.side { display: flex; flex-direction: column; gap: 16px; height: fit-content; position: sticky; top: 12px; }
.side-row { display: flex; flex-direction: column; gap: 6px; }
.side-lbl {
  font-size: 12px;
  color: var(--el-text-color-secondary);
  font-weight: 500;
  display: flex; justify-content: space-between; align-items: center;
  letter-spacing: 0.3px;
  text-transform: uppercase;
}
.side-val { font-weight: 600; color: var(--el-color-primary); letter-spacing: 0; text-transform: none; font-size: 13px; }
.side-hint { font-size: 12px; color: var(--el-text-color-placeholder); line-height: 1.5; }
.side-btn { margin-top: 4px; }
.gen-btn { box-shadow: 0 6px 18px -6px rgba(64, 158, 255, 0.55); }

/* ---- 输出尺寸(本地高清放大)单选组 ---- */
.upscale-group { display: flex; width: 100%; }
.upscale-group :deep(.el-radio-button) { flex: 1; }
.upscale-group :deep(.el-radio-button__inner) {
  width: 100%;
  padding-left: 0;
  padding-right: 0;
  letter-spacing: 0.2px;
}
/* ====================== Chat ====================== */
.chat-grid {
  display: grid;
  grid-template-columns: 300px minmax(0, 1fr);
  gap: 16px;
  min-height: 620px;
}
.chat-main {
  display: flex; flex-direction: column;
  padding: 0;
  overflow: hidden;
  height: 720px;
}
.chat-header {
  display: flex; justify-content: space-between; align-items: center;
  padding: 12px 18px;
  border-bottom: 1px solid var(--el-border-color-lighter);
  background: linear-gradient(180deg, var(--el-bg-color) 0%, var(--el-fill-color-lighter) 100%);
}
.chat-title { display: flex; align-items: center; gap: 10px; }
.chat-title-text { font-size: 14px; font-weight: 600; color: var(--el-text-color-primary); }
.chat-sub { font-size: 12px; color: var(--el-text-color-secondary); margin-top: 2px; }
.chat-tools { display: flex; gap: 6px; }

.chat-scroll {
  flex: 1;
  overflow-y: auto;
  padding: 22px 24px;
  scroll-behavior: smooth;
}

/* ----- 欢迎 ----- */
.welcome {
  min-height: 100%;
  display: flex; flex-direction: column; align-items: center; justify-content: center;
  padding: 30px 20px;
}
.welcome-hi {
  font-size: 24px; font-weight: 700;
  color: var(--el-text-color-primary);
  margin-bottom: 6px;
}
.welcome-sub { color: var(--el-text-color-secondary); margin-bottom: 22px; font-size: 14px; }
.suggest-grid {
  display: grid; grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
  gap: 12px;
  width: 100%; max-width: 680px;
}
.suggest-card {
  border: 1px solid var(--el-border-color-lighter);
  border-radius: 12px;
  padding: 14px 16px;
  cursor: pointer;
  background: var(--el-bg-color);
  transition: all 0.2s;
  .s-ic { font-size: 20px; margin-bottom: 4px; }
  .s-t { font-size: 13px; font-weight: 600; color: var(--el-text-color-primary); }
  .s-s { font-size: 12px; color: var(--el-text-color-secondary); margin-top: 4px; line-height: 1.5; }
  &:hover {
    border-color: var(--el-color-primary);
    transform: translateY(-1px);
    box-shadow: 0 6px 18px -8px rgba(64, 158, 255, 0.35);
  }
}

/* ----- 消息 ----- */
.msg {
  display: flex;
  gap: 12px;
  padding: 14px 0;
  border-bottom: 1px dashed var(--el-border-color-lighter);
  animation: fadeIn 0.25s ease;
  &:last-child { border-bottom: none; }
  &.err .msg-content { color: var(--el-color-danger); }
}
@keyframes fadeIn {
  from { opacity: 0; transform: translateY(4px); }
  to   { opacity: 1; transform: translateY(0); }
}
.avatar-user {
  background: var(--el-color-primary);
  color: #fff;
  flex-shrink: 0;
}
.avatar-bot {
  background: var(--el-color-success);
  color: #fff;
  flex-shrink: 0;
}
.msg-body { flex: 1; min-width: 0; }
.msg-head {
  display: flex; align-items: center; justify-content: space-between;
  margin-bottom: 4px;
  .who { font-size: 12px; font-weight: 600; color: var(--el-text-color-secondary); }
  .copy-btn {
    font-size: 12px; color: var(--el-text-color-placeholder); cursor: pointer;
    display: inline-flex; align-items: center; gap: 2px;
    opacity: 0; transition: opacity 0.2s;
    &:hover { color: var(--el-color-primary); }
  }
}
.msg:hover .copy-btn { opacity: 1; }
.msg-content {
  font-size: 14px; line-height: 1.75;
  color: var(--el-text-color-primary);
  word-break: break-word;
}

/* markdown 渲染产物 */
.md :deep(.mdk-pre) {
  background: #0f172a;
  color: #e2e8f0;
  padding: 12px 14px;
  border-radius: 10px;
  overflow-x: auto;
  font-family: ui-monospace, Menlo, Consolas, monospace;
  font-size: 12.5px;
  line-height: 1.6;
  margin: 8px 0;
  position: relative;
  &::before {
    content: attr(data-lang);
    position: absolute;
    top: 6px; right: 10px;
    font-size: 10px;
    text-transform: uppercase;
    letter-spacing: 1px;
    color: #94a3b8;
    opacity: 0.8;
  }
}
.md :deep(.mdk-ic) {
  background: var(--el-fill-color);
  color: var(--el-color-primary);
  padding: 1px 6px;
  border-radius: 4px;
  font-family: ui-monospace, Menlo, Consolas, monospace;
  font-size: 12.5px;
}
.md :deep(a) { color: var(--el-color-primary); text-decoration: none; }
.md :deep(a:hover) { text-decoration: underline; }
.md :deep(strong) { font-weight: 600; }

/* typing 指示器 */
.typing {
  display: inline-flex; gap: 5px; padding: 4px 0;
  span {
    width: 7px; height: 7px; border-radius: 50%;
    background: var(--el-color-primary);
    animation: blink 1.4s infinite ease-in-out both;
  }
  span:nth-child(2) { animation-delay: 0.2s; }
  span:nth-child(3) { animation-delay: 0.4s; }
}
@keyframes blink {
  0%, 80%, 100% { opacity: 0.2; transform: scale(0.7); }
  40% { opacity: 1; transform: scale(1); }
}

/* ----- 输入条 ----- */
.composer {
  padding: 12px 18px 16px;
  border-top: 1px solid var(--el-border-color-lighter);
  background: var(--el-bg-color);
  transition: box-shadow 0.2s;
  :deep(.el-textarea__inner) {
    border-radius: 12px;
    padding: 10px 14px;
    font-size: 14px;
    box-shadow: none;
    border: 1px solid var(--el-border-color);
    transition: border-color 0.2s, box-shadow 0.2s;
    &:focus {
      border-color: var(--el-color-primary);
      box-shadow: 0 0 0 3px rgba(64, 158, 255, 0.15);
    }
  }
}
.composer-tools {
  display: flex; align-items: center; gap: 8px; margin-top: 10px;
  .hint {
    display: inline-flex; align-items: center; gap: 4px;
    font-size: 12px; color: var(--el-text-color-placeholder);
  }
}

/* ====================== 图片面板 ====================== */
.img-grid {
  display: grid;
  grid-template-columns: 340px minmax(0, 1fr);
  gap: 16px;
}
/* img-main 高度随内容自适应，stage（空态/loading）保留最小高度 */
.img-main { min-height: 0; }
.result-toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 0 0 12px;
  margin-bottom: 12px;
  border-bottom: 1px solid var(--el-border-color-lighter);
  > div {
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 2px;
  }
  strong {
    font-size: 14px;
    color: var(--el-text-color-primary);
    font-weight: 700;
  }
  span {
    font-size: 12px;
    color: var(--el-text-color-secondary);
  }
  :deep(.el-button) {
    flex: 0 0 auto;
  }
}
:global(.play-history-popover.el-popper) {
  width: min(420px, 92vw) !important;
  padding: 10px;
  border-radius: 12px;
  border-color: var(--el-border-color-lighter);
}
.play-history-panel {
  display: flex;
  flex-direction: column;
  gap: 10px;
}
.play-history-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  div {
    display: flex;
    flex-direction: column;
    gap: 2px;
  }
  span {
    font-size: 12px;
    color: var(--el-text-color-secondary);
  }
  b {
    font-size: 14px;
    color: var(--el-text-color-primary);
  }
}
.play-history-list {
  display: flex;
  flex-direction: column;
  gap: 8px;
  max-height: 390px;
  overflow-y: auto;
  padding-right: 2px;
}
.play-history-item {
  width: 100%;
  min-height: 72px;
  display: grid;
  grid-template-columns: 58px minmax(0, 1fr) auto;
  align-items: center;
  gap: 10px;
  border: 1px solid var(--el-border-color-lighter);
  border-radius: 10px;
  padding: 8px;
  background: var(--el-bg-color);
  color: inherit;
  text-align: left;
  cursor: pointer;
  transition: border-color 0.15s, background 0.15s;
  &:hover {
    border-color: var(--el-color-primary);
    background: var(--el-color-primary-light-9);
  }
}
.play-history-thumb {
  width: 58px;
  height: 58px;
  border-radius: 8px;
  overflow: hidden;
  background: var(--el-fill-color-light);
  color: var(--el-text-color-placeholder);
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 22px;
  img,
  video {
    width: 100%;
    height: 100%;
    object-fit: cover;
    display: block;
  }
  &.video {
    background: #0f172a;
    color: #fff;
  }
}
.play-history-copy {
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 5px;
  b,
  small {
    display: block;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  b {
    font-size: 13px;
    color: var(--el-text-color-primary);
    font-weight: 700;
  }
  small {
    font-size: 12px;
    color: var(--el-text-color-secondary);
  }
}
.play-history-status {
  min-width: 44px;
  border-radius: 999px;
  padding: 3px 8px;
  font-size: 12px;
  font-style: normal;
  font-weight: 700;
  text-align: center;
  color: var(--el-text-color-secondary);
  background: var(--el-fill-color);
  &.success {
    color: var(--el-color-success);
    background: var(--el-color-success-light-9);
  }
  &.danger {
    color: var(--el-color-danger);
    background: var(--el-color-danger-light-9);
  }
}

/* 比例按钮 —— 10 档预设,5 列 × 2 行 grid */
.ratio-row {
  display: grid;
  grid-template-columns: repeat(5, 1fr);
  gap: 6px;
}
.ratio-btn {
  background: var(--el-bg-color);
  border: 1px solid var(--el-border-color-lighter);
  border-radius: 8px;
  padding: 6px 2px 5px;
  cursor: pointer;
  display: flex; flex-direction: column; align-items: center;
  gap: 3px;
  font-size: 11px; color: var(--el-text-color-secondary);
  transition: all 0.15s;
  min-width: 0;
  .ratio-box {
    background: var(--el-fill-color-light);
    border-radius: 2px;
    border: 1px solid var(--el-border-color-lighter);
    flex: 0 0 auto;
    /* 固定一个 36px 高度的占位,避免不同比例下按钮整体高度抖动 */
    margin: 2px 0;
  }
  .ratio-name {
    font-size: 11px;
    line-height: 1.2;
    letter-spacing: 0;
  }
  .ratio-val-sm {
    font-size: 10px;
    color: var(--el-text-color-placeholder);
    font-family: ui-monospace, Menlo, Consolas, monospace;
    line-height: 1.2;
  }
  &:hover {
    border-color: var(--el-color-primary);
    color: var(--el-color-primary);
    .ratio-val-sm { color: var(--el-color-primary); }
  }
  &.active {
    border-color: var(--el-color-primary);
    background: var(--el-color-primary-light-9);
    color: var(--el-color-primary);
    font-weight: 600;
    .ratio-box {
      background: var(--el-color-primary);
      border-color: var(--el-color-primary);
    }
    .ratio-val-sm { color: var(--el-color-primary); font-weight: 600; }
  }
}

/* ratio 说明文字里的内联代码样式 */
.hint-code {
  background: var(--el-fill-color);
  color: var(--el-color-primary);
  padding: 1px 6px;
  border-radius: 4px;
  font-family: ui-monospace, Menlo, Consolas, monospace;
  font-size: 11.5px;
}

/* prompt chips */
.chips { display: flex; flex-wrap: wrap; gap: 6px; margin-top: 8px; }
.chip { cursor: pointer; max-width: 100%; overflow: hidden; text-overflow: ellipsis; }
.chip:hover { background: var(--el-color-primary-light-9); color: var(--el-color-primary); }

/* 上传区 */
.upload-zone {
  position: relative;
  display: flex; flex-direction: column; align-items: center;
  padding: 20px 12px;
  border: 2px dashed var(--el-border-color);
  border-radius: 12px;
  cursor: pointer;
  background: var(--el-fill-color-lighter);
  transition: all 0.2s;
  &:hover { border-color: var(--el-color-primary); background: var(--el-color-primary-light-9); }
  .up-ic { font-size: 32px; color: var(--el-color-primary); }
  .up-t { font-size: 13px; margin-top: 6px; color: var(--el-text-color-primary); }
  .up-s { font-size: 11px; color: var(--el-text-color-placeholder); margin-top: 2px; }
  input { position: absolute; inset: 0; opacity: 0; cursor: pointer; }
}

.video-ratio-row {
  display: grid;
  grid-template-columns: repeat(5, 1fr);
  gap: 6px;
}
.video-ratio-btn {
  border: 1px solid var(--el-border-color-lighter);
  background: var(--el-bg-color);
  color: var(--el-text-color-secondary);
  border-radius: 8px;
  min-height: 34px;
  font-size: 12px;
  cursor: pointer;
  transition: all 0.15s;
  font-family: ui-monospace, Menlo, Consolas, monospace;
  &:hover {
    border-color: var(--el-color-primary);
    color: var(--el-color-primary);
  }
  &.active {
    border-color: var(--el-color-primary);
    background: var(--el-color-primary-light-9);
    color: var(--el-color-primary);
    font-weight: 700;
  }
}
.video-param-row {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 10px;
}
.video-upload-zone {
  min-height: 112px;
  text-align: center;
}
.video-ref-preview {
  position: relative;
  border-radius: 10px;
  overflow: hidden;
  border: 1px solid var(--el-border-color-lighter);
  background: var(--el-fill-color-light);
  img,
  video {
    width: 100%;
    max-height: 190px;
    object-fit: contain;
    display: block;
    background: #000;
  }
  img { background: var(--el-fill-color-light); }
  button {
    position: absolute;
    top: 8px;
    right: 8px;
    width: 26px;
    height: 26px;
    border: 0;
    border-radius: 50%;
    background: rgba(0, 0, 0, 0.58);
    color: #fff;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    cursor: pointer;
  }
}
.video-actions {
  display: grid;
  grid-template-columns: 92px 1fr;
  gap: 10px;
  align-items: center;
}
.video-main {
  min-height: 540px;
}
.video-stage {
  gap: 14px;
}
.video-progress {
  width: min(420px, 100%);
}
.video-error {
  width: min(560px, 100%);
}
.video-result {
  display: flex;
  flex-direction: column;
  gap: 12px;
}
.video-result video {
  width: 100%;
  max-height: 640px;
  border-radius: 12px;
  background: #000;
  display: block;
}
.video-result-bar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 14px;
  flex-wrap: wrap;
  strong {
    display: block;
    font-size: 14px;
    color: var(--el-text-color-primary);
  }
  span {
    display: block;
    margin-top: 3px;
    font-size: 12px;
    color: var(--el-text-color-secondary);
  }
}
.video-result-actions {
  min-width: 190px;
  border-radius: 10px;
  overflow: hidden;
}
.video-empty-art {
  width: 64px;
  height: 64px;
  border-radius: 18px;
  background: var(--el-color-primary-light-9);
  color: var(--el-color-primary);
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 28px;
  margin-bottom: 16px;
}

.ref-grid {
  margin-top: 10px;
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  gap: 8px;
}
.ref-thumb {
  position: relative;
  aspect-ratio: 1;
  border-radius: 8px;
  overflow: hidden;
  background: var(--el-fill-color-light);
  img { width: 100%; height: 100%; object-fit: cover; display: block; }
  .ref-x {
    position: absolute; top: 4px; right: 4px;
    width: 20px; height: 20px; border-radius: 50%;
    background: rgba(0,0,0,0.55); color: #fff;
    display: flex; align-items: center; justify-content: center;
    cursor: pointer; font-size: 12px;
    opacity: 0; transition: opacity 0.2s;
  }
  .ref-meta {
    position: absolute; bottom: 0; left: 0; right: 0;
    padding: 2px 6px;
    background: linear-gradient(transparent, rgba(0,0,0,0.6));
    color: #fff; font-size: 10px;
    opacity: 0; transition: opacity 0.2s;
  }
  &:hover { .ref-x, .ref-meta { opacity: 1; } }
}

/* 主区 stage / 结果 */
.stage {
  min-height: 440px;
  display: flex; flex-direction: column; align-items: center; justify-content: center;
  text-align: center; color: var(--el-text-color-secondary); padding: 40px 24px;
  .stage-art { font-size: 64px; margin-bottom: 16px; opacity: 0.7; }
  .stage-title { font-size: 16px; font-weight: 600; color: var(--el-text-color-primary); }
  .stage-sub { font-size: 13px; margin-top: 6px; }
  &.loading { gap: 14px; }
  .orb {
    width: 72px; height: 72px; border-radius: 50%;
    background: var(--el-color-primary-light-8);
    display: flex; align-items: center; justify-content: center;
    animation: pulseOrb 1.8s ease-in-out infinite;
  }
}
@keyframes pulseOrb {
  0%, 100% { transform: scale(1); box-shadow: 0 0 0 0 var(--el-color-primary-light-5); }
  50%      { transform: scale(1.08); box-shadow: 0 0 0 14px rgba(64,158,255,0); }
}
.spin { font-size: 30px; animation: spin 1s linear infinite; color: var(--el-color-primary); }
@keyframes spin { to { transform: rotate(360deg); } }

.err-block {
  background: var(--el-color-danger-light-9);
  color: var(--el-color-danger);
  padding: 12px 14px;
  border-radius: 10px;
  display: flex; align-items: center; gap: 8px;
  white-space: pre-wrap; word-break: break-word;
  border: 1px solid var(--el-color-danger-light-5);
}

/* 结果网格:1 张→铺满,2 张→各占一半,3-4 张→2 列自动换行 */
.result-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(min(100%, 360px), 1fr));
  gap: 14px;
  padding: 4px;
  align-items: start;
}
.img-cell {
  border-radius: 12px;
  overflow: hidden;
  background: var(--el-fill-color-light);
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.06);
  transition: box-shadow 0.2s;
  &:hover { box-shadow: 0 8px 24px rgba(0, 0, 0, 0.13); }
}
.img-thumb {
  width: 100%;
  height: auto;
  display: block;
  cursor: pointer;
  transition: opacity 0.2s;
  &:hover { opacity: 0.93; }
}
/* 图片下方固定按钮条 */
.img-btns {
  display: flex;
  gap: 1px;
  background: var(--el-border-color-lighter);
}
.img-btn {
  flex: 1;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 5px;
  padding: 8px 0;
  font-size: 13px;
  font-weight: 500;
  color: var(--el-text-color-regular);
  background: var(--el-bg-color);
  border: none;
  cursor: pointer;
  transition: background 0.15s, color 0.15s;
  &:hover {
    background: var(--el-color-primary-light-9);
    color: var(--el-color-primary);
  }
  &:first-child { border-radius: 0 0 0 12px; }
  &:last-child  { border-radius: 0 0 12px 0; }
}

.result-wrap {
  display: flex; flex-direction: column; gap: 10px;
  padding: 4px;
  .result-grid { padding: 0; }
}

/* ====================== Dark mode ====================== */
:global(html.dark) .md :deep(.mdk-pre) {
  background: #0b1020;
  color: #cbd5e1;
}

/* ====================== Responsive ====================== */
@media (max-width: 1100px) {
  .chat-grid, .img-grid { grid-template-columns: 1fr; }
  .side { position: static; }
  .chat-main { height: 580px; }
}
@media (max-width: 720px) {
  .hero {
    flex-direction: column;
    align-items: flex-start;
    gap: 10px;
  }
  .hero-sub { display: none; }
  .hero-stats { width: 100%; justify-content: flex-start; }
}
</style>
