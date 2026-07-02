<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { useUserStore } from '@/stores/user'
import { useUIStore } from '@/stores/ui'
import { useSiteStore } from '@/stores/site'

const router = useRouter()
const user = useUserStore()
const ui = useUIStore()
const site = useSiteStore()

const siteName = computed(() => site.get('site.name', '灵境智创'))
const siteLogo = computed(() => site.get('site.logo_url', ''))
const siteFooter = computed(() => site.get('site.footer', ''))
const allowRegister = computed(() => site.allowRegister())
const loggedIn = computed(() => user.isLoggedIn)

function goCreate() {
  if (loggedIn.value) router.push('/personal/ecommerce-v2')
  else router.push('/login?redirect=/personal/ecommerce-v2')
}
function goPlay() {
  if (loggedIn.value) router.push('/personal/play')
  else router.push('/login?redirect=/personal/play')
}
function goDashboard() { router.push('/personal/dashboard') }
function goLogin() { router.push('/login') }
function goRegister() { router.push('/register') }
function scrollTop() { window.scrollTo({ top: 0, behavior: 'smooth' }) }

// 滚动监听,nav 加实体背景
const scrolled = ref(false)
onMounted(() => {
  const onScroll = () => { scrolled.value = window.scrollY > 24 }
  window.addEventListener('scroll', onScroll, { passive: true })
  onScroll()
})

// 工具货架：围绕电商创作任务组织能力
const features = [
  {
    icon: 'ShoppingBag',
    color: '#2563EB',
    title: '电商智能体',
    desc: '输入商品资料、卖点、规格和平台要求，统一生成商品图、短视频、Listing 文案和详情页结构。',
  },
  {
    icon: 'VideoPlay',
    color: '#14B8A6',
    title: '商品短视频',
    desc: '把已生成的商品素材继续变成 9:16 短视频，展示提交、排队、生成中、完成的全过程。',
  },
  {
    icon: 'Collection',
    color: '#F59E0B',
    title: '素材资产库',
    desc: '沉淀商品、模特、参考图和生成结果，让团队复用可控资产，而不是每次从零开始。',
  },
]
</script>

<template>
  <div class="landing" :class="{ dark: ui.isDark }">
    <!-- ============= 顶部导航 ============= -->
    <header class="nav" :class="{ scrolled }">
      <div class="nav-inner">
        <a class="logo" @click="scrollTop">
          <img v-if="siteLogo" :src="siteLogo" class="logo-img" alt="logo" />
          <span v-else class="logo-mark">{{ (siteName[0] || 'G').toUpperCase() }}</span>
          <span class="logo-name">{{ siteName }}</span>
        </a>
        <nav class="menu">
          <a @click="goCreate">电商智能体</a>
          <a @click="goPlay">在线体验</a>
          <a @click="router.push('/login?redirect=/personal/ecommerce-assets')">资产库</a>
          <a @click="router.push('/login?redirect=/personal/docs')">接口文档</a>
        </nav>
        <div class="nav-actions">
          <el-button
            link :title="ui.isDark ? '切换到亮色' : '切换到暗色'"
            class="theme-btn" @click="ui.toggleDark()"
          >
            <el-icon :size="18"><component :is="ui.isDark ? 'Sunny' : 'Moon'" /></el-icon>
          </el-button>
          <template v-if="!loggedIn">
            <el-button text class="btn-login" @click="goLogin">登录</el-button>
            <el-button v-if="allowRegister" type="primary" round @click="goRegister">免费注册</el-button>
          </template>
          <template v-else>
            <el-button type="primary" round @click="goDashboard">
              进入控制台 <el-icon><ArrowRight /></el-icon>
            </el-button>
          </template>
        </div>
      </div>
    </header>

    <!-- ============= Hero:AI 电商创作工作台 ============= -->
    <section id="hero" class="hero">
      <div class="hero-bg"></div>
      <div class="hero-inner">
        <div class="hero-text">
          <div class="eyebrow">
            <span class="dot"></span>
            AI Commerce Workspace
          </div>
          <h1 class="hero-title">
            电商 AI<br/>
            创作工作台
          </h1>
          <p class="hero-sub">
            把商品资料变成主图、短视频、Listing 文案和详情页结构。<br/>
            一个入口完成从素材到交付的生成流程。
          </p>
          <div class="hero-cta">
            <el-button size="large" type="primary" round @click="goCreate">
              <el-icon><MagicStick /></el-icon> 开始创建
            </el-button>
            <el-button size="large" round @click="goPlay">
              查看在线体验
            </el-button>
          </div>
        </div>

        <div class="hero-workbench">
          <div class="prompt-tabs">
            <span class="active">商品主图</span>
            <span>商品短视频</span>
            <span>Listing 文案</span>
            <span>A+ 详情页</span>
          </div>
          <div class="prompt-card">
            <p>316 不锈钢煎锅，适合小红书和亚马逊，突出不粘、轻烟、轻量手柄。生成主图、短视频和五点描述。</p>
            <div class="prompt-actions">
              <span>上传参考图</span>
              <span>选择模特</span>
              <span>套用模板</span>
              <el-button type="primary" @click="goCreate">开始生成</el-button>
            </div>
          </div>
          <div class="result-strip">
            <article>
              <div class="preview-thumb"></div>
              <b>商品主图</b>
              <span>4 张待交付</span>
            </article>
            <article>
              <div class="preview-thumb video"></div>
              <b>短视频</b>
              <span>15 秒预览</span>
            </article>
            <article>
              <div class="preview-thumb copy"></div>
              <b>Listing 文案</b>
              <span>中文 / English</span>
            </article>
          </div>
        </div>
      </div>
    </section>

    <!-- ============= 工具货架 ============= -->
    <section class="section features">
      <div class="feature-grid">
        <div v-for="f in features" :key="f.title" class="feature-card">
          <div class="feature-icon" :style="{ background: f.color + '1A', color: f.color }">
            <el-icon :size="22"><component :is="f.icon" /></el-icon>
          </div>
          <div class="feature-title">{{ f.title }}</div>
          <div class="feature-desc" v-html="f.desc"></div>
        </div>
      </div>
      <div class="features-cta">
        <el-button size="large" type="primary" round @click="goCreate">
          打开电商智能体 <el-icon><ArrowRight /></el-icon>
        </el-button>
      </div>
    </section>

    <!-- ============= Footer(极简) ============= -->
    <footer v-if="siteFooter" class="footer">
      <div class="footer-inner">
        <span>{{ siteFooter }}</span>
      </div>
    </footer>
  </div>
