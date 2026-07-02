<script setup lang="ts">
import { reactive, ref, computed } from 'vue'
import type { FormInstance } from 'element-plus'
import { ElMessage } from 'element-plus'
import { useRouter, useRoute } from 'vue-router'
import { useUserStore } from '@/stores/user'
import { useSiteStore } from '@/stores/site'

const router = useRouter()
const route = useRoute()
const store = useUserStore()
const site = useSiteStore()

const defaultLoginDesc = '支持商品图生成、营销文案创作与 API 管理'
const siteName = computed(() => site.get('site.name', 'GPT2API'))
const siteDesc = computed(() => {
  const desc = site.get('site.description', defaultLoginDesc)
  return desc === '企业级 OpenAI 兼容网关' ? defaultLoginDesc : desc
})
const siteLogo = computed(() => site.get('site.logo_url', ''))
const siteFooter = computed(() => site.get('site.footer', ''))
const allowRegister = computed(() => site.allowRegister())

const formRef = ref<FormInstance>()
const loading = ref(false)
const savedEmailKey = 'gpt2api.login.remembered_email'
const inlineError = ref('')

const form = reactive({
  email: localStorage.getItem(savedEmailKey) || '',
  password: '',
  rememberEmail: Boolean(localStorage.getItem(savedEmailKey)),
})

const rules = {
  email: [
    { required: true, message: '请输入邮箱', trigger: 'blur' },
    { type: 'email', message: '邮箱格式不正确', trigger: 'blur' },
  ],
  password: [
    { required: true, message: '请输入密码', trigger: 'blur' },
  ],
}

