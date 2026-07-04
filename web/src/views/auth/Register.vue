<script setup lang="ts">
import { reactive, ref, computed } from 'vue'
import type { FormInstance } from 'element-plus'
import { ElMessage } from 'element-plus'
import { useRouter } from 'vue-router'
import { useUserStore } from '@/stores/user'
import { useSiteStore } from '@/stores/site'

const router = useRouter()
const store = useUserStore()
const site = useSiteStore()

const siteName = computed(() => site.get('site.name', '灵境智创'))
const allowRegister = computed(() => site.allowRegister())

const formRef = ref<FormInstance>()
const loading = ref(false)
const form = reactive({ email: '', password: '', confirm: '', nickname: '' })

const rules = {
  email: [
    { required: true, message: '请输入邮箱', trigger: 'blur' },
    { type: 'email', message: '邮箱格式不正确', trigger: 'blur' },
  ],
  password: [
    { required: true, message: '请输入密码', trigger: 'blur' },
    { min: 6, max: 64, message: '6~64 位', trigger: 'blur' },
  ],
  confirm: [
    { required: true, message: '请再次输入密码', trigger: 'blur' },
    {
      validator: (_r: unknown, v: string, cb: (e?: Error) => void) => {
        if (v !== form.password) cb(new Error('两次密码不一致'))
        else cb()
      },
      trigger: 'blur',
    },
  ],
}

