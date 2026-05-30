# Xray 面板前端 Vue 重构 Implementation Plan (Plan 2 of 2)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.
>
> **前置条件:** Plan 1(后端 FastAPI)已完成,`/api/*` 全部可用,`uv run pytest` 全绿。

**Goal:** 用 Vue 3 + Element Plus + Vite 把单页滚动 UI 重构为多页面专业面板(概览/入站/出站三子页/路由/设置),侧边栏导航,Bearer 鉴权,常驻「应用」栏的两段式 dirty 交互。

**Architecture:** Vite 构建的 Vue 3 SPA,hash 路由。`api/` 层用 axios 封装(请求拦截器注入 Bearer,响应拦截器处理 401);Pinia `auth` store 管 token、`panel` store 管跨页共享(出口选项、dirty、xray 状态)。`MainLayout` 提供侧边栏 + 顶栏 + 应用栏外壳。纯逻辑(出口选项聚合、规则收集保序)用 Vitest 覆盖;UI 以手动冒烟验证。构建产物 `dist/` 由后端 `frontend_dist/` 静态托管。

**Tech Stack:** Vue 3 (Composition API, `<script setup>`), Vue Router 4, Pinia, Element Plus, axios, Vite, Vitest, vuedraggable(规则拖拽)。

**参考:** spec `docs/superpowers/specs/2026-05-30-xray-panel-refactor-design.md`;后端 API 见 Plan 1。

---

## 文件结构

| 文件 | 职责 |
|------|------|
| `frontend/package.json` `vite.config.js` `index.html` | 工程与构建配置 |
| `frontend/src/main.js` | 挂载 app、Element Plus、Pinia、Router |
| `frontend/src/App.vue` | 根组件(`<router-view>`) |
| `frontend/src/api/http.js` | axios 实例 + Bearer/401 拦截器 |
| `frontend/src/api/*.js` | 各资源 API 封装 |
| `frontend/src/stores/auth.js` | token 持久化、登录登出 |
| `frontend/src/stores/panel.js` | 出口选项、xray 状态、dirty |
| `frontend/src/router/index.js` | hash 路由 + 登录守卫 |
| `frontend/src/layouts/MainLayout.vue` | 侧边栏 + 顶栏 + 应用栏 |
| `frontend/src/views/Login.vue` | 登录页 |
| `frontend/src/views/Dashboard.vue` | 概览(统计卡 + 拓扑表) |
| `frontend/src/views/Inbound.vue` | 入站 CRUD |
| `frontend/src/views/outbound/Subscription.vue` | 订阅 + 节点 + 测速 |
| `frontend/src/views/outbound/Balancers.vue` | 自动组 |
| `frontend/src/views/outbound/Proxies.vue` | 落地代理 |
| `frontend/src/views/Routing.vue` | 规则(拖拽保序)+ 默认出口 + 模板 |
| `frontend/src/views/Settings.vue` | 改密码 + 原始配置 + 订阅信息 |
| `frontend/src/utils/format.js` | 延迟分级、出口标签等纯函数 |

---

## Task 1: Vite + Vue 脚手架与依赖

**Files:**
- Create: `frontend/package.json` `frontend/vite.config.js` `frontend/index.html` `frontend/src/main.js` `frontend/src/App.vue`
- Test: `frontend/src/__tests__/smoke.test.js`

- [ ] **Step 1: 创建工程目录与 package.json**

```bash
mkdir -p frontend/src/{api,stores,router,layouts,views/outbound,utils,__tests__}
```

`frontend/package.json`:

```json
{
  "name": "xray-panel-frontend",
  "private": true,
  "type": "module",
  "scripts": {
    "dev": "vite",
    "build": "vite build",
    "preview": "vite preview",
    "test": "vitest run"
  },
  "dependencies": {
    "axios": "^1.7.0",
    "element-plus": "^2.7.0",
    "pinia": "^2.1.0",
    "vue": "^3.4.0",
    "vue-router": "^4.3.0",
    "vuedraggable": "^4.1.0"
  },
  "devDependencies": {
    "@vitejs/plugin-vue": "^5.0.0",
    "vite": "^5.2.0",
    "vitest": "^1.6.0"
  }
}
```

- [ ] **Step 2: vite.config.js(hash 路由 + dev proxy + 相对 base)**

`frontend/vite.config.js`:

```javascript
import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

export default defineConfig({
  plugins: [vue()],
  base: './',                       // 相对路径,便于被后端任意挂载
  server: {
    proxy: { '/api': 'http://127.0.0.1:2017' },   // dev 时转发到 uvicorn
  },
  test: { environment: 'node' },
})
```

- [ ] **Step 3: index.html + main.js + App.vue**

`frontend/index.html`:

```html
<!DOCTYPE html>
<html lang="zh-CN">
<head>
  <meta charset="UTF-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1.0" />
  <title>Xray 面板</title>
</head>
<body>
  <div id="app"></div>
  <script type="module" src="/src/main.js"></script>
</body>
</html>
```

`frontend/src/main.js`:

```javascript
import { createApp } from 'vue'
import { createPinia } from 'pinia'
import ElementPlus from 'element-plus'
import 'element-plus/dist/index.css'
import 'element-plus/theme-chalk/dark/css-vars.css'
import App from './App.vue'
import router from './router'

const app = createApp(App)
app.use(createPinia())
app.use(router)
app.use(ElementPlus)
app.mount('#app')
```

`frontend/src/App.vue`:

```vue
<template>
  <router-view />
</template>
```

- [ ] **Step 4: 安装依赖**

```bash
cd frontend && npm install
```

- [ ] **Step 5: 写冒烟测试(纯逻辑占位,验证 vitest 跑通)**

`frontend/src/__tests__/smoke.test.js`:

```javascript
import { describe, it, expect } from 'vitest'
describe('toolchain', () => {
  it('runs', () => { expect(1 + 1).toBe(2) })
})
```

- [ ] **Step 6: 运行测试**

Run: `cd frontend && npm run test`
Expected: PASS(1 passed)。注:`router/index.js` 在 Task 3 才建,本任务的 `main.js` 引用它——**先在 Task 1 建一个占位** `frontend/src/router/index.js`:

```javascript
import { createRouter, createWebHashHistory } from 'vue-router'
export default createRouter({ history: createWebHashHistory(), routes: [] })
```

- [ ] **Step 7: 提交**

```bash
git add frontend/ && git commit -m "feat(fe): Vite + Vue 3 + Element Plus 脚手架"
```

---

## Task 2: API 层 + auth store

**Files:**
- Create: `frontend/src/api/http.js` 各 `frontend/src/api/*.js` `frontend/src/stores/auth.js`
- Test: `frontend/src/__tests__/api.test.js`

- [ ] **Step 1: 写失败测试(验证拦截器注入 Bearer)**

`frontend/src/__tests__/api.test.js`:

```javascript
import { describe, it, expect, beforeEach } from 'vitest'
import { http, setToken } from '../api/http.js'

describe('http interceptor', () => {
  beforeEach(() => setToken(null))
  it('injects bearer when token set', () => {
    setToken('abc')
    const cfg = http.interceptors.request.handlers[0].fulfilled({ headers: {} })
    expect(cfg.headers.Authorization).toBe('Bearer abc')
  })
  it('no header when token null', () => {
    const cfg = http.interceptors.request.handlers[0].fulfilled({ headers: {} })
    expect(cfg.headers.Authorization).toBeUndefined()
  })
})
```

- [ ] **Step 2: 运行确认失败**

Run: `cd frontend && npm run test -- api`
Expected: FAIL — 无法解析 `../api/http.js`

- [ ] **Step 3: 实现 http.js**

`frontend/src/api/http.js`:

```javascript
import axios from 'axios'

let _token = localStorage.getItem('xray_token') || null
export function setToken(t) {
  _token = t
  if (t) localStorage.setItem('xray_token', t)
  else localStorage.removeItem('xray_token')
}
export function getToken() { return _token }

export const http = axios.create({ baseURL: '/api' })

http.interceptors.request.use((cfg) => {
  if (_token) cfg.headers.Authorization = `Bearer ${_token}`
  return cfg
})

let onUnauth = () => {}
export function setUnauthHandler(fn) { onUnauth = fn }

http.interceptors.response.use(
  (r) => r,
  (err) => {
    if (err.response && err.response.status === 401) { setToken(null); onUnauth() }
    return Promise.reject(err)
  },
)

export function apiError(err) {
  return err?.response?.data?.detail || err?.message || '请求失败'
}
```

- [ ] **Step 4: 实现各资源 API 封装**

`frontend/src/api/index.js`:

```javascript
import { http } from './http.js'

export const authApi = {
  login: (password) => http.post('/auth/login', { password }),
  logout: () => http.post('/auth/logout'),
}
export const subscriptionApi = {
  get: () => http.get('/subscription'),
  set: (url) => http.put('/subscription', { url }),
  nodes: () => http.get('/nodes'),
  test: () => http.post('/nodes/test'),
}
export const inboundApi = {
  list: () => http.get('/inbounds'),
  create: (b) => http.post('/inbounds', b),
  update: (tag, b) => http.put(`/inbounds/${tag}`, b),
  remove: (tag) => http.delete(`/inbounds/${tag}`),
}
export const proxyApi = {
  list: () => http.get('/proxies'),
  create: (b) => http.post('/proxies', b),
  update: (tag, b) => http.put(`/proxies/${tag}`, b),
  remove: (tag) => http.delete(`/proxies/${tag}`),
}
export const balancerApi = {
  list: () => http.get('/balancers'),
  create: (b) => http.post('/balancers', b),
  update: (tag, b) => http.put(`/balancers/${tag}`, b),
  remove: (tag) => http.delete(`/balancers/${tag}`),
}
export const routingApi = {
  get: () => http.get('/routing'),
  put: (b) => http.put('/routing', b),
  templates: () => http.get('/routing/templates'),
  outbounds: () => http.get('/outbounds'),
}
export const xrayApi = {
  status: () => http.get('/xray/status'),
  apply: () => http.post('/apply'),
  restart: () => http.post('/xray/restart'),
  config: () => http.get('/config'),
  topology: () => http.get('/topology'),
}
```

- [ ] **Step 5: 实现 auth store**

`frontend/src/stores/auth.js`:

```javascript
import { defineStore } from 'pinia'
import { ref } from 'vue'
import { authApi } from '../api/index.js'
import { setToken, getToken } from '../api/http.js'

export const useAuth = defineStore('auth', () => {
  const token = ref(getToken())
  async function login(password) {
    const { data } = await authApi.login(password)
    setToken(data.token); token.value = data.token
  }
  async function logout() {
    try { await authApi.logout() } catch (_) {}
    setToken(null); token.value = null
  }
  function isAuthed() { return !!token.value }
  return { token, login, logout, isAuthed }
})
```

- [ ] **Step 6: 运行确认通过**

Run: `cd frontend && npm run test -- api`
Expected: PASS

- [ ] **Step 7: 提交**

```bash
git add frontend/src && git commit -m "feat(fe): axios API 层(Bearer/401 拦截)+ auth store"
```

---

## Task 3: 路由 + 登录页 + 登录守卫

**Files:**
- Modify: `frontend/src/router/index.js`
- Create: `frontend/src/views/Login.vue`
- Test: 手动冒烟

- [ ] **Step 1: 实现路由表 + 守卫**

`frontend/src/router/index.js`:

```javascript
import { createRouter, createWebHashHistory } from 'vue-router'
import { getToken, setUnauthHandler } from '../api/http.js'

const routes = [
  { path: '/login', name: 'login', component: () => import('../views/Login.vue') },
  {
    path: '/', component: () => import('../layouts/MainLayout.vue'),
    children: [
      { path: '', name: 'dashboard', component: () => import('../views/Dashboard.vue') },
      { path: 'inbound', name: 'inbound', component: () => import('../views/Inbound.vue') },
      { path: 'outbound/subscription', name: 'subscription', component: () => import('../views/outbound/Subscription.vue') },
      { path: 'outbound/balancers', name: 'balancers', component: () => import('../views/outbound/Balancers.vue') },
      { path: 'outbound/proxies', name: 'proxies', component: () => import('../views/outbound/Proxies.vue') },
      { path: 'routing', name: 'routing', component: () => import('../views/Routing.vue') },
      { path: 'settings', name: 'settings', component: () => import('../views/Settings.vue') },
    ],
  },
]

const router = createRouter({ history: createWebHashHistory(), routes })

router.beforeEach((to) => {
  if (to.name !== 'login' && !getToken()) return { name: 'login' }
  if (to.name === 'login' && getToken()) return { name: 'dashboard' }
})

setUnauthHandler(() => router.replace({ name: 'login' }))

export default router
```

- [ ] **Step 2: 实现 Login.vue**

`frontend/src/views/Login.vue`:

```vue
<script setup>
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { useAuth } from '../stores/auth.js'
import { apiError } from '../api/http.js'

const password = ref('')
const loading = ref(false)
const auth = useAuth()
const router = useRouter()

async function submit() {
  loading.value = true
  try {
    await auth.login(password.value)
    router.replace({ name: 'dashboard' })
  } catch (e) { ElMessage.error(apiError(e)) }
  finally { loading.value = false }
}
</script>

<template>
  <div class="login-wrap">
    <el-card class="login-card">
      <h2>Xray 面板登录</h2>
      <el-input v-model="password" type="password" placeholder="密码" show-password
                @keyup.enter="submit" />
      <el-button type="primary" :loading="loading" class="login-btn" @click="submit">登录</el-button>
    </el-card>
  </div>
</template>

<style scoped>
.login-wrap { display:flex; justify-content:center; padding-top:16vh; }
.login-card { width:360px; }
.login-card h2 { margin:0 0 16px; }
.login-btn { width:100%; margin-top:12px; }
</style>
```

- [ ] **Step 3: 手动冒烟(需后端在 2017 运行)**

```bash
# 终端1: 后端
uv run uvicorn backend.main:app --port 2017
# 终端2: 前端
cd frontend && npm run dev
```
打开 dev URL → 应跳转 `#/login` → 输入后端启动日志里的密码 → 进入面板(此时其它页是空,Task 4+ 填充)。
Expected: 错误密码弹「密码错误」;正确密码进入 `#/`。

- [ ] **Step 4: 提交**

```bash
git add frontend/src && git commit -m "feat(fe): 路由 + 登录守卫 + 登录页"
```

---

## Task 4: 主布局 + panel store(侧边栏/顶栏/应用栏)

**Files:**
- Create: `frontend/src/layouts/MainLayout.vue` `frontend/src/stores/panel.js`
- Test: 手动冒烟

- [ ] **Step 1: 实现 panel store(出口选项 + xray 状态 + dirty)**

`frontend/src/stores/panel.js`:

```javascript
import { defineStore } from 'pinia'
import { ref } from 'vue'
import { routingApi, xrayApi } from '../api/index.js'

export const usePanel = defineStore('panel', () => {
  const outbounds = ref([])              // [{tag,label,kind}]
  const status = ref({ running: false, applied: false, dirty: false })

  async function refreshOutbounds() {
    const { data } = await routingApi.outbounds(); outbounds.value = data
  }
  async function refreshStatus() {
    const { data } = await xrayApi.status(); status.value = data
  }
  async function refreshAll() { await Promise.all([refreshOutbounds(), refreshStatus()]) }
  return { outbounds, status, refreshOutbounds, refreshStatus, refreshAll }
})
```