async function onSubmit() {
  if (!formRef.value) return
  if (loading.value) return
  inlineError.value = ''
  const ok = await formRef.value.validate().catch(() => false)
  if (!ok) return
  loading.value = true
  try {
    await store.login(form.email, form.password, { silent: true })
    if (form.rememberEmail) {
      localStorage.setItem(savedEmailKey, form.email)
    } else {
      localStorage.removeItem(savedEmailKey)
    }
    ElMessage.success('登录成功')
    const redirect = (route.query.redirect as string) || '/personal/dashboard'
    router.replace(redirect)
  } catch {
    inlineError.value = '邮箱或密码错误，请重新输入'
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <div class="login-page">
    <div class="hero-preview">
      <div class="brand">
        <img v-if="siteLogo" :src="siteLogo" class="logo-img" alt="logo" />
        <div v-else class="mark">{{ (siteName[0] || 'G').toUpperCase() }}</div>
        <div>
          <h1>{{ siteName }}</h1>
          <small>电商 AI 创作平台</small>
        </div>
      </div>
      <div class="hero-copy">
        <span class="eyebrow"><el-icon><MagicStick /></el-icon> AI 商品生图引擎</span>
        <h2><span>登录后继续</span><span>生成高转化电商素材</span></h2>
        <p class="tagline">{{ siteDesc }}</p>
      </div>
      <div class="generation-board" aria-hidden="true">
        <div class="board-top">
          <div>
            <span>图像工作台</span>
            <strong>商品主图生成中</strong>
          </div>
          <em>72%</em>
        </div>
        <div class="prompt-strip">
          <el-icon><EditPen /></el-icon>
          <span>奶油白蓝牙音箱，亚马逊主图，柔光棚拍，干净背景，高级质感</span>
        </div>
        <div class="generation-body">
          <div class="art-canvas">
            <div class="canvas-toolbar">
              <span><el-icon><Picture /></el-icon> 1:1 主图</span>
              <span>商品图</span>
              <span>种子 28</span>
            </div>
            <div class="product-scene">
              <i class="scene-halo"></i>
              <div class="speaker-shell">
                <i class="speaker-glow"></i>
                <i class="speaker-body"></i>
                <i class="speaker-grille"></i>
                <i class="speaker-dot dot-a"></i>
                <i class="speaker-dot dot-b"></i>
              </div>
              <span class="scene-chip chip-main"><el-icon><Check /></el-icon> 主图已完成</span>
              <span class="scene-chip chip-light">柔光棚拍</span>
            </div>
          </div>
          <div class="side-stack">
            <div class="status-card active">
              <span><el-icon class="spin"><Loading /></el-icon> 生图中</span>
              <strong>4 / 6</strong>
              <i></i>
            </div>
            <div class="status-card">
              <span><el-icon><VideoPlay /></el-icon> 短视频</span>
              <strong>待合成</strong>
              <i></i>
            </div>
            <div class="status-card">
              <span><el-icon><Document /></el-icon> 文案</span>
              <strong>已出稿</strong>
              <i></i>
            </div>
          </div>
        </div>
        <div class="asset-rail">
          <span class="asset-card active"><b>主图</b><em></em></span>
          <span class="asset-card mint"><b>场景</b><em></em></span>
          <span class="asset-card warm"><b>详情</b><em></em></span>
          <span class="asset-card text"><b>文案</b><em>卖点文案</em></span>
        </div>
      </div>
    </div>
    <el-card class="form-card" shadow="hover">
      <div class="mobile-brand">
        <img v-if="siteLogo" :src="siteLogo" class="logo-img" alt="logo" />
        <div v-else class="mark">{{ (siteName[0] || 'G').toUpperCase() }}</div>
        <span>{{ siteName }}</span>
      </div>
      <div class="form-title">欢迎回来</div>
      <div class="form-sub">登录后管理商品图、营销文案、素材资产与 API 配置。</div>
      <el-form
        ref="formRef"
        :model="form"
        :rules="rules"
        size="large"
        label-position="top"
        @submit.prevent="onSubmit"
      >
        <el-form-item label="邮箱" prop="email">
          <el-input v-model="form.email" placeholder="you@example.com" autocomplete="username" @input="inlineError = ''">
            <template #prefix><el-icon><Message /></el-icon></template>
          </el-input>
        </el-form-item>
        <el-form-item label="密码" prop="password">
          <el-input v-model="form.password" type="password" show-password placeholder="请输入密码"
                    autocomplete="current-password" @input="inlineError = ''">
            <template #prefix><el-icon><Lock /></el-icon></template>
          </el-input>
        </el-form-item>
        <div class="form-options">
          <el-checkbox v-model="form.rememberEmail">记住邮箱</el-checkbox>
        </div>
        <div class="inline-error" aria-live="polite">{{ inlineError }}</div>
        <el-button type="primary" native-type="submit" :loading="loading" :disabled="loading" class="submit">
          {{ loading ? '登录中...' : '登录' }}
        </el-button>
        <div class="foot">
          <template v-if="allowRegister">
            没有账号？<router-link to="/register">免费创建账号</router-link>
          </template>
          <template v-else>
            <span class="muted">管理员已关闭自助注册,请联系管理员创建账号</span>
          </template>
        </div>
      </el-form>
    </el-card>
    <div v-if="siteFooter" class="site-footer">{{ siteFooter }}</div>
  </div>
</template>

<style scoped lang="scss">
.login-page {
  min-height: 100vh;
  position: relative;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: clamp(28px, 4vh, 44px) 24px;
  box-sizing: border-box;
  overflow-x: hidden;
  overflow-y: auto;
  background:
    radial-gradient(760px 360px at 18% 16%, rgba(37,99,235,.13), transparent 62%),
    radial-gradient(720px 360px at 86% 76%, rgba(20,184,166,.18), transparent 64%),
    linear-gradient(135deg, #f5f8ff, #eef7fb);
}

:global(html.dark) .login-page {
  background:
    radial-gradient(1000px 400px at 10% 20%, #1b3a6a99, transparent),
    radial-gradient(800px 400px at 90% 80%, #1c4c2688, transparent),
    linear-gradient(135deg, #0d1117, #0b1f17);
}

:global(html.dark) .hero-preview .tagline { color: #cfd3dc; }
:global(html.dark) .hero-preview h1 { color: #f2f3f5; }

.hero-preview {
  position: relative;
  width: min(1100px, 100%);
  min-height: min(596px, calc(100vh - 88px));
  min-width: 0;
  padding: clamp(24px, 3vh, 32px) clamp(28px, 3vw, 40px);
  padding-right: clamp(390px, 38vw, 430px);
  box-sizing: border-box;
  overflow: hidden;
  border: 1px solid rgba(221, 229, 242, .9);
  border-radius: 28px;
  background:
    linear-gradient(135deg, rgba(255,255,255,.8), rgba(255,255,255,.38)),
    radial-gradient(600px 340px at 72% 18%, rgba(20,184,166,.22), transparent 65%),
    radial-gradient(440px 280px at 18% 86%, rgba(37,99,235,.12), transparent 62%);
  box-shadow: 0 28px 90px rgba(15, 23, 42, .11);

  &::before {
    content: "";
    position: absolute;
    inset: 0;
    background-image:
      linear-gradient(rgba(148,163,184,.08) 1px, transparent 1px),
      linear-gradient(90deg, rgba(148,163,184,.08) 1px, transparent 1px);
    background-size: 36px 36px;
    mask-image: linear-gradient(90deg, #000 0%, rgba(0,0,0,.72) 48%, transparent 90%);
    pointer-events: none;
  }

  &::after {
    content: "";
    position: absolute;
    width: 430px;
    height: 430px;
    right: -70px;
    bottom: -150px;
    border-radius: 999px;
    background:
      radial-gradient(circle at 52% 48%, rgba(20,184,166,.22), transparent 45%),
      radial-gradient(circle, rgba(37,99,235,.1), transparent 62%);
    filter: blur(4px);
    pointer-events: none;
  }

  > * { position: relative; z-index: 1; }

  .brand { display: flex; align-items: center; gap: 14px; }
  .logo-img { width: 40px; height: 40px; border-radius: 13px; object-fit: contain; background: #fff; }
  .mark {
    width: 40px; height: 40px; border-radius: 13px;
    display: inline-flex; align-items: center; justify-content: center;
    color: #fff; font-weight: 800; font-size: 18px;
    background: linear-gradient(135deg,var(--lc-primary),var(--lc-mint));
  }
  h1 { font-size: 21px; margin: 0; color: var(--lc-text); line-height: 25px; }
  small { color: #475569; font-size: 12px; font-weight: 650; }
  .tagline { max-width: 460px; color: var(--lc-muted); margin: 10px 0 0; line-height: 1.7; }
}

.hero-copy {
  margin-top: clamp(18px, 2.6vh, 26px);
  max-width: 430px;

  .eyebrow {
    width: fit-content;
    min-height: 32px;
    display: inline-flex;
    align-items: center;
    gap: 8px;
    padding: 0 12px;
    border: 1px solid rgba(37,99,235,.16);
    border-radius: 999px;
    color: var(--lc-primary-strong);
    background: rgba(255,255,255,.72);
    font-size: 13px;
    font-weight: 800;
    box-shadow: 0 10px 24px rgba(37,99,235,.08);
  }

  h2 {
    max-width: 430px;
    margin: 16px 0 0;
    color: var(--lc-text);
    font-size: clamp(28px, 2.1vw, 32px);
    line-height: 1.12;
    letter-spacing: 0;
    font-weight: 900;

    span {
      display: block;
      white-space: nowrap;
    }
  }
}

.site-footer {
  position: absolute;
  bottom: 12px; left: 0; right: 0;
  text-align: center; font-size: 12px; color: #909399;
  pointer-events: none;
}

.foot .muted { color: #64748b; }

.form-card {
  position: absolute;
  z-index: 4;
  top: 50%;
  right: max(36px, calc((100vw - 1100px) / 2 + 46px));
  transform: translateY(-48%);
  width: 100%;
  max-width: 370px;
  min-width: 0;
  border: 1px solid rgba(226,232,240,.92);
  border-radius: 24px;
  background: rgba(255,255,255,.96);
  box-shadow:
    0 34px 90px rgba(15, 23, 42, .24),
    0 1px 0 rgba(255,255,255,.85) inset;
  backdrop-filter: blur(22px);
  -webkit-backdrop-filter: blur(22px);

  :deep(.el-card__body) { padding: 26px; }
  .form-title { font-size: 24px; font-weight: 800; margin-bottom: 6px; letter-spacing: 0; }
  .form-sub { color: var(--el-text-color-secondary); margin-bottom: 18px; font-size: 14px; line-height: 1.65; }
  .submit {
    width: 100%;
    min-height: 42px;
    margin-top: 4px;
    cursor: pointer;
  }
  .foot {
    margin-top: 16px;
    text-align: center;
    font-size: 13px;
    color: var(--el-text-color-secondary);

    a {
      color: var(--lc-primary);
      font-weight: 800;
      text-decoration: none;
    }

    a:hover { color: var(--lc-primary-strong); }
  }

  :deep(.el-form-item) {
    margin-bottom: 14px;
  }

  :deep(.el-form-item__label) {
    color: #334155;
    font-weight: 700;
    line-height: 20px;
    margin-bottom: 6px;
  }

  :deep(.el-input__wrapper) {
    min-height: 42px;
    border: 1px solid rgba(203,213,225,.92);
    box-shadow: none;
    transition: border-color .2s ease, box-shadow .2s ease, background-color .2s ease;
  }

  :deep(.el-input__wrapper.is-focus) {
    border-color: var(--lc-primary);
    box-shadow: 0 0 0 3px rgba(37,99,235,.12);
  }

  :deep(.el-input__inner::placeholder) {
    color: #94a3b8;
  }

  :deep(.el-checkbox) {
    min-height: 32px;
    color: #475569;
    font-weight: 650;
  }

  :deep(.el-checkbox__input.is-checked + .el-checkbox__label) {
    color: #334155;
  }
}

.form-options {
  display: flex;
  align-items: center;
  justify-content: space-between;
  min-height: 32px;
  margin-top: -6px;
}

.inline-error {
  min-height: 22px;
  margin: 2px 0 8px;
  color: var(--lc-danger);
  font-size: 13px;
  line-height: 22px;
}

.mobile-brand { display: none; }

.generation-board {
  position: relative;
  margin-top: clamp(16px, 2.2vh, 20px);
  z-index: 1;
  width: min(600px, 100%);
  border: 1px solid rgba(221,229,242,.92);
  border-radius: 24px;
  padding: 14px;
  opacity: .88;
  background: linear-gradient(180deg, rgba(255,255,255,.82), rgba(255,255,255,.58));
  box-shadow: 0 18px 46px rgba(15, 23, 42, .1);
  backdrop-filter: blur(18px);
  -webkit-backdrop-filter: blur(18px);
}

.board-top {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 14px;

  span,
  em {
    display: block;
    color: var(--lc-muted);
    font-size: 12px;
    font-style: normal;
  }

  strong {
    display: block;
    margin-top: 3px;
    color: var(--lc-text);
    font-size: 16px;
    line-height: 22px;
  }

  em {
    min-width: 56px;
    height: 34px;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    border-radius: 12px;
    color: #fff;
    font-weight: 900;
    background: linear-gradient(135deg, var(--lc-primary), var(--lc-mint));
  }
}

.prompt-strip {
  min-height: 36px;
  margin: 10px 0;
  padding: 6px 11px;
  display: flex;
  align-items: center;
  gap: 9px;
  border: 1px solid var(--lc-border-soft);
  border-radius: 14px;
  color: #334155;
  background: #f8fbff;
  font-size: 12px;
  line-height: 18px;

  .el-icon { color: var(--lc-primary); flex: 0 0 auto; }
}

.generation-body {
  display: grid;
  grid-template-columns: minmax(0, 1fr) 124px;
  gap: 10px;
  align-items: stretch;
}

.art-canvas {
  min-height: 178px;
  position: relative;
  overflow: hidden;
  border: 1px solid rgba(203,213,225,.72);
  border-radius: 20px;
  background:
    radial-gradient(circle at 50% 52%, rgba(255,255,255,.98) 0 19%, rgba(255,255,255,.62) 20% 29%, transparent 30%),
    linear-gradient(145deg, #dbeafe, #dcfce7 58%, #f8fafc);

  &::before {
    content: "";
    position: absolute;
    inset: 18px;
    border-radius: 18px;
    background-image:
      linear-gradient(rgba(15,23,42,.05) 1px, transparent 1px),
      linear-gradient(90deg, rgba(15,23,42,.05) 1px, transparent 1px);
    background-size: 28px 28px;
    mask-image: radial-gradient(circle at 48% 52%, transparent 0 34%, #000 62%);
  }
}

.canvas-toolbar {
  position: absolute;
  z-index: 2;
  left: 14px;
  right: 14px;
  top: 14px;
  display: flex;
  gap: 8px;
  flex-wrap: wrap;

  span {
    min-height: 28px;
    display: flex;
    align-items: center;
    gap: 6px;
    padding: 0 10px;
    border-radius: 999px;
    color: #475569;
    background: rgba(255,255,255,.74);
    font-size: 12px;
    font-weight: 800;
    box-shadow: 0 8px 20px rgba(15,23,42,.07);
  }
}

.product-scene { position: absolute; inset: 0; }

.scene-halo {
  position: absolute;
  width: 160px;
  height: 160px;
  left: 50%;
  top: 50%;
  transform: translate(-52%, -41%);
  border-radius: 999px;
  background:
    radial-gradient(circle, rgba(255,255,255,.95) 0 25%, rgba(37,99,235,.16) 26% 49%, rgba(20,184,166,.08) 50% 68%, transparent 69%);
  box-shadow: 0 30px 80px rgba(37,99,235,.18);
}

.speaker-shell {
  position: absolute;
  width: 122px;
  height: 128px;
  left: 50%;
  top: 51%;
  transform: translate(-50%, -41%);
}

.speaker-body,
.speaker-grille,
.speaker-glow,
.speaker-dot {
  position: absolute;
  display: block;
}

.speaker-glow {
  inset: 14px 10px 0;
  border-radius: 42px;
  background: rgba(15,23,42,.16);
  filter: blur(18px);
  transform: translateY(34px) scale(.92);
}

.speaker-body {
  inset: 0 16px 18px;
  border-radius: 38px;
  background:
    linear-gradient(135deg, rgba(255,255,255,.92), rgba(219,234,254,.72) 52%, rgba(186,230,253,.85));
  border: 1px solid rgba(255,255,255,.9);
  box-shadow:
    inset 16px 16px 28px rgba(255,255,255,.72),
    inset -20px -18px 28px rgba(59,130,246,.12),
    0 26px 46px rgba(15,23,42,.18);
}

.speaker-grille {
  left: 30px;
  right: 30px;
  bottom: 26px;
  height: 48px;
  border-radius: 999px;
  background:
    radial-gradient(circle, rgba(15,23,42,.18) 1.4px, transparent 1.7px);
  background-size: 11px 11px;
  opacity: .74;
}

.speaker-dot {
  width: 10px;
  height: 10px;
  border-radius: 999px;
  background: var(--lc-primary);
  box-shadow: 0 0 0 5px rgba(37,99,235,.1);

  &.dot-a { left: 45px; top: 20px; }
  &.dot-b { right: 45px; top: 20px; background: var(--lc-mint); box-shadow: 0 0 0 5px rgba(20,184,166,.12); }
}

.scene-chip {
  position: absolute;
  min-height: 30px;
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 0 11px;
  border-radius: 999px;
  border: 1px solid rgba(255,255,255,.74);
  background: rgba(255,255,255,.72);
  color: #334155;
  font-size: 12px;
  font-weight: 850;
  box-shadow: 0 12px 26px rgba(15,23,42,.1);
}

.chip-main {
  left: 24px;
  bottom: 24px;
  color: var(--lc-mint-strong);

  .el-icon { color: var(--lc-mint-strong); }
}

.chip-light {
  right: 22px;
  bottom: 24px;
}

.side-stack {
  display: grid;
  gap: 8px;
}

.status-card {
  min-width: 0;
  min-height: 60px;
  padding: 9px;
  border: 1px solid var(--lc-border-soft);
  border-radius: 18px;
  background: #f8fafc;
  display: flex;
  flex-direction: column;
  justify-content: space-between;

  span,
  strong {
    display: flex;
    align-items: center;
    gap: 6px;
    color: #475569;
    font-size: 12px;
    line-height: 16px;
  }

  strong {
    color: var(--lc-text);
    font-size: 15px;
    font-weight: 900;
  }

  i {
    height: 5px;
    border-radius: 999px;
    background: #e2e8f0;
    overflow: hidden;
    position: relative;
    font-style: normal;

    &::after {
      content: "";
      position: absolute;
      inset: 0;
      width: 44%;
      border-radius: inherit;
      background: #94a3b8;
    }
  }

  &.active {
    background: var(--lc-primary-soft);
    border-color: rgba(37,99,235,.22);

    span,
    strong { color: var(--lc-primary-strong); }

    i::after {
      width: 72%;
      background: linear-gradient(90deg, var(--lc-primary), var(--lc-mint));
    }
  }
}

.spin {
  animation: spin 1s linear infinite;
}

@media (prefers-reduced-motion: reduce) {
  .spin {
    animation: none;
  }
}

.asset-rail {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 8px;
  margin-top: 10px;
}

.asset-card {
  min-width: 0;
  min-height: 48px;
  padding: 9px;
  border: 1px solid var(--lc-border-soft);
  border-radius: 16px;
  overflow: hidden;
  display: flex;
  flex-direction: column;
  justify-content: flex-end;
  background:
    radial-gradient(circle at 62% 34%, rgba(255,255,255,.95), transparent 18%),
    linear-gradient(135deg, #dbeafe, #f8fafc);

  &.active { box-shadow: inset 0 0 0 2px rgba(37,99,235,.34); }
  &.mint { background: radial-gradient(circle at 62% 34%, rgba(255,255,255,.95), transparent 18%), linear-gradient(135deg, #ccfbf1, #f8fafc); }
  &.warm { background: radial-gradient(circle at 62% 34%, rgba(255,255,255,.95), transparent 18%), linear-gradient(135deg, #ffedd5, #f8fafc); }
  &.text { background: #f8fafc; }

  b,
  em {
    display: block;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-size: 12px;
    font-style: normal;
    line-height: 16px;
  }
  b { color: var(--lc-text); font-weight: 900; }
  em { color: var(--lc-muted); margin-top: 2px; }
}

@keyframes spin {
  to { transform: rotate(360deg); }
}

@media (max-width: 960px) {
  .login-page { padding: 24px 20px; }
  .hero-preview {
    min-height: calc(100vh - 48px);
    padding: 28px;
    padding-right: 28px;
    text-align: left;
  }
  .hero-copy {
    margin-top: 26px;
    h2 { font-size: 38px; }
  }
  .generation-board {
    width: 100%;
    opacity: .42;
  }
  .generation-body {
    grid-template-columns: minmax(0, 1fr) 128px;
  }
  .art-canvas { min-height: 220px; }
  .form-card {
    left: 50%;
    right: auto;
    width: min(420px, calc(100vw - 48px));
    transform: translate(-50%, -38%);
  }
}

@media (max-width: 640px) {
  .login-page {
    min-height: 100dvh;
    align-items: center;
    padding: 16px;
  }
  .hero-preview {
    position: absolute;
    inset: 0;
    width: auto;
    min-height: auto;
    padding: 0;
    border: 0;
    border-radius: 0;
    background: transparent;
    box-shadow: none;

    &::before,
    &::after {
      display: none;
    }

    .brand,
    .hero-copy,
    .generation-board { display: none; }
  }
  .form-card {
    position: relative;
    left: auto;
    top: auto;
    width: calc(100vw - 32px);
    max-width: 100%;
    transform: none;
    :deep(.el-card__body) { padding: 24px; }
  }
  .mobile-brand {
    display: flex;
    align-items: center;
    gap: 10px;
    margin-bottom: 22px;
    font-weight: 800;
    .logo-img,
    .mark { width: 34px; height: 34px; border-radius: 12px; }
    .mark {
      display: inline-flex;
      align-items: center;
      justify-content: center;
      color: #fff;
      background: linear-gradient(135deg,var(--lc-primary),var(--lc-mint));
    }
  }
}

@media (max-height: 680px) and (min-width: 641px) {
  .login-page {
    align-items: flex-start;
    padding-top: 32px;
    padding-bottom: 32px;
  }

  .hero-preview {
    min-height: 616px;
  }
}
</style>