</template>

<style scoped lang="scss">
// ========= 全局色变量(同时适配亮 / 暗) =========
.landing {
  --lp-bg: #ffffff;
  --lp-bg-soft: var(--lc-bg);
  --lp-text: var(--lc-text);
  --lp-text-soft: var(--lc-muted);
  --lp-text-mute: var(--lc-subtle);
  --lp-border: var(--lc-border);
  --lp-card: rgba(255, 255, 255, 0.72);
  --lp-card-solid: #ffffff;
  --lp-nav-bg: rgba(255, 255, 255, 0.78);
  --lp-nav-border: var(--lc-border-soft);

  min-height: 100vh;
  background: var(--lp-bg);
  color: var(--lp-text);
  font-family: -apple-system, BlinkMacSystemFont, 'PingFang SC', 'Microsoft YaHei',
               'Helvetica Neue', Arial, 'Segoe UI', sans-serif;
  line-height: 1.6;
  display: flex;
  flex-direction: column;
}
.landing.dark {
  --lp-bg: #0b1220;
  --lp-bg-soft: #0f1a2e;
  --lp-text: #e6e9ef;
  --lp-text-soft: #b3b7c3;
  --lp-text-mute: #7a8096;
  --lp-border: rgba(255, 255, 255, 0.08);
  --lp-card: rgba(255, 255, 255, 0.04);
  --lp-card-solid: #111a2b;
  --lp-nav-bg: rgba(15, 22, 38, 0.72);
  --lp-nav-border: rgba(255, 255, 255, 0.07);
}