- [ ] **Step 2: 实现 MainLayout.vue**

`frontend/src/layouts/MainLayout.vue`:

```vue
<script setup>
import { onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { usePanel } from '../stores/panel.js'
import { useAuth } from '../stores/auth.js'
import { xrayApi } from '../api/index.js'
import { apiError } from '../api/http.js'

const panel = usePanel()
const auth = useAuth()
const route = useRoute()
const router = useRouter()

onMounted(() => panel.refreshAll())

async function apply() {
  try {
    const { data } = await xrayApi.apply()
    await panel.refreshStatus()
    ElMessage[data.xray_running ? 'success' : 'warning'](
      data.xray_running ? '已应用,Xray 已重启' : '已应用,但 Xray 未运行')
  } catch (e) { ElMessage.error(apiError(e)) }
}
async function restart() {
  try { await xrayApi.restart(); await panel.refreshStatus(); ElMessage.success('已重启') }
  catch (e) { ElMessage.error(apiError(e)) }
}
async function logout() { await auth.logout(); router.replace({ name: 'login' }) }
</script>

<template>
  <el-container class="app">
    <el-aside width="200px" class="side">
      <div class="brand">⚡ Xray 面板</div>
      <el-menu :default-active="route.path" router>
        <el-menu-item index="/"><span>概览</span></el-menu-item>
        <el-menu-item index="/inbound">入站 Inbound</el-menu-item>
        <el-sub-menu index="outbound">
          <template #title>出站 Outbound</template>
          <el-menu-item index="/outbound/subscription">订阅节点</el-menu-item>
          <el-menu-item index="/outbound/balancers">自动组</el-menu-item>
          <el-menu-item index="/outbound/proxies">落地代理</el-menu-item>
        </el-sub-menu>
        <el-menu-item index="/routing">路由 Routing</el-menu-item>
        <el-menu-item index="/settings">设置</el-menu-item>
      </el-menu>
    </el-aside>
    <el-container>
      <el-header class="top">
        <div class="status">
          <el-tag :type="panel.status.running ? 'success' : 'danger'" effect="dark">
            {{ panel.status.running ? 'Xray 运行中' : 'Xray 未运行' }}
          </el-tag>
          <el-button size="small" @click="restart">重启 Xray</el-button>
        </div>
        <div class="apply-bar">
          <template v-if="panel.status.dirty">
            <span class="dirty">⚠ 有未应用更改</span>
            <el-button type="primary" size="small" @click="apply">应用并重启 Xray</el-button>
          </template>
          <span v-else class="clean">✅ 配置已生效</span>
          <el-button size="small" text @click="logout">退出</el-button>
        </div>
      </el-header>
      <el-main><router-view @changed="panel.refreshAll" /></el-main>
    </el-container>
  </el-container>
</template>

<style scoped>
.app { height:100vh; }
.side { background:#1a2029; color:#e6e6e6; }
.brand { padding:16px; font-weight:700; color:#4f9cf9; }
.top { display:flex; justify-content:space-between; align-items:center; border-bottom:1px solid var(--el-border-color); }
.status, .apply-bar { display:flex; align-items:center; gap:10px; }
.dirty { color:var(--el-color-warning); font-weight:600; }
.clean { color:var(--el-color-success); }
</style>
```

> 约定:每个子页面在成功写操作后 `emit('changed')`,由 `MainLayout` 刷新出口选项与 dirty 状态。

- [ ] **Step 3: 手动冒烟**

`npm run dev` + 后端运行 → 登录后看到侧边栏(出站可展开三子项)、顶栏 Xray 状态、应用栏(初次无配置应显示「有未应用更改」或「配置已生效」视后端状态)。
Expected: 导航点击切换路由;重启/退出按钮可用。

- [ ] **Step 4: 提交**

```bash
git add frontend/src && git commit -m "feat(fe): MainLayout(侧边栏+顶栏+应用栏)+ panel store"
```

---

## Task 5: 工具函数 + Dashboard

**Files:**
- Create: `frontend/src/utils/format.js` `frontend/src/views/Dashboard.vue`
- Test: `frontend/src/__tests__/format.test.js`

- [ ] **Step 1: 写失败测试**

`frontend/src/__tests__/format.test.js`:

```javascript
import { describe, it, expect } from 'vitest'
import { latClass, latText, aliveStats } from '../utils/format.js'

describe('format', () => {
  it('latClass', () => {
    expect(latClass(null)).toBe('bad')
    expect(latClass(100)).toBe('good')
    expect(latClass(300)).toBe('mid')
    expect(latClass(900)).toBe('bad')
  })
  it('latText', () => {
    expect(latText(null)).toBe('超时')
    expect(latText(88)).toBe('88ms')
  })
  it('aliveStats', () => {
    const s = aliveStats([{ latency: 100, name: 'A' }, { latency: null }, { latency: 50, name: 'B' }])
    expect(s.alive).toBe(2)
    expect(s.fastest.name).toBe('B')
  })
})
```

- [ ] **Step 2: 运行确认失败**

Run: `cd frontend && npm run test -- format`
Expected: FAIL — 无法解析 `../utils/format.js`

- [ ] **Step 3: 实现 format.js**

`frontend/src/utils/format.js`:

```javascript
export function latClass(ms) {
  if (ms == null) return 'bad'
  if (ms < 200) return 'good'
  if (ms < 500) return 'mid'
  return 'bad'
}
export function latText(ms) { return ms == null ? '超时' : `${ms}ms` }

export function aliveStats(nodes) {
  const alive = nodes.filter((n) => n.latency != null)
  const fastest = alive.slice().sort((a, b) => a.latency - b.latency)[0] || null
  return { total: nodes.length, alive: alive.length, fastest }
}
```

- [ ] **Step 4: 运行确认通过**

Run: `cd frontend && npm run test -- format`
Expected: PASS

- [ ] **Step 5: 实现 Dashboard.vue**

`frontend/src/views/Dashboard.vue`:

```vue
<script setup>
import { ref, onMounted, computed } from 'vue'
import { subscriptionApi, inboundApi, routingApi, xrayApi } from '../api/index.js'
import { usePanel } from '../stores/panel.js'
import { aliveStats } from '../utils/format.js'

const panel = usePanel()
const nodes = ref([])
const inbounds = ref([])
const routing = ref({ rules: [], default_outbound: '' })
const sub = ref({})
const topo = ref({ applied: false, outbounds: [], routing: [] })

const stats = computed(() => aliveStats(nodes.value))
const enabledRules = computed(() => routing.value.rules.filter((r) => r.enabled).length)
const defaultLabel = computed(() => {
  const o = panel.outbounds.find((x) => x.tag === routing.value.default_outbound)
  return o ? o.label : (routing.value.default_outbound || '—')
})

onMounted(async () => {
  await panel.refreshAll()
  const [n, ib, rt, s, tp] = await Promise.all([
    subscriptionApi.nodes(), inboundApi.list(), routingApi.get(),
    subscriptionApi.get(), xrayApi.topology(),
  ])
  nodes.value = n.data; inbounds.value = ib.data; routing.value = rt.data
  sub.value = s.data; topo.value = tp.data
})
</script>

<template>
  <div class="grid">
    <el-card><div class="lab">Xray 状态</div>
      <div class="val" :class="panel.status.running ? 'ok' : 'err'">
        {{ panel.status.running ? '运行中' : '未运行' }}</div>
      <div class="sub">{{ inbounds.map(i => i.protocol + ':' + i.port).join(' · ') }}</div></el-card>
    <el-card><div class="lab">应用状态</div>
      <div class="val" :class="panel.status.dirty ? 'warn' : 'ok'">
        {{ panel.status.dirty ? '⚠ 未应用更改' : '已生效' }}</div></el-card>
    <el-card><div class="lab">节点</div>
      <div class="val acc">{{ stats.total }}</div>
      <div class="sub">存活 {{ stats.alive }}<template v-if="stats.fastest"> · 最快 {{ stats.fastest.name }} ({{ stats.fastest.latency }}ms)</template></div></el-card>
    <el-card><div class="lab">默认出口</div><div class="val sm">{{ defaultLabel }}</div></el-card>
    <el-card><div class="lab">入站</div><div class="val">{{ inbounds.length }}</div></el-card>
    <el-card><div class="lab">出站合计</div><div class="val">{{ panel.outbounds.filter(o => o.kind !== 'builtin').length }}</div></el-card>
    <el-card><div class="lab">分流规则</div><div class="val">{{ enabledRules }} <span class="sub">/ {{ routing.rules.length }}</span></div></el-card>
    <el-card><div class="lab">订阅</div><div class="val sm">{{ sub.remarks || '未配置' }}</div>
      <div class="sub" v-if="sub.fetched_at">拉取于 {{ new Date(sub.fetched_at * 1000).toLocaleString() }}</div></el-card>
  </div>

  <div class="tables">
    <el-card>
      <template #header>出口清单(每个出口 ← 命中它的规则)</template>
      <el-table :data="topo.outbounds" size="small">
        <el-table-column prop="tag" label="tag" width="100" />
        <el-table-column prop="label" label="出口" />
        <el-table-column label="命中规则"><template #default="{ row }">{{ row.rules.join('、') || '—' }}</template></el-table-column>
      </el-table>
    </el-card>
    <el-card>
      <template #header>路由优先级(自上而下,先命中先生效)</template>
      <el-table :data="topo.routing" size="small">
        <el-table-column prop="order" label="#" width="50" />
        <el-table-column prop="match" label="匹配条件" />
        <el-table-column label="出口"><template #default="{ row }">→ {{ row.label }}</template></el-table-column>
      </el-table>
    </el-card>
  </div>
</template>

<style scoped>
.grid { display:grid; grid-template-columns:repeat(4,1fr); gap:12px; }
.tables { display:grid; grid-template-columns:1fr 1fr; gap:12px; margin-top:16px; }
.lab { font-size:12px; color:var(--el-text-color-secondary); }
.val { font-size:22px; font-weight:700; margin-top:4px; }
.val.sm { font-size:15px; } .val.ok { color:var(--el-color-success); }
.val.warn { color:var(--el-color-warning); } .val.err { color:var(--el-color-danger); }
.val.acc { color:var(--el-color-primary); }
.sub { font-size:12px; color:var(--el-text-color-secondary); margin-top:4px; }
@media (max-width:900px){ .grid{ grid-template-columns:repeat(2,1fr);} .tables{ grid-template-columns:1fr;} }
</style>
```

- [ ] **Step 6: 手动冒烟** — 登录后概览页应显示 8 张卡 + 两张表(无数据时表为空)。

- [ ] **Step 7: 提交**

```bash
git add frontend/src && git commit -m "feat(fe): 工具函数 + Dashboard 概览"
```

---

## Task 6: 入站页

**Files:**
- Create: `frontend/src/views/Inbound.vue`
- Test: 手动冒烟

- [ ] **Step 1: 实现 Inbound.vue**

`frontend/src/views/Inbound.vue`:

```vue
<script setup>
import { ref, onMounted, reactive } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { inboundApi } from '../api/index.js'
import { apiError } from '../api/http.js'

const emit = defineEmits(['changed'])
const list = ref([])
const dialog = ref(false)
const editing = ref(null)            // tag | null(新建)
const form = reactive({ protocol: 'socks', listen: '127.0.0.1', port: null, udp: true, user: '', pass: '' })

async function load() { list.value = (await inboundApi.list()).data }
onMounted(load)

function openCreate() {
  editing.value = null
  Object.assign(form, { protocol: 'socks', listen: '127.0.0.1', port: null, udp: true, user: '', pass: '' })
  dialog.value = true
}
function openEdit(row) {
  editing.value = row.tag
  Object.assign(form, { protocol: row.protocol, listen: row.listen, port: row.port,
    udp: row.udp ?? true, user: row.auth?.user || '', pass: row.auth?.pass || '' })
  dialog.value = true
}
function payload() {
  const p = { protocol: form.protocol, listen: form.listen.trim() || '127.0.0.1', port: form.port }
  if (form.protocol === 'socks') p.udp = form.udp
  if (form.user.trim() || form.pass) p.auth = { user: form.user.trim(), pass: form.pass }
  return p
}
async function save() {
  try {
    if (editing.value) await inboundApi.update(editing.value, payload())
    else await inboundApi.create(payload())
    dialog.value = false; await load(); emit('changed'); ElMessage.success('已保存')
  } catch (e) { ElMessage.error(apiError(e)) }
}
async function remove(row) {
  await ElMessageBox.confirm(`删除入站 ${row.tag}?`, '确认', { type: 'warning' })
  try { await inboundApi.remove(row.tag); await load(); emit('changed') }
  catch (e) { ElMessage.error(apiError(e)) }
}
function risky(row) { return row.listen === '0.0.0.0' && !row.auth }
</script>

<template>
  <el-card>
    <template #header>
      <div class="hd"><span>入站 Inbound(本地代理端口)</span>
        <el-button type="primary" @click="openCreate">+ 新建入站</el-button></div>
    </template>
    <el-table :data="list">
      <el-table-column prop="tag" label="tag" width="90" />
      <el-table-column prop="protocol" label="协议" width="90" />
      <el-table-column prop="listen" label="监听" />
      <el-table-column prop="port" label="端口" width="90" />
      <el-table-column label="鉴权"><template #default="{ row }">
        <span v-if="row.auth">{{ row.auth.user }}</span>
        <el-tag v-else-if="risky(row)" type="warning" size="small">⚠ 0.0.0.0 无密码</el-tag>
        <span v-else>—</span></template></el-table-column>
      <el-table-column label="操作" width="140"><template #default="{ row }">
        <el-button size="small" @click="openEdit(row)">编辑</el-button>
        <el-button size="small" type="danger" @click="remove(row)">删</el-button></template></el-table-column>
    </el-table>
  </el-card>

  <el-dialog v-model="dialog" :title="editing ? '编辑入站' : '新建入站'" width="460px">
    <el-form label-width="80px">
      <el-form-item label="协议"><el-select v-model="form.protocol">
        <el-option label="socks" value="socks" /><el-option label="http" value="http" /></el-select></el-form-item>
      <el-form-item label="监听地址"><el-input v-model="form.listen" placeholder="127.0.0.1" /></el-form-item>
      <el-form-item label="端口"><el-input-number v-model="form.port" :min="1" :max="65535" controls-position="right" /></el-form-item>
      <el-form-item v-if="form.protocol === 'socks'" label="UDP"><el-switch v-model="form.udp" /></el-form-item>
      <el-form-item label="账号"><el-input v-model="form.user" placeholder="可选" /></el-form-item>
      <el-form-item label="密码"><el-input v-model="form.pass" placeholder="可选" /></el-form-item>
    </el-form>
    <template #footer><el-button @click="dialog = false">取消</el-button>
      <el-button type="primary" @click="save">保存</el-button></template>
  </el-dialog>
</template>

<style scoped>.hd { display:flex; justify-content:space-between; align-items:center; }</style>
```

