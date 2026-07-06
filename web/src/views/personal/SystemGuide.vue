<script setup lang="ts">
import { computed, ref } from 'vue'
import { useRouter } from 'vue-router'
import {
  Brush,
  ChatLineRound,
  Check,
  Collection,
  Compass,
  Connection,
  DataAnalysis,
  Document,
  Guide,
  Key,
  MagicStick,
  Menu as MenuIcon,
  Operation,
  Picture,
  Reading,
  ShoppingBag,
  Tickets,
  VideoPlay,
  Wallet,
} from '@element-plus/icons-vue'

const router = useRouter()
const activeTab = ref<'features' | 'design' | 'workflow'>('features')

const featureGroups = [
  {
    title: '创作与生成',
    desc: '面向商品运营的内容生产链路，从资料输入到图片、文案、视频和详情页交付。',
    icon: ShoppingBag,
    items: [
      { name: '电商智能体', text: '一站式生成商品主图、文案、详情页和短视频。', path: '/personal/ecommerce-v2' },
      { name: '电商画布', text: '用节点视角查看任务链路、上下游关系和素材状态。', path: '/personal/ecommerce-canvas' },
      { name: '在线体验', text: '直接测试文本、图片、图生图和视频生成能力。', path: '/personal/play' },
    ],
  },
  {
    title: '资产与复用',
    desc: '沉淀商品、模特、参考图和结果素材，减少重复上传和重复生成。',
    icon: Collection,
    items: [
      { name: '电商资产库', text: '管理商品资产、模特资产和已生成图片。', path: '/personal/ecommerce-assets' },
      { name: '参考图片', text: '在生成任务中复用参考图，控制风格、构图和主体。', path: '/personal/ecommerce-v2' },
      { name: '结果下载', text: '预览、下载并回收生成结果到后续任务。', path: '/personal/ecommerce-canvas' },
    ],
  },
  {
    title: '接口与用量',
    desc: '为开发者和运营人员提供 API Key、接口示例、扣费记录和趋势分析。',
    icon: Connection,
    items: [
      { name: 'API Keys', text: '在接口文档内创建和停用调用凭证，控制外部系统接入。', path: '/personal/docs?tab=keys' },
      { name: '接口文档', text: '查看 OpenAI 兼容接口示例和调用说明。', path: '/personal/docs' },
      { name: '使用记录', text: '查询请求明细、错误原因、扣费和图片任务。', path: '/personal/usage' },
    ],
  },
]

const designSections = [
  {
    title: '左侧菜单',
    icon: MenuIcon,
    points: ['按角色展示可用功能', '桌面端支持折叠', '移动端以抽屉方式打开'],
  },
  {
    title: '顶部栏',
    icon: Compass,
    points: ['显示当前页面名称', '提供明暗主题切换', '账号入口集中处理个人操作'],
  },
  {
    title: '工作区',
    icon: Operation,
    points: ['表单、画布、详情各自滚动', '高频操作按钮保持可见', '任务状态用标签和进度统一表达'],
  },
  {
    title: '弹窗与预览',
    icon: Picture,
    points: ['图片、视频独立预览', '内容超长时弹窗内部滚动', '下载、重试、关闭固定在底部'],
  },
]

const workflows = [
  {
    title: '新建电商任务',
    icon: MagicStick,
    steps: [
      '进入电商智能体或电商画布。',
      '填写商品资料、目标平台、文案语言和风格模板。',
      '选择商品资产、模特资产或上传参考图片。',
      '点击生成任务，等待文案、图片、视频和详情页陆续完成。',
      '在节点详情或右侧检查面板中预览、下载或重试结果。',
    ],
    action: '开始生成',
    path: '/personal/ecommerce-v2',
  },
  {
    title: '维护素材资产',
    icon: Collection,
    steps: [
      '进入电商资产库。',
      '按商品、模特或生成结果筛选素材。',
      '上传可复用素材，补充名称和用途说明。',
      '在新任务中选择已有素材，减少重复配置。',
    ],
    action: '打开资产库',
    path: '/personal/ecommerce-assets',
  },
  {
    title: '查看消耗和错误',
    icon: DataAnalysis,
    steps: [
      '进入使用记录。',
      '按类型、状态、时间筛选请求。',
      '查看扣费、Token、图片数量和错误信息。',
      '需要开发接入时，前往接口文档复制示例。',
    ],
    action: '查看用量',
    path: '/personal/usage',
  },
]

