<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { storeToRefs } from 'pinia'
import { useUserStore } from '@/stores/user'
import { useUIStore } from '@/stores/ui'
import { useSiteStore } from '@/stores/site'
import { APP_VERSION } from '@/version'
import type { MenuItem } from '@/api/auth'

const store = useUserStore()
const ui = useUIStore()
const site = useSiteStore()
const router = useRouter()
const route = useRoute()

const siteName = computed(() => site.get('site.name', '灵境智创'))
const siteLogo = computed(() => site.get('site.logo_url', ''))
const siteFooter = computed(() => site.get('site.footer', ''))

const { menu, user, role, permissions } = storeToRefs(store)
const collapsed = ref(false)      // 桌面端折叠状态
const drawerOpen = ref(false)     // 移动端抽屉展开状态
const isMobile = ref(false)
const loadingMenu = ref(false)

const MOBILE_BP = 768

function checkMobile() {
  const mobile = window.innerWidth < MOBILE_BP
  if (mobile !== isMobile.value) {
    isMobile.value = mobile
    if (!mobile) drawerOpen.value = false  // 切换到桌面时自动关抽屉
  }
}

// 顶栏汉堡按钮行为:移动端控制抽屉,桌面端控制折叠
function toggleSidebar() {
  if (isMobile.value) {
    drawerOpen.value = !drawerOpen.value
  } else {
    collapsed.value = !collapsed.value
  }
}

// 侧栏图标:移动端始终用 Menu 图标,桌面端跟随折叠状态
const menuIcon = computed(() => {
  if (isMobile.value) return 'Menu'
  return collapsed.value ? 'Expand' : 'Fold'
})

// 侧栏实际是否折叠(移动端抽屉展开时不折叠)
const sideCollapsed = computed(() => isMobile.value ? false : collapsed.value)

// 侧栏宽度(桌面端动态;移动端固定 252px 由 CSS 管理)
const asideWidth = computed(() => isMobile.value ? '0px' : (collapsed.value ? '72px' : '236px'))

const activePath = computed(() => route.path)

const titleMap = computed(() => {
  const m = new Map<string, string>()
  function walk(items: MenuItem[]) {
    for (const it of items) {
      if (it.path) m.set(it.path, it.title)
      if (it.children) walk(it.children)
    }
  }
  walk(menu.value)
  return m
})

const currentTitle = computed(() => titleMap.value.get(activePath.value) || (route.meta.title as string) || '')

async function loadMenu() {
  loadingMenu.value = true
  try {
    await store.fetchMenu()
  } finally {
    loadingMenu.value = false
  }
}

async function logout() {
  await store.logout()
  router.replace('/login')
}

function goto(path?: string) {
  if (path) router.push(path)
}

// 路由切换时关闭移动端抽屉
watch(() => route.path, () => {
  if (isMobile.value) drawerOpen.value = false
})

onMounted(() => {
  checkMobile()
  window.addEventListener('resize', checkMobile)
  loadMenu()
})
onUnmounted(() => window.removeEventListener('resize', checkMobile))
watch(() => store.isLoggedIn, (v) => { if (v) loadMenu() })
</script>

