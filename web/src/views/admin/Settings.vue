<script setup lang="ts">
import { ref, computed, reactive, onMounted, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
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
  uploadSiteAsset,
  type SettingItem,
  type VideoGenBalance,
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
  for (const k of Object.keys(map)) map[k].sort((a, b) => a.key.localeCompare(b.key))
  return map
})

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
function isVideoModelSetting(it: SettingItem) { return it.key === 'videogen.model' }
function siteAssetName(it: SettingItem) { return isLogo(it) ? 'Logo' : '图标' }
function inputType(it: SettingItem) {
  if (it.type === 'email') return 'email'
  if (it.type === 'url') return 'url'
  return 'text'
}

const imageGenTesting = ref(false)
const textGenTesting = ref(false)
const videoGenTesting = ref(false)
const videoGenBalanceLoading = ref(false)
const videoGenBalance = ref<VideoGenBalance | null>(null)
const videoGenBalanceLoaded = ref(false)
const videoGenModels = ref<VideoGenProbeModel[]>([])
const videoGenFreeQuotaTotal = computed(() => (
  videoGenBalance.value?.free_quotas || []
).reduce((sum, quota) => sum + Number(quota.remaining_count || 0), 0))

function formatNumber(n: number | undefined | null) {
  return Number(n || 0).toLocaleString('zh-CN')
}

async function loadVideoGenBalance(silent = false) {
  videoGenBalanceLoading.value = true
  try {
    videoGenBalance.value = await fetchVideoGenBalance(silent)
    videoGenBalanceLoaded.value = true
    if (!silent) ElMessage.success('视频网关余额已刷新')
  } catch {
    videoGenBalance.value = null
    videoGenBalanceLoaded.value = true
  } finally {
    videoGenBalanceLoading.value = false
  }
}

function ensureVideoGenBalanceLoaded() {
  if (activeTab.value === 'videogen' && !videoGenBalanceLoaded.value && !videoGenBalanceLoading.value) {
    loadVideoGenBalance(true)
  }
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

async function doTestVideoGen() {
  if (dirtyCount.value > 0) {
    await ElMessageBox.confirm('当前有未保存修改。是否先保存后再探测?', '确认', {
      type: 'warning',
    })
    await save()
  }
  videoGenTesting.value = true
  try {
    const res = await testVideoGen()
    videoGenModels.value = normalizeVideoModels(res.models || [])
    const currentModel = String(draft['videogen.model'] || '').trim()
    const matched = videoGenModels.value.find((model) => model.value === currentModel || model.name === currentModel)
    if (matched) {
      draft['videogen.model'] = matched.value
    } else if (!currentModel && videoGenModels.value[0]) {
      draft['videogen.model'] = videoGenModels.value[0].value
    }
    ElMessage.success(`探测成功: ${res.model_name || '已连接'}, 可选视频模型 ${videoGenModels.value.length} 个 / 上游总模型 ${res.model_count} 个, ${res.duration_ms}ms`)
    loadVideoGenBalance(true)
  } catch {
    // 拦截器已处理
  } finally {
    videoGenTesting.value = false
  }
}

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

function videoModelOptions(current: string) {
  const opts = [...videoGenModels.value]
  const value = String(current || '').trim()
  if (value && !opts.some((item) => item.value === value || item.id === value || item.name === value)) {
    opts.unshift({ id: value, name: value, type: '', label: `${value}（当前配置）`, value })
  }
  return opts
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
    if (Object.keys(diff).some((key) => key.startsWith('videogen.'))) {
      videoGenBalanceLoaded.value = false
      ensureVideoGenBalanceLoaded()
    }
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

watch(activeTab, () => ensureVideoGenBalanceLoaded())

onMounted(async () => {
  await load()
  ensureVideoGenBalanceLoaded()
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
          <el-button
            v-if="activeTab === 'videogen'"
            :icon="VideoPlay"
            :loading="videoGenTesting"
            @click="doTestVideoGen"
          >探测视频</el-button>
          <el-button
            v-if="activeTab === 'videogen'"
            :icon="Refresh"
            :loading="videoGenBalanceLoading"
            @click="loadVideoGenBalance()"
          >刷新余额</el-button>
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
              <div v-if="t.name === 'videogen'" class="gateway-balance" v-loading="videoGenBalanceLoading">
                <div class="balance-main">
                  <div>
                    <span>积分余额</span>
                    <strong>{{ formatNumber(videoGenBalance?.credits) }}</strong>
                  </div>
                  <div>
                    <span>充值余额</span>
                    <strong>{{ formatNumber(videoGenBalance?.recharge_balance) }}</strong>
                  </div>
                  <div>
                    <span>免费次数</span>
                    <strong>{{ formatNumber(videoGenFreeQuotaTotal) }}</strong>
                  </div>
                </div>
                <div class="balance-side">
                  <template v-if="videoGenBalance?.free_quotas?.length">
                    <el-tag
                      v-for="quota in videoGenBalance.free_quotas"
                      :key="`${quota.model_id || quota.model_name}-${quota.remaining_count}`"
                      size="small"
                      type="success"
                      effect="plain"
                    >
                      {{ quota.model_name || quota.model_id || '未命名模型' }}：{{ quota.remaining_count || 0 }}
                    </el-tag>
                  </template>
                  <span v-else class="balance-empty">
                    {{ videoGenBalanceLoaded ? '暂无剩余免费次数' : '打开页签后自动获取余额' }}
                  </span>
                  <small v-if="videoGenBalance">耗时 {{ videoGenBalance.duration_ms }}ms</small>
                </div>
              </div>
              <el-form
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
                    :min="0"
                    :max="1"
                    :step="0.05"
                    :precision="2"
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
                  <el-select
                    v-else-if="isVideoModelSetting(it)"
                    v-model="draft[it.key]"
                    filterable
                    allow-create
                    default-first-option
                    clearable
                    placeholder="请先点击探测视频获取模型"
                    style="max-width: 520px; width: 100%"
                  >
                    <el-option
                      v-for="model in videoModelOptions(draft[it.key])"
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
.field-wrap {
  width: 100%;
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
  .setting-form :deep(.el-form-item__label) {
    width: auto !important;
    padding-right: 8px !important;
    line-height: 1.5;
  }
}
</style>