// ========= 顶部导航 =========
.nav {
  position: sticky;
  top: 0;
  z-index: 50;
  padding: 14px 0;
  background: transparent;
  border-bottom: 1px solid transparent;
  transition: background .25s, border-color .25s, box-shadow .25s, padding .25s;
  backdrop-filter: blur(0);
}
.nav.scrolled {
  background: var(--lp-nav-bg);
  border-bottom-color: var(--lp-nav-border);
  backdrop-filter: saturate(180%) blur(12px);
  -webkit-backdrop-filter: saturate(180%) blur(12px);
  padding: 10px 0;
  box-shadow: 0 4px 24px rgba(15, 23, 42, 0.04);
}
.nav-inner {
  max-width: 1240px;
  margin: 0 auto;
  padding: 0 24px;
  display: flex;
  align-items: center;
  gap: 28px;
}
.logo {
  display: inline-flex;
  align-items: center;
  gap: 10px;
  cursor: pointer;
  text-decoration: none;
  color: var(--lp-text);
  .logo-img { width: 32px; height: 32px; border-radius: 8px; object-fit: contain; }
  .logo-mark {
    width: 34px; height: 34px; border-radius: 12px;
    display: inline-flex; align-items: center; justify-content: center;
    color: #fff; font-weight: 800; font-size: 15px;
    background: linear-gradient(135deg, var(--lc-primary), var(--lc-mint));
    box-shadow: 0 8px 18px rgba(37, 99, 235, .18);
  }
  .logo-name { font-size: 17px; font-weight: 700; letter-spacing: 0.3px; }
}
.menu {
  display: flex;
  gap: 22px;
  flex: 1;
  a {
    color: var(--lp-text-soft);
    font-size: 14px;
    cursor: pointer;
    text-decoration: none;
    display: inline-flex;
    align-items: center;
    gap: 4px;
    transition: color .2s;
    .ext { opacity: .7; }
  }
  a:hover { color: #409eff; }
}
.nav-actions {
  display: inline-flex;
  align-items: center;
  gap: 10px;
  .theme-btn { padding: 4px 8px; }
  .btn-login { font-weight: 600; }
}

// ========= Hero =========
.hero {
  position: relative;
  overflow: hidden;
  padding: 74px 24px 58px;
}
.hero-bg {
  position: absolute;
  inset: -10% -10% 0 -10%;
  pointer-events: none;
  background:
    radial-gradient(900px 420px at 15% 25%, rgba(37, 99, 235, .14), transparent 62%),
    radial-gradient(800px 420px at 85% 15%, rgba(20, 184, 166, .16), transparent 62%),
    linear-gradient(180deg, #ffffff, #f8fbff);
  z-index: 0;
}
.landing.dark .hero-bg {
  background:
    radial-gradient(900px 420px at 15% 25%, #1b3a6a88, transparent 60%),
    radial-gradient(800px 420px at 85% 15%, #4b2a7066, transparent 60%),
    radial-gradient(600px 400px at 70% 90%, #1c4c2666, transparent 60%);
}
.hero-inner {
  position: relative;
  z-index: 1;
  max-width: 1240px;
  margin: 0 auto;
  display: grid;
  grid-template-columns: .92fr 1.08fr;
  gap: 56px;
  align-items: center;
}
.eyebrow {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  padding: 6px 12px;
  border-radius: 999px;
  border: 1px solid var(--lp-border);
  background: var(--lc-primary-soft);
  backdrop-filter: blur(6px);
  font-size: 12px;
  color: var(--lc-primary);
  font-weight: 720;
  .dot {
    width: 6px; height: 6px; border-radius: 50%;
    background: var(--lc-mint);
    box-shadow: 0 0 0 4px rgba(20, 184, 166, .18);
  }
}
.hero-title {
  font-size: clamp(42px, 5.2vw, 64px);
  line-height: 1.1;
  margin: 22px 0 18px;
  font-weight: 800;
  letter-spacing: 0;
}
.hero-sub {
  font-size: 16px;
  color: var(--lp-text-soft);
  margin: 0 0 26px;
  b { color: var(--lp-text); font-weight: 600; }
}
.hero-cta {
  display: flex;
  flex-wrap: wrap;
  gap: 12px;
  margin-bottom: 24px;
}
.hero-meta {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px;
  font-size: 13px;
  color: var(--lp-text-mute);
  .meta-link {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    color: var(--lp-text-soft);
    text-decoration: none;
    padding: 4px 10px;
    border-radius: 6px;
    transition: background .2s;
  }
  .meta-link:hover { background: var(--lp-card); color: #409eff; }
  .dot-sep { color: var(--lp-text-mute); }
}

.hero-workbench {
  position: relative;
  border: 1px solid var(--lp-border);
  border-radius: 22px;
  background: rgba(255,255,255,.88);
  padding: 20px;
  box-shadow: var(--lc-shadow-popover);
}
.prompt-tabs {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin-bottom: 14px;
  span {
    min-height: 34px;
    display: inline-flex;
    align-items: center;
    border-radius: 10px;
    padding: 0 14px;
    color: var(--lp-text-soft);
    background: var(--lp-bg-soft);
    font-size: 13px;
    font-weight: 700;
  }
  .active {
    color: #fff;
    background: var(--lc-primary);
  }
}
.prompt-card {
  border: 1px solid #cfdaf0;
  border-radius: 16px;
  background: #fff;
  padding: 16px;
  p {
    min-height: 78px;
    color: #475569;
    font-size: 15px;
    line-height: 1.75;
  }
}
.prompt-actions {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-top: 14px;
  flex-wrap: wrap;
  span {
    height: 28px;
    border-radius: 999px;
    padding: 0 10px;
    display: inline-flex;
    align-items: center;
    color: var(--lp-text-soft);
    background: var(--lp-bg-soft);
    font-size: 12px;
  }
  .el-button { margin-left: auto; }
}
.result-strip {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 12px;
  margin-top: 16px;
  article {
    min-width: 0;
    border: 1px solid var(--lp-border);
    border-radius: 16px;
    background: linear-gradient(145deg, #fff, #f6faff);
    padding: 12px;
  }
  b, span {
    display: block;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  b { font-size: 13px; }
  span { margin-top: 2px; color: var(--lp-text-soft); font-size: 12px; }
}
.preview-thumb {
  height: 76px;
  border-radius: 12px;
  margin-bottom: 8px;
  background:
    linear-gradient(135deg, rgba(37,99,235,.18), rgba(20,184,166,.14)),
    repeating-linear-gradient(45deg, #f8fafc 0 10px, #eef2f7 10px 20px);
  &.video {
    background:
      radial-gradient(circle at 54% 38%, rgba(245,158,11,.55), transparent 18%),
      linear-gradient(135deg, #dbeafe, #ccfbf1);
  }
  &.copy {
    background:
      linear-gradient(#e2e8f0 0 0) 14px 18px / 70% 7px no-repeat,
      linear-gradient(#e2e8f0 0 0) 14px 36px / 56% 7px no-repeat,
      linear-gradient(#e2e8f0 0 0) 14px 54px / 82% 7px no-repeat,
      #f8fafc;
  }
}

// ========= 三张卖点卡 =========
.section {
  padding: 40px 24px 60px;
  position: relative;
}
.features {
  background:
    radial-gradient(800px 300px at 20% 10%, rgba(37, 99, 235, .07), transparent 60%),
    radial-gradient(800px 300px at 80% 90%, rgba(20, 184, 166, .09), transparent 60%);
}
.feature-grid {
  max-width: 1140px;
  margin: 0 auto;
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  gap: 18px;
}
.feature-card {
  background: var(--lp-card-solid);
  border: 1px solid var(--lp-border);
  border-radius: 16px;
  padding: 26px 24px;
  transition: transform .2s, box-shadow .2s, border-color .2s;
}
.feature-card:hover {
  transform: translateY(-4px);
  border-color: transparent;
  box-shadow:
    0 20px 40px -12px rgba(37, 99, 235, 0.16),
    0 8px 24px rgba(15, 23, 42, 0.08);
}
.feature-icon {
  width: 44px;
  height: 44px;
  border-radius: 12px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  margin-bottom: 14px;
}
.feature-title {
  font-size: 17px;
  font-weight: 700;
  margin-bottom: 8px;
}
.feature-desc {
  font-size: 14px;
  color: var(--lp-text-soft);
  line-height: 1.75;
  :deep(code) {
    padding: 1px 6px;
    border-radius: 4px;
    background: rgba(64, 158, 255, 0.12);
    color: #409eff;
    font-family: 'JetBrains Mono', Menlo, Consolas, monospace;
    font-size: 12px;
  }
  :deep(b) { color: var(--lp-text); font-weight: 600; }
}
.features-cta {
  text-align: center;
  margin-top: 40px;
}

// ========= Footer =========
.footer {
  margin-top: auto;
  background: var(--lp-bg-soft);
  border-top: 1px solid var(--lp-border);
  padding: 22px 24px;
}
.footer-inner {
  max-width: 1240px;
  margin: 0 auto;
  text-align: center;
  font-size: 12.5px;
  color: var(--lp-text-mute);
  a { color: var(--lp-text-soft); text-decoration: none; margin: 0 2px; }
  a:hover { color: #409eff; }
  .sep { margin: 0 8px; color: var(--lp-border); }
}

// ========= 响应式 =========
@media (max-width: 1100px) {
  .hero-inner { grid-template-columns: 1fr; }
  .hero-workbench { order: 2; margin-top: 30px; }
}
@media (max-width: 900px) {
  .feature-grid { grid-template-columns: 1fr; }
}
@media (max-width: 640px) {
  .hero { padding: 36px 20px 48px; }
  .section { padding: 28px 20px 48px; }
  .nav-inner { gap: 12px; }
  .nav-actions .btn-login { display: none; }
  .menu { display: none; }
  .hero-cta .el-button { width: 100%; }
  .result-strip { grid-template-columns: 1fr; }
  .prompt-actions .el-button { width: 100%; margin-left: 0; }
  .footer-inner { line-height: 2; }
}
</style>