<template>
  <el-container class="layout-root">
    <!-- 移动端遮罩层 -->
    <transition name="overlay-fade">
      <div v-if="isMobile && drawerOpen" class="sidebar-overlay" @click="drawerOpen = false" />
    </transition>

    <!-- 侧栏:桌面 inline / 移动端 fixed drawer -->
    <el-aside
      :width="asideWidth"
      class="sidebar"
      :class="{ 'sidebar-mobile': isMobile, 'sidebar-open': isMobile && drawerOpen }"
    >
      <div class="logo" :class="{ 'has-site-logo': siteLogo, 'logo-collapsed': sideCollapsed }">
        <img v-if="siteLogo" :src="siteLogo" class="logo-img" alt="logo" />
        <span v-else class="mark">{{ (siteName[0] || 'G').toUpperCase() }}</span>
        <span v-if="!sideCollapsed && !siteLogo" class="title">
          <b>{{ siteName }}</b>
          <small>电商 AI 工作台</small>
        </span>
      </div>
      <el-menu
        :default-active="activePath"
        :collapse="sideCollapsed"
        background-color="transparent"
        :text-color="'var(--gp-menu-text)'"
        :active-text-color="'var(--lc-primary)'"
        popper-class="layout-menu-popper"
        class="side-menu"
        router
      >
        <template v-for="group in menu" :key="group.key">
          <el-menu-item v-if="!group.children?.length && group.path" :index="group.path">
            <el-icon v-if="group.icon"><component :is="group.icon" /></el-icon>
            <template #title>{{ group.title }}</template>
          </el-menu-item>
          <el-sub-menu v-else-if="group.children?.length" :index="group.key" popper-class="layout-menu-popper">
            <template #title>
              <el-icon v-if="group.icon"><component :is="group.icon" /></el-icon>
              <span>{{ group.title }}</span>
            </template>
            <el-menu-item
              v-for="child in group.children"
              :key="child.key"
              :index="child.path!"
            >
              <el-icon v-if="child.icon"><component :is="child.icon" /></el-icon>
              <template #title>{{ child.title }}</template>
            </el-menu-item>
          </el-sub-menu>
        </template>
      </el-menu>

      <div class="sidebar-version" :class="{ collapsed: sideCollapsed }">
        <span class="ver-text">{{ sideCollapsed ? APP_VERSION.replace('v','') : APP_VERSION }}</span>
      </div>
    </el-aside>

    <el-container class="right-container">
      <el-header class="topbar">
        <div class="left">
          <el-button link @click="toggleSidebar">
            <el-icon :size="18"><component :is="menuIcon" /></el-icon>
          </el-button>
          <span class="crumb">{{ currentTitle }}</span>
        </div>
        <div class="right">
          <el-tooltip :content="ui.isDark ? '切换到亮色' : '切换到暗色'" placement="bottom">
            <el-button link class="theme-btn" @click="ui.toggleDark()">
              <el-icon :size="18">
                <component :is="ui.isDark ? 'Sunny' : 'Moon'" />
              </el-icon>
            </el-button>
          </el-tooltip>
          <el-dropdown trigger="click" @command="(c: string) => c === 'logout' ? logout() : goto(c)">
            <span class="user-entry">
              <el-avatar :size="28" style="background:linear-gradient(135deg,#2563eb,#14b8a6)">
                {{ (user?.nickname || user?.email || 'U').slice(0, 1).toUpperCase() }}
              </el-avatar>
              <span class="nick">{{ user?.nickname || user?.email }}</span>
              <el-tag v-if="role === 'admin' && !isMobile" type="warning" size="small">管理员</el-tag>
              <el-icon><ArrowDown /></el-icon>
            </span>
            <template #dropdown>
              <el-dropdown-menu>
                <el-dropdown-item command="/personal/dashboard">
                  <el-icon><User /></el-icon> 个人中心
                </el-dropdown-item>
                <el-dropdown-item command="/personal/billing">
                  <el-icon><Wallet /></el-icon> 账单
                </el-dropdown-item>
                <el-dropdown-item divided command="logout">
                  <el-icon><SwitchButton /></el-icon> 退出登录
                </el-dropdown-item>
              </el-dropdown-menu>
            </template>
          </el-dropdown>
        </div>
      </el-header>

      <el-main class="main" v-loading="loadingMenu">
        <router-view v-slot="{ Component }">
          <transition name="fade" mode="out-in">
            <component :is="Component" :key="route.fullPath" />
          </transition>
        </router-view>
      </el-main>

      <el-footer v-if="siteFooter" class="footer">
        <div class="footer-line footer-custom">{{ siteFooter }}</div>
      </el-footer>
    </el-container>
  </el-container>
</template>

<style scoped lang="scss">
// ─── 根容器 ──────────────────────────────────────────────────────────────────
.layout-root {
  height: 100vh;
  overflow: hidden;
  color: var(--lc-text);
  background:
    radial-gradient(900px 420px at 18% -12%, rgba(37, 99, 235, .12), transparent 62%),
    radial-gradient(760px 360px at 96% 0%, rgba(20, 184, 166, .12), transparent 60%),
    var(--lc-bg);
}

.right-container { min-width: 0; flex: 1; overflow: hidden; }

// ─── 侧栏 ────────────────────────────────────────────────────────────────────
.sidebar {
  background: var(--gp-sidebar-bg);
  border-right: 1px solid var(--lc-border);
  backdrop-filter: blur(18px);
  transition: width .22s ease;
  overflow-x: hidden;
  display: flex !important;
  flex-direction: column;
  .side-menu {
    flex: 1;
    overflow-y: auto;
    overflow-x: hidden;
  }
}

// 移动端：侧栏脱离文档流变成 fixed overlay drawer
.sidebar-mobile {
  position: fixed !important;
  left: 0;
  top: 0;
  height: 100vh;
  width: 252px !important;  // 覆盖 :width 绑定
  z-index: 1001;
  transform: translateX(-100%);
  transition: transform .25s ease, box-shadow .25s ease;
  box-shadow: none;
}
.sidebar-mobile.sidebar-open {
  transform: translateX(0);
  box-shadow: 18px 0 48px rgba(15, 23, 42, 0.18);
}

// 移动端遮罩
.sidebar-overlay {
  position: fixed;
  inset: 0;
  background: rgba(15, 23, 42, 0.32);
  z-index: 1000;
}
.overlay-fade-enter-active, .overlay-fade-leave-active { transition: opacity .25s; }
.overlay-fade-enter-from, .overlay-fade-leave-to { opacity: 0; }

