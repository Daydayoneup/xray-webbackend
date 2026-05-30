<script setup>
import { onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
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
      <el-main class="main"><router-view @changed="panel.refreshAll" /></el-main>
    </el-container>
  </el-container>
</template>

<style scoped>
.app { height:100vh; }
/* 深色侧边栏:整条统一深色,菜单透明继承底色,浅色文字 + 主题蓝激活 */
.side { background:#1a2029; display:flex; flex-direction:column; border-right:1px solid #2a323d; }
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
.top { background:#fff; display:flex; justify-content:space-between; align-items:center;
       border-bottom:1px solid var(--el-border-color); }
.status, .apply-bar { display:flex; align-items:center; gap:10px; }
.dirty { color:var(--el-color-warning); font-weight:600; }
.clean { color:var(--el-color-success); font-weight:500; }
.main { background:#f5f7fa; }
</style>