- [ ] **Step 2: 手动冒烟** — 新建/编辑/删除入站;0.0.0.0 无密码显示告警 tag;非法端口后端返回 422,弹错误。

- [ ] **Step 3: 提交**

```bash
git add frontend/src/views/Inbound.vue && git commit -m "feat(fe): 入站管理页"
```

---

## Task 7: 出站 · 订阅节点页

**Files:**
- Create: `frontend/src/views/outbound/Subscription.vue`
- Test: 手动冒烟

- [ ] **Step 1: 实现 Subscription.vue**

`frontend/src/views/outbound/Subscription.vue`:

```vue
<script setup>
import { ref, onMounted, computed } from 'vue'
import { ElMessage } from 'element-plus'
import { subscriptionApi, routingApi } from '../../api/index.js'
import { apiError } from '../../api/http.js'
import { latClass, latText } from '../../utils/format.js'
import { usePanel } from '../../stores/panel.js'

const emit = defineEmits(['changed'])
const panel = usePanel()
const url = ref('')
const sub = ref({})
const nodes = ref([])
const filter = ref('')
const fetching = ref(false)
const testing = ref(false)

const shown = computed(() => {
  const q = filter.value.toLowerCase()
  return nodes.value.filter((n) => !q || n.name.toLowerCase().includes(q) || n.type.includes(q))
})

async function load() {
  sub.value = (await subscriptionApi.get()).data; url.value = sub.value.url || ''
  nodes.value = (await subscriptionApi.nodes()).data
}
onMounted(load)

async function fetchSub() {
  fetching.value = true
  try {
    const { data } = await subscriptionApi.set(url.value)
    nodes.value = data.nodes; sub.value = data.subscription
    await panel.refreshOutbounds(); emit('changed')
    ElMessage.success(`解析到 ${data.nodes.length} 个节点${data.skipped ? `,跳过 ${data.skipped} 个` : ''}`)
  } catch (e) { ElMessage.error(apiError(e)) }
  finally { fetching.value = false }
}
async function testSpeed() {
  testing.value = true
  try { nodes.value = (await subscriptionApi.test()).data; ElMessage.success('测速完成') }
  catch (e) { ElMessage.error(apiError(e)) }
  finally { testing.value = false }
}
async function pickFastest() {
  const alive = nodes.value.filter((n) => n.latency != null).sort((a, b) => a.latency - b.latency)
  if (!alive.length) return ElMessage.warning('先测速,或当前无可连节点')
  const rt = (await routingApi.get()).data
  await routingApi.put({ default_outbound: alive[0].tag, rules: rt.rules })
  await panel.refreshAll(); emit('changed')
  ElMessage.success(`默认出口已设为 ${alive[0].name}(${alive[0].latency}ms)`)
}
</script>

<template>
  <el-card>
    <template #header>订阅</template>
    <div class="row">
      <el-input v-model="url" placeholder="粘贴订阅链接 (http...)" />
      <el-button type="primary" :loading="fetching" @click="fetchSub">拉取并解析</el-button>
    </div>
    <div class="meta" v-if="sub.remarks || sub.status">{{ sub.remarks }} {{ sub.status }}</div>
  </el-card>

  <el-card style="margin-top:16px;">
    <template #header>
      <div class="hd"><span>节点({{ nodes.length }})</span>
        <div class="row">
          <el-input v-model="filter" placeholder="过滤 名称/协议" style="width:180px" />
          <el-button @click="pickFastest">选最快为默认</el-button>
          <el-button :loading="testing" @click="testSpeed">测速</el-button>
        </div></div>
    </template>
    <el-table :data="shown" max-height="460">
      <el-table-column prop="tag" label="tag" width="90" />
      <el-table-column label="延迟" width="90"><template #default="{ row }">
        <span :class="'lat-' + latClass(row.latency)">{{ latText(row.latency) }}</span></template></el-table-column>
      <el-table-column prop="type" label="协议" width="110" />
      <el-table-column prop="name" label="名称" />
    </el-table>
  </el-card>
</template>

<style scoped>
.row { display:flex; gap:8px; }
.meta { margin-top:8px; color:var(--el-text-color-secondary); font-size:13px; }
.hd { display:flex; justify-content:space-between; align-items:center; }
.lat-good { color:var(--el-color-success); } .lat-mid { color:var(--el-color-warning); }
.lat-bad { color:var(--el-color-danger); }
</style>
```

- [ ] **Step 2: 手动冒烟** — 拉取订阅(用真实可达订阅或本地 mock 文件 serve 的 http)、测速、选最快为默认。

- [ ] **Step 3: 提交**

```bash
git add frontend/src/views/outbound/Subscription.vue && git commit -m "feat(fe): 订阅节点页"
```

---

## Task 8: 出站 · 自动组页

**Files:**
- Create: `frontend/src/views/outbound/Balancers.vue`
- Test: 手动冒烟

- [ ] **Step 1: 实现 Balancers.vue**

`frontend/src/views/outbound/Balancers.vue`:

```vue
<script setup>
import { ref, onMounted, reactive } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { balancerApi, subscriptionApi } from '../../api/index.js'
import { apiError } from '../../api/http.js'
import { usePanel } from '../../stores/panel.js'

const emit = defineEmits(['changed'])
const panel = usePanel()
const list = ref([])
const nodes = ref([])
const dialog = ref(false)
const editing = ref(null)
const form = reactive({ name: '', nodes: [] })

async function load() {
  list.value = (await balancerApi.list()).data
  nodes.value = (await subscriptionApi.nodes()).data
}
onMounted(load)

const transferData = () => nodes.value.map((n) => ({ key: n.tag, label: `${n.name} (${n.tag})` }))

function openCreate() { editing.value = null; Object.assign(form, { name: '', nodes: [] }); dialog.value = true }
function openEdit(row) { editing.value = row.tag; Object.assign(form, { name: row.name, nodes: [...row.nodes] }); dialog.value = true }
async function save() {
  try {
    const body = { name: form.name, nodes: form.nodes }
    if (editing.value) await balancerApi.update(editing.value, body)
    else await balancerApi.create(body)
    dialog.value = false; await load(); await panel.refreshOutbounds(); emit('changed'); ElMessage.success('已保存')
  } catch (e) { ElMessage.error(apiError(e)) }
}
async function remove(row) {
  await ElMessageBox.confirm(`删除自动组「${row.name}」?`, '确认', { type: 'warning' })
  try { await balancerApi.remove(row.tag); await load(); await panel.refreshAll(); emit('changed') }
  catch (e) { ElMessage.error(apiError(e)) }
}
</script>

<template>
  <el-card>
    <template #header>
      <div class="hd"><span>自动组(负载均衡 · 自动选最快)</span>
        <el-button type="primary" @click="openCreate">+ 新建自动组</el-button></div>
    </template>
    <el-empty v-if="!list.length" description="无自动组。新建并勾选一组节点后,Xray 自动走延迟最低的活节点。" />
    <el-table v-else :data="list">
      <el-table-column prop="tag" label="tag" width="90" />
      <el-table-column prop="name" label="名称" />
      <el-table-column label="成员节点"><template #default="{ row }">{{ row.nodes.length }} 个</template></el-table-column>
      <el-table-column label="操作" width="140"><template #default="{ row }">
        <el-button size="small" @click="openEdit(row)">编辑</el-button>
        <el-button size="small" type="danger" @click="remove(row)">删</el-button></template></el-table-column>
    </el-table>
  </el-card>

  <el-dialog v-model="dialog" :title="editing ? '编辑自动组' : '新建自动组'" width="600px">
    <el-form label-width="70px">
      <el-form-item label="名称"><el-input v-model="form.name" /></el-form-item>
      <el-form-item label="节点">
        <el-transfer v-model="form.nodes" :data="transferData()" :titles="['可选节点', '已选节点']" filterable />
      </el-form-item>
    </el-form>
    <template #footer><el-button @click="dialog = false">取消</el-button>
      <el-button type="primary" @click="save">保存</el-button></template>
  </el-dialog>
</template>

<style scoped>.hd { display:flex; justify-content:space-between; align-items:center; }</style>
```