// ─── Logo ─────────────────────────────────────────────────────────────────────
.logo {
  min-height: 72px;
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 0 18px;
  color: var(--lc-text);
  font-weight: 760;
  letter-spacing: 0;
  flex-shrink: 0;
  .logo-img {
    width: 32px; height: 32px; border-radius: 10px;
    object-fit: contain; background: #fff;
  }
  .mark {
    display: inline-flex;
    width: 34px; height: 34px;
    border-radius: 12px;
    background: linear-gradient(135deg,var(--lc-primary),var(--lc-mint));
    align-items: center; justify-content: center;
    color: #fff;
    font-size: 14px;
    flex-shrink: 0;
  }
  .title {
    display: grid;
    gap: 2px;
    min-width: 0;
    white-space: nowrap;
    overflow: hidden;
    b {
      overflow: hidden;
      text-overflow: ellipsis;
      font-size: 16px;
      line-height: 20px;
    }
    small {
      overflow: hidden;
      text-overflow: ellipsis;
      color: var(--lc-muted);
      font-size: 12px;
      font-weight: 500;
      line-height: 16px;
    }
  }
}
.logo.has-site-logo {
  padding: 0 12px;
  .logo-img {
    width: 164px;
    max-width: 100%;
    height: 38px;
    border-radius: 8px;
    object-fit: contain;
    background: transparent;
    flex-shrink: 0;
  }
}
.logo.logo-collapsed.has-site-logo {
  justify-content: center;
  padding: 0 10px;
  .logo-img {
    width: 38px;
    height: 38px;
  }
}

.side-menu {
  border-right: none !important;
  padding: 0 12px 12px;
  background: transparent !important;
  --el-menu-hover-bg-color: var(--gp-menu-hover-bg);
  --el-menu-text-color: var(--gp-menu-text);
  --el-menu-active-color: var(--lc-primary);
  :deep(.el-menu-item),
  :deep(.el-sub-menu__title) {
    height: 42px;
    margin: 3px 0;
    border-radius: 12px;
    color: var(--gp-menu-text);
    font-weight: 650;
  }
  :deep(.el-menu-item:hover),
  :deep(.el-sub-menu__title:hover) {
    color: var(--lc-text);
    background: var(--gp-menu-hover-bg);
  }
  :deep(.el-menu-item.is-active) {
    position: relative;
    color: var(--lc-primary);
    background: var(--gp-menu-active-bg);
  }
  :deep(.el-menu-item.is-active::before) {
    content: '';
    position: absolute;
    left: 0;
    top: 10px;
    bottom: 10px;
    width: 3px;
    border-radius: 999px;
    background: var(--lc-primary);
  }
  :deep(.el-sub-menu .el-menu-item) {
    min-width: 0;
    padding-left: 42px !important;
  }
  :deep(.el-icon) {
    color: inherit;
  }
}

// ─── 顶栏 ─────────────────────────────────────────────────────────────────────
.topbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  height: 60px;
  min-height: 60px;
  background: var(--gp-topbar-bg);
  color: var(--lc-text);
  border-bottom: 1px solid var(--lc-border);
  padding: 0 22px;
  backdrop-filter: blur(18px);
  flex-shrink: 0;
  .left { display: flex; align-items: center; gap: 10px; min-width: 0; }
  .crumb {
    font-size: 16px;
    font-weight: 720;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .user-entry {
    display: inline-flex;
    align-items: center;
    gap: 8px;
    cursor: pointer;
    color: var(--lc-text);
    .nick {
      font-size: 14px;
      max-width: 120px;
      overflow: hidden;
      text-overflow: ellipsis;
      white-space: nowrap;
    }
  }
  .right {
    display: inline-flex;
    align-items: center;
    gap: 10px;
    flex-shrink: 0;
  }
  .theme-btn { padding: 0 6px; }
}

// ─── 主区 ─────────────────────────────────────────────────────────────────────
.main {
  background: var(--gp-bg);
  padding: 0;
  overflow-y: auto;
  overflow-x: hidden;
}

// ─── 页脚 ─────────────────────────────────────────────────────────────────────
.footer {
  background: transparent;
  text-align: center;
  color: var(--el-text-color-secondary);
  font-size: 12px;
  padding: 6px 12px;
  height: auto;
  min-height: 36px;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 2px;
  flex-shrink: 0;
}
.footer-line { line-height: 1.6; }
.footer-custom { color: var(--el-text-color-placeholder); font-size: 11px; }

// ─── 过渡 ─────────────────────────────────────────────────────────────────────
.fade-enter-active, .fade-leave-active { transition: opacity .15s; }
.fade-enter-from, .fade-leave-to { opacity: 0; }

// ─── 版本号 ───────────────────────────────────────────────────────────────────
.sidebar-version {
  position: sticky;
  bottom: 0;
  padding: 10px 16px;
  text-align: center;
  border-top: 1px solid var(--lc-border-soft);
  background: var(--gp-sidebar-bg);
  flex-shrink: 0;
  .ver-text {
    display: inline-block;
    font-size: 11px;
    color: var(--lc-subtle);
    letter-spacing: 0.5px;
    user-select: none;
    white-space: nowrap;
  }
  &.collapsed .ver-text { font-size: 9px; letter-spacing: 0; }
}

// ─── 移动端补丁 ───────────────────────────────────────────────────────────────
@media (max-width: 767px) {
  .topbar {
    padding: 0 12px;
    .crumb { font-size: 14px; }
    .nick { display: none; }
  }
  .footer { display: none; }  // 移动端页脚占空间,隐藏
}
</style>