const quickLinks = [
  { label: '生成任务', path: '/personal/ecommerce-v2', icon: ShoppingBag },
  { label: '画布视图', path: '/personal/ecommerce-canvas', icon: Operation },
  { label: '在线体验', path: '/personal/play', icon: ChatLineRound },
  { label: '接口文档', path: '/personal/docs', icon: Document },
  { label: '充值账单', path: '/personal/billing', icon: Wallet },
  { label: 'API Key', path: '/personal/docs?tab=keys', icon: Key },
]

const currentSummary = computed(() => {
  if (activeTab.value === 'features') return '按能力域了解系统能做什么。'
  if (activeTab.value === 'design') return '理解控制台界面层级和交互规则。'
  return '按常见任务一步步完成操作。'
})

function go(path: string) {
  router.push(path)
}
</script>

<template>
  <div class="page-container guide-page">
    <section class="guide-hero card-block">
      <div class="hero-copy">
        <span class="kicker">System Guide</span>
        <h1>系统指南</h1>
        <p>快速了解系统功能、界面设计和用户操作路径。适合新用户上手，也适合团队统一培训口径。</p>
        <div class="hero-actions">
          <el-button type="primary" :icon="ShoppingBag" @click="go('/personal/ecommerce-v2')">开始创建</el-button>
          <el-button :icon="Reading" @click="activeTab = 'workflow'">查看操作指引</el-button>
        </div>
      </div>
      <div class="hero-panel">
        <div class="panel-head">
          <el-icon><Guide /></el-icon>
          <span>{{ currentSummary }}</span>
        </div>
        <div class="panel-grid">
          <button v-for="link in quickLinks" :key="link.path" type="button" @click="go(link.path)">
            <el-icon><component :is="link.icon" /></el-icon>
            <span>{{ link.label }}</span>
          </button>
        </div>
      </div>
    </section>

    <el-tabs v-model="activeTab" class="guide-tabs">
      <el-tab-pane label="系统功能" name="features">
        <div class="feature-grid">
          <article v-for="group in featureGroups" :key="group.title" class="feature-card card-block">
            <header>
              <span class="feature-icon"><el-icon><component :is="group.icon" /></el-icon></span>
              <div>
                <h2>{{ group.title }}</h2>
                <p>{{ group.desc }}</p>
              </div>
            </header>
            <div class="feature-list">
              <button v-for="item in group.items" :key="item.name" type="button" @click="go(item.path)">
                <b>{{ item.name }}</b>
                <span>{{ item.text }}</span>
              </button>
            </div>
          </article>
        </div>
      </el-tab-pane>

      <el-tab-pane label="界面设计" name="design">
        <div class="design-layout">
          <section class="card-block design-map">
            <header>
              <span class="kicker">Interface</span>
              <h2>控制台界面结构</h2>
              <p>系统以“菜单导航 + 顶部上下文 + 主工作区”的方式组织功能，避免一次展示过多信息。</p>
            </header>
            <div class="layout-board">
              <div class="layout-sidebar">菜单</div>
              <div class="layout-main">
                <div class="layout-topbar">顶部栏</div>
                <div class="layout-content">
                  <span>表单区</span>
                  <span>画布/列表</span>
                  <span>详情/预览</span>
                </div>
              </div>
            </div>
          </section>
          <section class="design-cards">
            <article v-for="section in designSections" :key="section.title" class="design-card card-block">
              <header>
                <el-icon><component :is="section.icon" /></el-icon>
                <h3>{{ section.title }}</h3>
              </header>
              <ul>
                <li v-for="point in section.points" :key="point">
                  <el-icon><Check /></el-icon>
                  <span>{{ point }}</span>
                </li>
              </ul>
            </article>
          </section>
        </div>
      </el-tab-pane>

      <el-tab-pane label="用户操作指引" name="workflow">
        <div class="workflow-grid">
          <article v-for="flow in workflows" :key="flow.title" class="workflow-card card-block">
            <header>
              <span class="feature-icon"><el-icon><component :is="flow.icon" /></el-icon></span>
              <h2>{{ flow.title }}</h2>
            </header>
            <ol>
              <li v-for="step in flow.steps" :key="step">
                <span>{{ step }}</span>
              </li>
            </ol>
            <el-button type="primary" plain @click="go(flow.path)">{{ flow.action }}</el-button>
          </article>
        </div>

        <section class="card-block tips-card">
          <header>
            <el-icon><Tickets /></el-icon>
            <div>
              <h2>使用建议</h2>
              <p>先沉淀素材资产，再批量创建任务；遇到单个节点失败时优先重试该节点，避免整条任务重复消耗。</p>
            </div>
          </header>
          <div class="tip-list">
            <span><Brush /> 风格模板用于稳定视觉调性</span>
            <span><VideoPlay /> 视频生成适合在文案和主图确认后执行</span>
            <span><DataAnalysis /> 使用记录可用于排查错误和核对扣费</span>
          </div>
        </section>
      </el-tab-pane>
    </el-tabs>
  </div>