- [ ] **Step 2: 手动冒烟** — 需先有节点;新建组穿梭框选节点;空组后端拒绝(弹错)。

- [ ] **Step 3: 提交**

```bash
git add frontend/src/views/outbound/Balancers.vue && git commit -m "feat(fe): 自动组页"
```

---

## Task 9: 出站 · 落地代理页

**Files:**
- Create: `frontend/src/views/outbound/Proxies.vue`
- Test: 手动冒烟

- [ ] **Step 1: 实现 Proxies.vue**

`frontend/src/views/outbound/Proxies.vue`:

```vue
<script setup>
import { ref, onMounted, reactive } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { proxyApi } from '../../api/index.js'
import { apiError } from '../../api/http.js'
import { usePanel } from '../../stores/panel.js'

const emit = defineEmits(['changed'])
const panel = usePanel()
const list = ref([])
const dialog = ref(false)
const editing = ref(null)
const form = reactive({ name: '', protocol: 'socks', host: '', port: null, user: '', pass: '' })

async function load() { list.value = (await proxyApi.list()).data }
onMounted(load)

function openCreate() { editing.value = null; Object.assign(form, { name: '', protocol: 'socks', host: '', port: null, user: '', pass: '' }); dialog.value = true }
function openEdit(row) {
  editing.value = row.tag
  Object.assign(form, { name: row.name, protocol: row.protocol, host: row.host, port: row.port,
    user: row.auth?.user || '', pass: row.auth?.pass || '' })
  dialog.value = true
}
function payload() {
  const p = { name: form.name.trim(), protocol: form.protocol, host: form.host.trim(), port: form.port }
  if (form.user.trim() || form.pass) p.auth = { user: form.user.trim(), pass: form.pass }
  return p
}
async function save() {
  try {
    if (editing.value) await proxyApi.update(editing.value, payload())
    else await proxyApi.create(payload())
    dialog.value = false; await load(); await panel.refreshOutbounds(); emit('changed'); ElMessage.success('已保存')
  } catch (e) { ElMessage.error(apiError(e)) }
}
async function remove(row) {
  await ElMessageBox.confirm(`删除代理「${row.name}」?`, '确认', { type: 'warning' })
  try { await proxyApi.remove(row.tag); await load(); await panel.refreshAll(); emit('changed') }
  catch (e) { ElMessage.error(apiError(e)) }
}
</script>

<template>
  <el-card>
    <template #header>
      <div class="hd"><span>自定义出口代理(落地代理)</span>
        <el-button type="primary" @click="openCreate">+ 新建代理</el-button></div>
    </template>
    <el-table :data="list">
      <el-table-column prop="tag" label="tag" width="80" />
      <el-table-column prop="name" label="名称" />
      <el-table-column prop="protocol" label="协议" width="90" />
      <el-table-column prop="host" label="地址" />
      <el-table-column prop="port" label="端口" width="90" />
      <el-table-column label="操作" width="140"><template #default="{ row }">
        <el-button size="small" @click="openEdit(row)">编辑</el-button>
        <el-button size="small" type="danger" @click="remove(row)">删</el-button></template></el-table-column>
    </el-table>
  </el-card>

  <el-dialog v-model="dialog" :title="editing ? '编辑代理' : '新建代理'" width="460px">
    <el-form label-width="80px">
      <el-form-item label="名称"><el-input v-model="form.name" /></el-form-item>
      <el-form-item label="协议"><el-select v-model="form.protocol">
        <el-option label="socks" value="socks" /><el-option label="http" value="http" /></el-select></el-form-item>
      <el-form-item label="地址"><el-input v-model="form.host" placeholder="host" /></el-form-item>
      <el-form-item label="端口"><el-input-number v-model="form.port" :min="1" :max="65535" controls-position="right" /></el-form-item>
      <el-form-item label="账号"><el-input v-model="form.user" placeholder="可选" /></el-form-item>
      <el-form-item label="密码"><el-input v-model="form.pass" placeholder="可选" /></el-form-item>
    </el-form>
    <template #footer><el-button @click="dialog = false">取消</el-button>
      <el-button type="primary" @click="save">保存</el-button></template>
  </el-dialog>
</template>

<style scoped>.hd { display:flex; justify-content:space-between; align-items:center; }</style>
```

- [ ] **Step 2: 手动冒烟** — 新建/编辑/删除代理;空 host 返回 422 弹错。

- [ ] **Step 3: 提交**

```bash
git add frontend/src/views/outbound/Proxies.vue && git commit -m "feat(fe): 落地代理页"
```

---

## Task 10: 路由页(拖拽保序 + 默认出口 + 模板)

**Files:**
- Create: `frontend/src/views/Routing.vue`
- Test: `frontend/src/__tests__/rules.test.js`(纯逻辑)+ 手动冒烟

- [ ] **Step 1: 写失败测试(规则净化保序逻辑)**

`frontend/src/__tests__/rules.test.js`:

```javascript
import { describe, it, expect } from 'vitest'
import { cleanRules, applyTemplate } from '../views/rules-helpers.js'

describe('rules helpers', () => {
  it('cleanRules drops empty value and keeps order', () => {
    const out = cleanRules([
      { type: 'full', value: 'a.com', outbound: 'direct', enabled: true },
      { type: 'full', value: '  ', outbound: 'direct', enabled: true },
      { type: 'geoip', value: 'cn', outbound: 'direct', enabled: false },
    ])
    expect(out.map((r) => r.value)).toEqual(['a.com', 'cn'])  // 空值丢弃,顺序不变
  })
  it('applyTemplate substitutes __PROXY__', () => {
    const tpl = [{ type: 'geosite', value: 'netflix', outbound: '__PROXY__' }]
    const out = applyTemplate(tpl, 'node-0')
    expect(out[0].outbound).toBe('node-0')
  })
})
```

- [ ] **Step 2: 运行确认失败**

Run: `cd frontend && npm run test -- rules`
Expected: FAIL — 无法解析 `../views/rules-helpers.js`

- [ ] **Step 3: 实现 rules-helpers.js**

`frontend/src/views/rules-helpers.js`:

```javascript
export const RULE_TYPES = [
  ['domain-suffix', '域名后缀'], ['full', '完整域名'], ['keyword', '关键字'],
  ['geosite', '预置集合'], ['ip', 'IP段'], ['geoip', '地区IP(如cn)'], ['port', '端口'],
]

export function cleanRules(rules) {
  return rules
    .filter((r) => (r.value || '').trim())
    .map((r) => ({ type: r.type, value: r.value.trim(), outbound: r.outbound, enabled: r.enabled !== false }))
}

export function applyTemplate(tpl, proxyTag) {
  return tpl.map((r) => ({ type: r.type, value: r.value, enabled: true,
    outbound: r.outbound === '__PROXY__' ? proxyTag : r.outbound }))
}
```

- [ ] **Step 4: 运行确认通过**

Run: `cd frontend && npm run test -- rules`
Expected: PASS

- [ ] **Step 5: 实现 Routing.vue(vuedraggable 拖拽 + 上移/下移)**

`frontend/src/views/Routing.vue`:

```vue
<script setup>
import { ref, onMounted } from 'vue'
import draggable from 'vuedraggable'
import { ElMessage } from 'element-plus'
import { routingApi } from '../api/index.js'
import { apiError } from '../api/http.js'
import { usePanel } from '../stores/panel.js'
import { RULE_TYPES, cleanRules, applyTemplate } from './rules-helpers.js'

const emit = defineEmits(['changed'])
const panel = usePanel()
const defaultOut = ref('')
const rules = ref([])
const templates = ref({})
const tplSel = ref('')

async function load() {
  await panel.refreshOutbounds()
  const { data } = await routingApi.get()
  defaultOut.value = data.default_outbound
  rules.value = data.rules.map((r) => ({ ...r }))
  templates.value = (await routingApi.templates()).data
}
onMounted(load)

function addRule() {
  rules.value.push({ type: 'domain-suffix', value: '', outbound: defaultOut.value || 'direct', enabled: true })
}
function move(i, d) {
  const j = i + d
  if (j < 0 || j >= rules.value.length) return
  const a = rules.value
  ;[a[i], a[j]] = [a[j], a[i]]
}
function doTemplate() {
  const tpl = templates.value[tplSel.value]
  if (!tpl) return
  rules.value.push(...applyTemplate(tpl, defaultOut.value || 'direct'))
  ElMessage.success('已追加模板规则,检查后保存')
}
async function save() {
  try {
    await routingApi.put({ default_outbound: defaultOut.value, rules: cleanRules(rules.value) })
    await panel.refreshAll(); emit('changed'); ElMessage.success('已保存路由')
    await load()
  } catch (e) { ElMessage.error(apiError(e)) }
}
function exportRules() {
  const blob = new Blob([JSON.stringify({ rules: cleanRules(rules.value) }, null, 2)], { type: 'application/json' })
  const a = document.createElement('a'); a.href = URL.createObjectURL(blob); a.download = 'xray-rules.json'; a.click()
}
function importRules(ev) {
  const f = ev.target.files[0]; if (!f) return
  const rd = new FileReader()
  rd.onload = () => { try { rules.value = (JSON.parse(rd.result).rules || []).map((r) => ({ ...r, enabled: r.enabled !== false })); ElMessage.success('已导入,检查后保存') }
    catch (e) { ElMessage.error('导入失败: ' + e.message) } }
  rd.readAsText(f); ev.target.value = ''
}
</script>

<template>
  <el-card>
    <template #header>默认出口(未命中规则的流量)</template>
    <el-select v-model="defaultOut" style="width:320px">
      <el-option v-for="o in panel.outbounds" :key="o.tag" :label="`${o.label} (${o.tag})`" :value="o.tag" />
    </el-select>
  </el-card>

  <el-card style="margin-top:16px;">
    <template #header>
      <div class="hd"><span>分流规则(自上而下,先命中先生效 · 可拖拽排序)</span>
        <div class="tools">
          <el-select v-model="tplSel" placeholder="套用模板…" style="width:160px">
            <el-option v-for="(_, k) in templates" :key="k" :label="k" :value="k" /></el-select>
          <el-button @click="doTemplate">追加</el-button>
          <el-button @click="exportRules">导出</el-button>
          <el-button @click="$refs.imp.click()">导入</el-button>
          <input ref="imp" type="file" accept="application/json" hidden @change="importRules" />
          <el-button type="primary" @click="addRule">+ 添加规则</el-button>
        </div></div>
    </template>

    <table class="rules">
      <thead><tr><th>#</th><th>排序</th><th>启用</th><th>匹配类型</th><th>匹配值</th><th>出口</th><th></th></tr></thead>
      <draggable v-model="rules" tag="tbody" item-key="_k" handle=".grip">
        <template #item="{ element, index }">
          <tr>
            <td class="grip">⠿ {{ index + 1 }}</td>
            <td><el-button-group>
              <el-button size="small" :disabled="index === 0" @click="move(index, -1)">↑</el-button>
              <el-button size="small" :disabled="index === rules.length - 1" @click="move(index, 1)">↓</el-button>
            </el-button-group></td>
            <td><el-switch v-model="element.enabled" /></td>
            <td><el-select v-model="element.type" size="small" style="width:130px">
              <el-option v-for="[v, l] in RULE_TYPES" :key="v" :label="l" :value="v" /></el-select></td>
            <td><el-input v-model="element.value" size="small" placeholder="如 google.com / cn / 443" /></td>
            <td><el-select v-model="element.outbound" size="small" style="width:200px">
              <el-option v-for="o in panel.outbounds" :key="o.tag" :label="o.label" :value="o.tag" /></el-select></td>
            <td><el-button size="small" type="danger" @click="rules.splice(index, 1)">删</el-button></td>
          </tr>
        </template>
      </draggable>
    </table>

    <div class="save"><el-button type="primary" @click="save">保存路由</el-button></div>
  </el-card>
</template>

<style scoped>
.hd { display:flex; justify-content:space-between; align-items:center; gap:8px; flex-wrap:wrap; }
.tools { display:flex; gap:6px; flex-wrap:wrap; }
.rules { width:100%; border-collapse:collapse; }
.rules th, .rules td { padding:6px 8px; border-bottom:1px solid var(--el-border-color); text-align:left; }
.grip { cursor:grab; color:var(--el-text-color-secondary); white-space:nowrap; }
.save { margin-top:12px; text-align:right; }
</style>
```

- [ ] **Step 6: 手动冒烟** — 拖拽改顺序 → 保存 → 重新加载顺序保持;上移/下移按钮可用;套用模板/导入导出;非法出口由后端拒绝。

- [ ] **Step 7: 提交**

```bash
git add frontend/src/views/Routing.vue frontend/src/views/rules-helpers.js frontend/src/__tests__/rules.test.js
git commit -m "feat(fe): 路由页(拖拽保序 + 上下移 + 模板 + 导入导出)"
```

---

## Task 11: 设置页

**Files:**
- Create: `frontend/src/views/Settings.vue`
- Test: 手动冒烟

> 后端改密码端点不在 Plan 1 范围。本任务前先给后端补一个 `PUT /api/auth/password`(见 Step 1),再做前端。

- [ ] **Step 1: 后端补改密码端点 + 测试**

在 `backend/routers/auth.py` 增加(并在 `backend/schemas.py` 加 `PasswordChangeIn`):

`backend/schemas.py` 追加:

```python
class PasswordChangeIn(BaseModel):
    old_password: str
    new_password: str
```

`backend/routers/auth.py` 追加(此路由需鉴权——由 `main.py` 的 `_mount_protected` 不含 auth,故单独加依赖):

```python
from backend.main import require_auth  # 延迟导入避免循环;或改用 make_auth_dependency

@router.put("/password", dependencies=[Depends(require_auth)])
def change_password(body: schemas.PasswordChangeIn, app: AppState = Depends(get_app_state)):
    from backend.security import verify_password, hash_password
    from backend.state import PasswordRec
    rec = app.state.password.model_dump() if app.state.password else None
    if not verify_password(rec, body.old_password):
        raise HTTPException(400, "原密码错误")
    if len(body.new_password) < 6:
        raise HTTPException(400, "新密码至少 6 位")
    app.state.password = PasswordRec(**hash_password(body.new_password))
    app.persist()
    return {"ok": True}
```

> 为避免 `auth.py` 反向 import `main` 造成循环,改为在 `auth.py` 顶部用 `from backend.deps import get_sessions` + `from backend.security import make_auth_dependency` 自建 `require_auth = make_auth_dependency(get_sessions)`,用于该端点的 `dependencies`。

后端测试 `tests/test_auth_api.py` 追加:

```python
def test_change_password(auth_client):
    r = auth_client.put("/api/auth/password",
                        json={"old_password": "testpw", "new_password": "newpw1"})
    assert r.status_code == 200


def test_change_password_wrong_old(auth_client):
    r = auth_client.put("/api/auth/password",
                        json={"old_password": "bad", "new_password": "newpw1"})
    assert r.status_code == 400
```

