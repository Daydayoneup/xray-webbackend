<script setup>
import { onMounted, onBeforeUnmount, ref, computed, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { apiError } from '../api/http.js'
import { usePanel } from '../stores/panel.js'
import { useAuth } from '../stores/auth.js'
import { xrayApi } from '../api/index.js'
import { applyConfig, restartXray } from '../utils/apply.js'
import { dark } from '../utils/theme.js'
import DiffView from '../components/DiffView.vue'
import { fmtDur } from '../utils/format.js'

const panel = usePanel()
const auth = useAuth()
const route = useRoute()
const router = useRouter()
const applying = ref(false)
const restarting = ref(false)
const logsOpen = ref(false)
const logs = ref([])
const diffOpen = ref(false)
const diffLines = ref([])
async function openDiff() {
  diffOpen.value = true
  try { diffLines.value = (await xrayApi.diff()).data.lines } catch (_) { diffLines.value = [] }
}
async function applyFromDiff() { diffOpen.value = false; await apply() }
const narrow = ref(window.innerWidth < 768)
const menuOpen = ref(!narrow.value)

const STATE = {
  running: { text: 'Xray 运行中', type: 'success' },
  restarting: { text: 'Xray 自动重启中', type: 'warning' },
  crashed: { text: 'Xray 已崩溃', type: 'danger' },
  stopped: { text: 'Xray 未运行', type: 'info' },
}
const stateInfo = computed(() => STATE[panel.status.state] || STATE.stopped)
// 找不到 Xray 时面板会自动下载官方版本
const install = computed(() => panel.status.install || {})
const installing = ref(false)
async function retryInstall() {
  installing.value = true
  try { await xrayApi.install(); await panel.refreshStatus() } catch (e) { ElMessage.error(apiError(e)) } finally { installing.value = false }
}
// 下载中加快刷新，完成后立即显示运行状态
let fastTimer = null
watch(() => install.value.state, (st, old) => {
  clearInterval(fastTimer)
  if (st === 'installing') fastTimer = setInterval(poll, 3000)
  if (old === 'installing' && st === 'done') panel.refreshStatus().catch(() => {})
})
const pwNoticeHidden = ref(sessionStorageGet('pw-notice-hidden') === '1')
function hidePwNotice() { pwNoticeHidden.value = true; sessionStorageSet('pw-notice-hidden', '1') }
function sessionStorageGet(k) { try { return sessionStorage.getItem(k) } catch (_) { return null } }
function sessionStorageSet(k, v) { try { sessionStorage.setItem(k, v) } catch (_) {} }
const warnings = computed(() => panel.status.warnings || [])
// 运行时长随状态轮询刷新
const uptime = computed(() => {
  const t = panel.status.started_at
  return panel.status.running && t ? fmtDur(Date.now() / 1000 - t) : ''
})

let timer = null
function poll() { if (!document.hidden) panel.refreshStatus().catch(() => {}) }
function onResize() { narrow.value = window.innerWidth < 768 }
watch(narrow, (v) => { menuOpen.value = !v })
watch(() => route.path, () => { if (narrow.value) menuOpen.value = false })

onMounted(() => {
  panel.refreshAll().catch(() => {})
  timer = setInterval(poll, 10000)
  window.addEventListener('resize', onResize)
})
onBeforeUnmount(() => { clearInterval(timer); clearInterval(fastTimer); window.removeEventListener('resize', onResize) })

async function apply() {
  applying.value = true
  try { await applyConfig(panel) } finally { applying.value = false }
}
async function restart() {
  restarting.value = true
  try { await restartXray(panel) } finally { restarting.value = false }
}
async function openLogs() {
  logsOpen.value = true
  await loadLogs()
}
async function loadLogs() {
  try { logs.value = (await xrayApi.logs(300)).data.lines } catch (_) { logs.value = [] }
}
async function logout() { await auth.logout(); router.replace({ name: 'login' }) }
</script>

<template>
  <el-container class="app">
    <el-aside v-show="menuOpen" :width="narrow ? '100%' : '200px'" class="side" :class="{ overlay: narrow }">
      <div class="brand">⚡ Xray 面板</div>
      <el-menu :default-active="route.path" router>
        <el-menu-item index="/">概览</el-menu-item>
        <el-menu-item index="/inbound">入站</el-menu-item>
        <el-sub-menu index="users-menu">
          <template #title>用户</template>
          <el-menu-item index="/users">用户管理</el-menu-item>
          <el-menu-item index="/online">在线连接</el-menu-item>
        </el-sub-menu>
        <el-sub-menu index="outbound">
          <template #title>出站</template>
          <el-menu-item index="/outbound/subscription">订阅节点</el-menu-item>
          <el-menu-item index="/outbound/balancers">节点组</el-menu-item>
          <el-menu-item index="/outbound/proxies">自定义代理</el-menu-item>
        </el-sub-menu>
        <el-sub-menu index="routing-menu">
          <template #title>路由</template>
          <el-menu-item index="/routing">分流规则</el-menu-item>
          <el-menu-item index="/routing/sets">域名 / IP 分组</el-menu-item>
        </el-sub-menu>
        <el-menu-item index="/settings">设置</el-menu-item>
      </el-menu>
    </el-aside>
    <el-container>
      <el-header class="top">
        <div class="status">
          <el-button v-if="narrow" size="small" text @click="menuOpen = !menuOpen">☰</el-button>
          <el-tag v-if="install.state === 'installing'" type="warning" effect="dark">正在下载 Xray…</el-tag>
          <template v-else-if="install.state === 'failed' && !panel.status.running">
            <el-tooltip :content="install.error" placement="bottom">
              <el-tag type="danger" effect="dark">Xray 下载失败</el-tag>
            </el-tooltip>
            <el-button size="small" type="primary" :loading="installing" @click="retryInstall">重新下载</el-button>
          </template>
          <el-tooltip v-else :content="panel.status.last_exit ? `最近退出：${panel.status.last_exit}` : (panel.status.version || '')"
                      :disabled="!panel.status.last_exit && !panel.status.version">
            <el-tag :type="stateInfo.type" effect="dark" class="clickable" @click="openLogs">{{ stateInfo.text }}</el-tag>
          </el-tooltip>
          <span class="meta" v-if="panel.status.running">
            {{ panel.status.version || 'Xray' }}<template v-if="uptime"> · 已运行 {{ uptime }}</template>
            <template v-if="panel.status.restarts"> · 自动重启 {{ panel.status.restarts }} 次</template>
          </span>
          <span class="meta err" v-else-if="panel.status.last_exit">最近退出：{{ panel.status.last_exit }}</span>
          <el-button size="small" :loading="restarting" :disabled="!panel.status.applied" @click="restart">重启</el-button>
          <el-button size="small" text @click="openLogs">日志</el-button>
        </div>
        <div class="apply-bar">
          <el-popover v-if="warnings.length" placement="bottom" :width="420" trigger="hover">
            <template #reference><el-tag type="warning" class="clickable">⚠ {{ warnings.length }} 条配置提示</el-tag></template>
            <ul class="warn-list"><li v-for="(w, i) in warnings" :key="i">{{ w }}</li></ul>
          </el-popover>
          <template v-if="panel.status.dirty">
            <span class="dirty">{{ panel.status.applied ? '有未应用更改' : '尚未应用配置' }}</span>
            <el-button v-if="panel.status.applied" size="small" text @click="openDiff">查看改动</el-button>
            <el-button type="primary" size="small" :loading="applying" @click="apply">应用并重启</el-button>
          </template>
          <span v-else class="clean">✅ 配置已生效</span>
          <el-tooltip :content="dark ? '切换到浅色' : '切换到暗色'">
            <el-button size="small" text @click="dark = !dark">{{ dark ? '☀' : '🌙' }}</el-button>
          </el-tooltip>
          <el-button size="small" text @click="logout">退出</el-button>
        </div>
      </el-header>
      <el-main class="main">
        <el-alert v-if="panel.status.password_initial && !pwNoticeHidden" type="warning" show-icon class="pw-notice"
                  @close="hidePwNotice">
          <template #title>
            当前使用的是首次启动时自动生成的密码，建议尽快修改。
            <router-link :to="{ path: '/settings', query: { tab: 'account' } }">去修改</router-link>
          </template>
        </el-alert>
        <el-alert v-if="install.state === 'failed' && !panel.status.running" type="error" show-icon :closable="false" class="pw-notice"
                  :title="`未找到 Xray，自动下载失败：${install.error}`"
                  description="网络无法访问 GitHub 时，可设置环境变量 GH_PROXY（GitHub 加速前缀）或 HTTPS_PROXY 后重启面板，或手动安装 Xray 并设置 XRAY_BIN。" />
        <router-view @changed="panel.refreshAll" />
      </el-main>
    </el-container>
  </el-container>

  <el-drawer v-model="diffOpen" title="待应用的改动（左：当前生效 → 右：应用后）" :size="narrow ? '100%' : '55%'">
    <DiffView :lines="diffLines" empty="与当前生效的配置没有差异" />
    <div style="margin-top:12px;text-align:right">
      <el-button @click="diffOpen = false">关闭</el-button>
      <el-button type="primary" :loading="applying" @click="applyFromDiff">应用并重启</el-button>
    </div>
  </el-drawer>

  <el-drawer v-model="logsOpen" title="Xray 日志（最近 300 行）" :size="narrow ? '100%' : '50%'">
    <div class="log-tools">
      <span class="muted">{{ panel.status.version || '' }}<template v-if="panel.status.restarts"> · 已自动重启 {{ panel.status.restarts }} 次</template></span>
      <el-button size="small" @click="loadLogs">刷新</el-button>
    </div>
    <pre class="log-pre log-full">{{ logs.length ? logs.join('\n') : '暂无日志' }}</pre>
  </el-drawer>
</template>

<style scoped>
.app { height:100vh; }
.side { background:#1a2029; display:flex; flex-direction:column; border-right:1px solid #2a323d; }
.side.overlay { position:fixed; z-index:2000; top:0; bottom:0; left:0; }
.brand { padding:18px 16px; font-weight:700; font-size:16px; color:#4f9cf9;
         border-bottom:1px solid #2a323d; white-space:nowrap; }
.side :deep(.el-menu) {
  --el-menu-bg-color: transparent;
  --el-menu-text-color: #c0c8d2;
  --el-menu-hover-bg-color: #232c38;
  --el-menu-hover-text-color: #ffffff;
  --el-menu-active-color: #4f9cf9;
  border-right: none;
  flex: 1;
}
.side :deep(.el-sub-menu__title:hover),
.side :deep(.el-menu-item:hover) { background:#232c38; }
.side :deep(.el-menu-item.is-active) { background:#11161d; }
.top { background:var(--el-bg-color); display:flex; justify-content:space-between; align-items:center; gap:8px;
       border-bottom:1px solid var(--el-border-color); flex-wrap:wrap; height:auto; min-height:60px; padding:8px 16px; }
.status, .apply-bar { display:flex; align-items:center; gap:8px; flex-wrap:wrap; }
.clickable { cursor:pointer; }
.meta { font-size:12px; color:var(--el-text-color-secondary); }
.meta.err { color:var(--el-color-danger); }
.dirty { color:var(--el-color-warning); font-weight:600; }
.clean { color:var(--el-color-success); font-weight:500; }
.main { background:var(--el-bg-color-page); }
.pw-notice { margin-bottom:12px; }
.pw-notice a { color:var(--el-color-primary); margin-left:6px; }
.warn-list { margin:0; padding-left:18px; font-size:13px; line-height:1.7; }
.log-tools { display:flex; justify-content:space-between; align-items:center; margin-bottom:8px; }
.log-full { max-height:calc(100vh - 160px); }
</style>