</template>

<style scoped lang="scss">
.guide-page {
  --guide-gap: 14px;
  --guide-radius: 14px;
  color: var(--lc-text);
}

.guide-hero {
  display: grid;
  grid-template-columns: minmax(0, 1.15fr) minmax(320px, .85fr);
  gap: var(--guide-gap);
  align-items: stretch;
  margin-bottom: var(--guide-gap);
  overflow: hidden;
  background:
    radial-gradient(540px 220px at 82% 12%, rgba(20, 184, 166, .12), transparent 62%),
    radial-gradient(640px 260px at 12% 0%, rgba(37, 99, 235, .10), transparent 60%),
    var(--lc-surface);
}

.hero-copy {
  min-width: 0;

  h1 {
    margin: 8px 0 8px;
    font-size: 26px;
    line-height: 34px;
    letter-spacing: 0;
  }

  p {
    max-width: 720px;
    margin: 0;
    color: var(--lc-muted);
    font-size: 14px;
    line-height: 22px;
  }
}

.kicker {
  display: inline-flex;
  align-items: center;
  height: 24px;
  padding: 0 9px;
  border-radius: 999px;
  color: var(--lc-primary);
  background: var(--lc-primary-soft);
  font-size: 12px;
  font-weight: 800;
}

.hero-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin-top: 14px;
}

.hero-panel {
  display: grid;
  grid-template-rows: auto 1fr;
  gap: 10px;
  min-width: 0;
  padding: 12px;
  border: 1px solid var(--lc-border);
  border-radius: var(--guide-radius);
  background: color-mix(in srgb, var(--lc-surface-soft) 82%, transparent);
}

.panel-head {
  display: flex;
  align-items: center;
  gap: 8px;
  color: var(--lc-muted);
  font-size: 13px;

  .el-icon {
    color: var(--lc-primary);
  }
}

.panel-grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 8px;

  button {
    min-width: 0;
    height: 54px;
    display: grid;
    place-items: center;
    gap: 4px;
    border: 1px solid var(--lc-border);
    border-radius: 10px;
    color: var(--lc-text);
    background: var(--lc-surface);
    cursor: pointer;
    transition: border-color .18s ease, color .18s ease, background .18s ease;

    &:hover {
      border-color: color-mix(in srgb, var(--lc-primary) 42%, var(--lc-border));
      color: var(--lc-primary);
      background: var(--lc-primary-soft);
    }

    .el-icon {
      font-size: 17px;
    }

    span {
      max-width: 100%;
      overflow: hidden;
      text-overflow: ellipsis;
      white-space: nowrap;
      font-size: 12px;
      font-weight: 700;
    }
  }
}

.guide-tabs {
  :deep(.el-tabs__header) {
    margin-bottom: var(--guide-gap);
  }

  :deep(.el-tabs__nav-wrap::after) {
    background: var(--lc-border-soft);
  }

  :deep(.el-tabs__item) {
    height: 36px;
    color: var(--lc-muted);
    font-weight: 700;
  }

  :deep(.el-tabs__item.is-active) {
    color: var(--lc-primary);
  }
}

.feature-grid,
.workflow-grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: var(--guide-gap);
}

.feature-card,
.workflow-card,
.design-card {
  margin: 0;
  padding: 14px;
  border-radius: var(--guide-radius);
}

.feature-card header,
.workflow-card header,
.design-card header,
.tips-card header {
  display: flex;
  align-items: flex-start;
  gap: 10px;

  h2,
  h3 {
    margin: 0;
    color: var(--lc-text);
    font-size: 16px;
    line-height: 22px;
  }

  p {
    margin: 4px 0 0;
    color: var(--lc-muted);
    font-size: 13px;
    line-height: 20px;
  }
}

.feature-icon {
  width: 34px;
  height: 34px;
  flex: 0 0 auto;
  display: grid;
  place-items: center;
  border-radius: 10px;
  color: var(--lc-primary);
  background: var(--lc-primary-soft);
  font-size: 18px;
}

.feature-list {
  display: grid;
  gap: 8px;
  margin-top: 12px;

  button {
    min-width: 0;
    display: grid;
    gap: 3px;
    padding: 9px 10px;
    border: 1px solid var(--lc-border);
    border-radius: 10px;
    text-align: left;
    background: var(--lc-surface-soft);
    cursor: pointer;
    transition: border-color .18s ease, background .18s ease;

    &:hover {
      border-color: color-mix(in srgb, var(--lc-primary) 42%, var(--lc-border));
      background: var(--lc-primary-soft);
    }

    b {
      color: var(--lc-text);
      font-size: 13px;
      line-height: 18px;
    }

    span {
      color: var(--lc-muted);
      font-size: 12px;
      line-height: 18px;
    }
  }
}