Run: `uv run pytest tests/test_auth_api.py -v` → PASS。提交后端改动。

- [ ] **Step 2: 前端 api 追加 + 实现 Settings.vue**

`frontend/src/api/index.js` 的 `authApi` 加:`changePassword: (oldp, newp) => http.put('/auth/password', { old_password: oldp, new_password: newp })`。

`frontend/src/views/Settings.vue`:

```vue
<script setup>
import { ref, reactive, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { authApi, subscriptionApi, xrayApi } from '../api/index.js'
import { apiError } from '../api/http.js'

const pw = reactive({ old_password: '', new_password: '' })
const sub = ref({})
const rawConfig = ref('')

async function load() {
  sub.value = (await subscriptionApi.get()).data
  try { rawConfig.value = JSON.stringify((await xrayApi.config()).data, null, 2) }
  catch (_) { rawConfig.value = '(尚未应用过配置)' }
}
onMounted(load)

async function changePw() {
  try {
    await authApi.changePassword(pw.old_password, pw.new_password)
    pw.old_password = ''; pw.new_password = ''; ElMessage.success('密码已修改')
  } catch (e) { ElMessage.error(apiError(e)) }
}
</script>

<template>
  <el-card>
    <template #header>修改密码</template>
    <el-form label-width="90px" style="max-width:420px">
      <el-form-item label="原密码"><el-input v-model="pw.old_password" type="password" show-password /></el-form-item>
      <el-form-item label="新密码"><el-input v-model="pw.new_password" type="password" show-password /></el-form-item>
      <el-form-item><el-button type="primary" @click="changePw">保存</el-button></el-form-item>
    </el-form>
  </el-card>

  <el-card style="margin-top:16px;">
    <template #header>订阅信息</template>
    <p>链接:{{ sub.url || '—' }}</p>
    <p>备注:{{ sub.remarks || '—' }} {{ sub.status }}</p>
    <p v-if="sub.fetched_at">拉取于:{{ new Date(sub.fetched_at * 1000).toLocaleString() }}</p>
  </el-card>

  <el-card style="margin-top:16px;">
    <template #header>生成的 config.json(只读)</template>
    <el-input v-model="rawConfig" type="textarea" :rows="16" readonly />
  </el-card>
</template>
```

- [ ] **Step 3: 手动冒烟** — 改密码(错误原密码弹错、成功后用新密码可重登);查看订阅信息与只读配置。

- [ ] **Step 4: 提交**

```bash
git add backend/ frontend/src tests/ && git commit -m "feat: 设置页 + 后端改密码端点"
```

---

## Task 12: 构建集成(Dockerfile 前端阶段)+ 全量验证

**Files:**
- Modify: `Dockerfile`(插入前端构建阶段)
- Modify: `.gitignore`(忽略 `frontend/node_modules`、`frontend/dist`、`frontend_dist`)
- Delete: 旧 `index.html`(已被 `frontend/` 取代)
- Test: 构建 + 端到端冒烟

- [ ] **Step 1: 删除旧单页**

```bash
git rm index.html
```

- [ ] **Step 2: 更新 .gitignore**

追加:

```
frontend/node_modules
frontend/dist
frontend_dist
```

- [ ] **Step 3: Dockerfile 插入前端构建阶段**

完整 `Dockerfile`:

```dockerfile
# 1) 取 xray 二进制 + geodata
FROM ghcr.io/xtls/xray-core:26.5.9 AS xray

# 2) 构建前端 → dist
FROM node:20-slim AS web
WORKDIR /web
COPY frontend/package.json frontend/package-lock.json* ./
RUN npm install
COPY frontend/ ./
RUN npm run build

# 3) python 运行时
FROM python:3.12-slim
COPY --from=xray /usr/local/bin/xray /usr/local/bin/xray
COPY --from=xray /usr/local/share/xray/ /usr/local/share/xray/
ENV XRAY_LOCATION_ASSET=/usr/local/share/xray
WORKDIR /app
RUN pip install --no-cache-dir uv
COPY pyproject.toml ./
RUN uv sync --no-dev
COPY backend/ ./backend/
COPY --from=web /web/dist ./frontend_dist/
EXPOSE 2017 10808 10809
CMD ["uv", "run", "uvicorn", "backend.main:app", "--host", "0.0.0.0", "--port", "2017"]
```

- [ ] **Step 4: 本地构建前端 + 拷给后端做本地端到端验证**

```bash
cd frontend && npm run build && cd ..
rm -rf frontend_dist && cp -r frontend/dist frontend_dist
uv run uvicorn backend.main:app --port 2017
```
浏览器开 `http://127.0.0.1:2017/` → 应由后端托管 SPA(非 dev server),完整跑一遍:登录 → 各页面 → 拉订阅 → 配置规则(拖拽排序)→ 应用并重启。
Expected: 全流程可用;刷新任意 `#/xxx` 路由不 404(hash 路由 + SPA fallback)。

- [ ] **Step 5: Docker 端到端验证**

```bash
docker compose up -d --build
docker compose logs | grep 密码        # 取首启密码
# 浏览器开 http://<host>:2017
```
Expected: 容器内 API + 前端均可用,行为与本地一致。

- [ ] **Step 6: 全量测试**

```bash
uv run pytest -v
cd frontend && npm run test
```
Expected: 后端全绿;前端单测全绿。

- [ ] **Step 7: 提交**

```bash
git add -A && git commit -m "feat: 多阶段 Dockerfile 集成前端构建 + 删除旧单页 UI"
```

---

## Self-Review(已核对)

- **Spec 覆盖**:§7 各页面 → Task 5-11(Dashboard/Inbound/Subscription/Balancers/Proxies/Routing/Settings);§7 侧边栏二级菜单 → Task 4 MainLayout;§5 两段式应用栏 → Task 4;§6 Bearer/401 → Task 2/3;§8 Dashboard 8 卡 + 两表 → Task 5;§9 构建 → Task 12;规则顺序可调(一等需求)→ Task 10(draggable + 上下移 + 保序测试)。
- **类型/接口一致**:`api/index.js` 各资源方法名(`inboundApi.create/update/remove` 等)在 Task 2 定义,Task 6-11 调用一致;`usePanel().refreshOutbounds/refreshStatus/refreshAll` 在 Task 4 定义,后续一致;`apiError`、`setToken/getToken/setUnauthHandler` 在 Task 2 定义,Task 3 使用一致;`cleanRules/applyTemplate/RULE_TYPES` 在 Task 10 定义并自测。
- **占位符**:无 TODO/TBD;每步含具体 .vue/.js 代码与命令。
- **跨计划依赖**:Task 11 需后端补 `PUT /api/auth/password`,已在该任务 Step 1 给出后端实现 + 测试(注意避免 `auth.py` 循环 import,用自建 `require_auth`)。

## 验收(Plan 2 完成时)

- [ ] 五功能域各有独立页面,侧边栏(出站二级菜单)可切换;刷新任意 hash 路由不 404。
- [ ] 所有现有功能在新 UI 可用:订阅拉取、测速、入站/代理/自动组 CRUD、规则(拖拽排序 + 上下移 + 模板 + 导入导出)、默认出口、应用并重启。
- [ ] 规则拖拽改序保存后顺序即生效优先级(后端保序已由 Plan 1 Task 12 测试覆盖)。
- [ ] 未登录访问任何页跳登录;任一请求 401 自动登出跳登录。
- [ ] 应用栏正确反映 dirty;Dashboard 显示 8 卡 + 两表。
- [ ] `frontend npm run test` 全绿;`docker compose up --build` 一键端到端可用。