async function onSubmit() {
  if (!formRef.value) return
  const ok = await formRef.value.validate().catch(() => false)
  if (!ok) return
  loading.value = true
  try {
    await store.register(form.email, form.password, form.nickname)
    ElMessage.success('注册成功,正在登录…')
    await store.login(form.email, form.password)
    router.replace('/personal/dashboard')
  } catch {
    // toast 由拦截器处理
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <div class="register-page">
    <div class="register-stage">
      <div class="stage-brand">
        <span class="mark">{{ (siteName[0] || '灵').toUpperCase() }}</span>
        <div>
          <b>{{ siteName }}</b>
          <small>电商 AI 工作台</small>
        </div>
      </div>
      <h1>把商品素材、短视频和文案流程收拢到一个账号里</h1>
      <p>注册后即可进入统一工作台，创建电商生成任务、沉淀资产库并管理 API 能力。</p>
      <div class="stage-preview" aria-hidden="true">
        <div class="preview-toolbar">
          <span></span>
          <span></span>
          <span></span>
        </div>
        <div class="preview-cover">
          <strong>新任务</strong>
          <em>商品主图 · 视频 · Listing</em>
        </div>
        <div class="preview-grid">
          <i></i>
          <i></i>
          <i></i>
        </div>
      </div>
    </div>
    <el-card class="form-card" shadow="hover">
      <div class="brand-line">
        <span class="mark">{{ (siteName[0] || '灵').toUpperCase() }}</span>
        <div>
          <b>{{ siteName }}</b>
          <small>电商 AI 工作台</small>
        </div>
      </div>
      <div class="form-title">创建 {{ siteName }} 账号</div>
      <div class="form-sub">注册后即可创建电商生成任务、管理素材资产和 API 能力。</div>
      <el-alert
        v-if="!allowRegister"
        type="warning"
        :closable="false"
        title="当前站点已关闭自助注册"
        description="请联系管理员开通账号,或改用已有账号登录。"
        style="margin-bottom:16px"
      />
      <el-form ref="formRef" :model="form" :rules="rules" label-position="top" size="large"
               :disabled="!allowRegister" @submit.prevent="onSubmit">
        <el-form-item label="邮箱" prop="email">
          <el-input v-model="form.email" placeholder="you@example.com" autocomplete="username" />
        </el-form-item>
        <el-form-item label="昵称" prop="nickname">
          <el-input v-model="form.nickname" placeholder="选填" />
        </el-form-item>
        <el-form-item label="密码" prop="password">
          <el-input v-model="form.password" type="password" show-password autocomplete="new-password" />
        </el-form-item>
        <el-form-item label="确认密码" prop="confirm">
          <el-input v-model="form.confirm" type="password" show-password autocomplete="new-password"
                    @keyup.enter="onSubmit" />
        </el-form-item>
        <el-button type="primary" class="submit" :loading="loading" :disabled="!allowRegister"
                   @click="onSubmit">
          注册
        </el-button>
        <div class="foot">
          已有账号?<router-link to="/login">直接登录</router-link>
        </div>
      </el-form>
    </el-card>
  </div>
</template>

<style scoped lang="scss">
.register-page {
  min-height: 100vh;
  position: relative;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: clamp(24px, 3.6vw, 44px) 24px;
  box-sizing: border-box;
  overflow-x: hidden;
  background: linear-gradient(135deg,#eef5ff,#f9fffb);
  background:
    radial-gradient(760px 360px at 18% 18%, rgba(37,99,235,.16), transparent 62%),
    radial-gradient(620px 300px at 82% 72%, rgba(20,184,166,.18), transparent 62%),
    linear-gradient(135deg, #f7faff, #eef4ff);
}
:global(html.dark .register-page) {
  background:
    radial-gradient(760px 360px at 18% 18%, rgba(37,99,235,.18), transparent 62%),
    radial-gradient(620px 300px at 82% 72%, rgba(20,184,166,.14), transparent 62%),
    var(--lc-bg);
}
:global(html.dark .register-stage) {
  border-color: var(--lc-border);
  background:
    radial-gradient(520px 320px at 72% 22%, rgba(20,184,166,.14), transparent 64%),
    radial-gradient(420px 260px at 28% 80%, rgba(37,99,235,.18), transparent 62%),
    rgba(17, 24, 39, .76);
}
.register-stage {
  position: relative;
  width: min(1120px, 100%);
  min-height: min(710px, calc(100vh - 88px));
  padding: clamp(28px, 4vw, 56px);
  box-sizing: border-box;
  overflow: hidden;
  border: 1px solid rgba(221, 229, 242, .9);
  border-radius: 30px;
  background:
    radial-gradient(520px 320px at 72% 22%, rgba(20,184,166,.22), transparent 64%),
    radial-gradient(420px 260px at 28% 80%, rgba(37,99,235,.16), transparent 62%),
    rgba(255, 255, 255, .42);
  box-shadow: 0 28px 90px rgba(15, 23, 42, .12);
  backdrop-filter: blur(20px);
  -webkit-backdrop-filter: blur(20px);

  h1 {
    max-width: 520px;
    margin: 78px 0 16px;
    color: var(--lc-text);
    font-size: clamp(30px, 4vw, 48px);
    line-height: 1.14;
    letter-spacing: 0;
  }

  p {
    max-width: 440px;
    margin: 0;
    color: var(--lc-muted);
    font-size: 15px;
    line-height: 1.8;
  }
}
.stage-brand {
  display: flex;
  align-items: center;
  gap: 12px;

  b, small { display: block; }
  b { color: var(--lc-text); font-size: 17px; line-height: 22px; }
  small { color: var(--lc-muted); font-size: 12px; line-height: 16px; }
  .mark { width: 42px; height: 42px; border-radius: 14px; }
}
.stage-preview {
  width: min(540px, 56%);
  margin-top: 34px;
  padding: 18px;
  border: 1px solid var(--lc-border);
  border-radius: 24px;
  background: rgba(255,255,255,.72);
  box-shadow: 0 20px 54px rgba(15, 23, 42, .14);
}
:global(html.dark .stage-preview) {
  border-color: var(--lc-border);
  background: rgba(17, 24, 39, .9);
}
.preview-toolbar {
  display: flex;
  gap: 7px;
  margin-bottom: 14px;

  span {
    width: 10px;
    height: 10px;
    border-radius: 999px;
    background: var(--lc-border);

    &:first-child { background: var(--lc-orange); }
    &:nth-child(2) { background: var(--lc-amber); }
    &:last-child { background: var(--lc-mint); }
  }
}
.preview-cover {
  min-height: 178px;
  display: flex;
  flex-direction: column;
  justify-content: flex-end;
  padding: 22px;
  border-radius: 18px;
  color: #fff;
  background:
    linear-gradient(180deg, rgba(15,23,42,.02), rgba(15,23,42,.42)),
    radial-gradient(circle at 52% 36%, rgba(245,158,11,.5), transparent 16%),
    linear-gradient(135deg, #dbeafe, #ccfbf1);

  strong { font-size: 30px; line-height: 1.2; }
  em { margin-top: 8px; font-style: normal; opacity: .88; }
}
.preview-grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 10px;
  margin-top: 12px;

  i {
    height: 74px;
    border: 1px solid var(--lc-border-soft);
    border-radius: 14px;
    background:
      repeating-linear-gradient(45deg, #f8fafc 0 10px, #eef2f7 10px 20px);
  }
}
:global(html.dark .preview-grid i) {
  border-color: var(--lc-border);
  background:
    repeating-linear-gradient(45deg, var(--lc-surface-soft) 0 10px, var(--lc-surface) 10px 20px);
}
.form-card {
  position: absolute;
  z-index: 4;
  top: 50%;
  right: max(44px, calc((100vw - 1120px) / 2 + 58px));
  transform: translateY(-48%);
  width: 100%;
  max-width: 432px;
  border: 1px solid rgba(255,255,255,.72);
  border-radius: 24px;
  background: rgba(255,255,255,.88);
  box-shadow: 0 30px 80px rgba(15, 23, 42, .22);
  backdrop-filter: blur(22px);
  -webkit-backdrop-filter: blur(22px);
  :deep(.el-card__body) { padding: 30px; }
}
:global(html.dark .form-card) {
  border-color: var(--lc-border);
  background: rgba(17, 24, 39, .9);
}
.form-title { font-size: 24px; font-weight: 800; margin-bottom: 6px; letter-spacing: 0; }
.form-sub { color: var(--el-text-color-secondary); margin-bottom: 20px; font-size: 14px; line-height: 1.7; }
.submit { width: 100%; }
.foot { margin-top: 16px; text-align: center; font-size: 13px; color: var(--el-text-color-secondary); }
.brand-line {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-bottom: 22px;
  b, small { display: block; }
  b { color: var(--lc-text); font-size: 16px; line-height: 20px; }
  small { color: var(--lc-muted); font-size: 12px; line-height: 16px; }
}
.mark {
  width: 34px;
  height: 34px;
  border-radius: 12px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  color: #fff;
  background: linear-gradient(135deg,var(--lc-primary),var(--lc-mint));
  font-weight: 800;
}

@media (max-width: 640px) {
  .register-page {
    min-height: 100dvh;
    padding: 16px;
  }
  .register-stage {
    position: absolute;
    inset: 16px;
    width: auto;
    min-height: auto;
    padding: 22px;
    border-radius: 24px;

    .stage-brand,
    h1,
    p,
    .stage-preview { display: none; }
  }
  .form-card {
    position: relative;
    top: auto;
    right: auto;
    max-width: 100%;
    transform: none;
    :deep(.el-card__body) { padding: 24px; }
  }
}

@media (min-width: 641px) and (max-width: 960px) {
  .register-page { padding: 24px 20px; }
  .register-stage {
    min-height: calc(100vh - 48px);
    padding: 28px;

    h1,
    p { display: none; }
  }
  .stage-preview {
    width: min(560px, 100%);
    margin-top: 24px;
    opacity: .38;
  }
  .form-card {
    left: 50%;
    right: auto;
    width: min(432px, calc(100vw - 48px));
    transform: translate(-50%, -44%);
  }
}
</style>