.design-layout {
  display: grid;
  grid-template-columns: minmax(0, .95fr) minmax(0, 1.05fr);
  gap: var(--guide-gap);
}

.design-map {
  margin: 0;

  h2 {
    margin: 8px 0 6px;
    font-size: 18px;
  }

  p {
    margin: 0;
    color: var(--lc-muted);
    font-size: 13px;
    line-height: 20px;
  }
}

.layout-board {
  display: grid;
  grid-template-columns: 92px minmax(0, 1fr);
  gap: 10px;
  min-height: 260px;
  margin-top: 14px;
}

.layout-sidebar,
.layout-topbar,
.layout-content span {
  display: grid;
  place-items: center;
  border: 1px solid var(--lc-border);
  border-radius: 10px;
  color: var(--lc-muted);
  background: var(--lc-surface-soft);
  font-size: 12px;
  font-weight: 800;
}

.layout-sidebar {
  color: var(--lc-primary);
  background: var(--lc-primary-soft);
}

.layout-main {
  display: grid;
  grid-template-rows: 42px minmax(0, 1fr);
  gap: 10px;
  min-width: 0;
}

.layout-content {
  display: grid;
  grid-template-columns: .85fr 1.3fr .9fr;
  gap: 10px;
}

.design-cards {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: var(--guide-gap);
}

.design-card {
  header {
    align-items: center;

    .el-icon {
      width: 30px;
      height: 30px;
      display: grid;
      place-items: center;
      border-radius: 9px;
      color: var(--lc-mint-strong);
      background: var(--lc-mint-soft);
      font-size: 16px;
    }
  }

  ul {
    display: grid;
    gap: 8px;
    margin: 12px 0 0;
    padding: 0;
    list-style: none;
  }

  li {
    display: flex;
    align-items: flex-start;
    gap: 7px;
    color: var(--lc-muted);
    font-size: 13px;
    line-height: 19px;

    .el-icon {
      margin-top: 2px;
      color: var(--lc-success);
    }
  }
}

.workflow-card {
  display: flex;
  flex-direction: column;

  ol {
    display: grid;
    gap: 8px;
    margin: 14px 0;
    padding: 0;
    list-style: none;
    counter-reset: workflow-step;
  }

  li {
    counter-increment: workflow-step;
    display: flex;
    gap: 8px;
    color: var(--lc-muted);
    font-size: 13px;
    line-height: 20px;

    &::before {
      content: counter(workflow-step);
      width: 20px;
      height: 20px;
      flex: 0 0 auto;
      display: grid;
      place-items: center;
      border-radius: 999px;
      color: var(--lc-primary);
      background: var(--lc-primary-soft);
      font-size: 11px;
      font-weight: 900;
    }
  }

  .el-button {
    margin-top: auto;
    align-self: flex-start;
  }
}

.tips-card {
  margin-top: var(--guide-gap);

  header .el-icon {
    width: 34px;
    height: 34px;
    display: grid;
    place-items: center;
    border-radius: 10px;
    color: var(--lc-warning-text);
    background: var(--lc-warning-soft);
    font-size: 18px;
  }
}

.tip-list {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin-top: 12px;

  span {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    min-height: 28px;
    padding: 0 10px;
    border: 1px solid var(--lc-border);
    border-radius: 999px;
    color: var(--lc-muted);
    background: var(--lc-surface-soft);
    font-size: 12px;

    svg {
      width: 14px;
      height: 14px;
      color: var(--lc-primary);
    }
  }
}

@media (max-width: 1180px) {
  .guide-hero,
  .design-layout {
    grid-template-columns: 1fr;
  }

  .feature-grid,
  .workflow-grid {
    grid-template-columns: 1fr;
  }
}

@media (max-width: 760px) {
  .guide-page {
    --guide-gap: 10px;
  }

  .guide-hero,
  .feature-card,
  .workflow-card,
  .design-card {
    padding: 12px;
  }

  .hero-copy h1 {
    font-size: 22px;
    line-height: 28px;
  }

  .panel-grid,
  .design-cards,
  .layout-content {
    grid-template-columns: 1fr;
  }

  .layout-board {
    grid-template-columns: 1fr;
    min-height: auto;
  }

  .layout-sidebar {
    min-height: 38px;
  }

  .layout-content span {
    min-height: 48px;
  }
}
</style>
