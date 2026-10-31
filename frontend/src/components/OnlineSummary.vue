<script setup>
// 概览页的在线用户摘要：固定高度（最多 5 个入站 + 5 位用户），在线人数再多也不会拉长。
// 完整列表与上下线记录在「在线连接」页（OnlinePanel）。
import { ref, computed, onMounted, onBeforeUnmount } from 'vue'
import { opsApi } from '../api/index.js'
import { fmtBytes } from '../utils/format.js'

const ROWS = 5
const rep = ref(null)
let timer = null
async function load() {
  try { rep.value = (await opsApi.online({ sort: 'traffic', limit: ROWS, event_limit: 1 })).data } catch (_) {}
}
onMounted(() => { load(); timer = setInterval(load, 10000) })
onBeforeUnmount(() => clearInterval(timer))

const sum = computed(() => rep.value?.summary || { users: 0, addrs: 0, anonymous: 0, by_inbound: [] })
const users = computed(() => rep.value?.users || [])
const inbounds = computed(() => sum.value.by_inbound.slice(0, ROWS))
const maxUsers = computed(() => Math.max(1, ...inbounds.value.map((b) => b.users)))
const traffic = (u) => (rep.value?.metrics_enabled && (u.up || u.down || u.has_traffic) ? fmtBytes(u.up + u.down) : '—')
</script>

<template>
  <el-card class="online">
    <template #header>
      <div class="hd">
        <span>在线用户</span>
        <router-link to="/online" class="more">在线连接 →</router-link>
      </div>
    </template>
    <el-alert v-if="rep && !rep.running" type="info" :closable="false" show-icon title="Xray 未运行" />
    <el-empty v-else-if="rep && !sum.users" :image-size="50" description="当前没有设备连接到入站" />
    <div v-else-if="rep" class="grid">
      <div class="col">
        <div class="stats">
          <div><b>{{ sum.users }}</b><span>在线用户</span></div>
          <div><b>{{ sum.addrs }}</b><span>来源地址</span></div>
          <div v-if="sum.anonymous"><b>{{ sum.anonymous }}</b><span>未鉴权</span></div>
        </div>
        <div class="sub">按入站</div>
        <router-link v-for="b in inbounds" :key="b.inbound" :to="{ path: '/online', query: { inbound: b.inbound } }"
                     class="bar-row" :title="`${b.label}：${b.users} 位在线`">
          <span class="lbl">{{ b.label }}</span>
          <span class="track"><i :style="{ width: (100 * b.users / maxUsers) + '%' }" /></span>
          <span class="num">{{ b.users }}</span>
        </router-link>
        <router-link v-if="sum.by_inbound.length > ROWS" to="/online" class="muted">另有 {{ sum.by_inbound.length - ROWS }} 个入站 →</router-link>
      </div>
      <div class="col">
        <div class="sub">本次流量最多</div>
        <div v-for="u in users" :key="u.inbound + (u.email || u.addrs[0]?.ip)" class="u-row">
          <b class="name">{{ u.user || '未鉴权' }}</b>
          <span class="muted ell">{{ u.inbound_label }}</span>
          <span class="ip">{{ u.addrs[0]?.ip }}<span v-if="u.addrs.length > 1" class="muted"> +{{ u.addrs.length - 1 }}</span></span>
          <span class="tr">{{ traffic(u) }}</span>
        </div>
        <router-link v-if="sum.users > users.length" to="/online" class="muted">查看全部 {{ sum.users }} 位 →</router-link>
      </div>
    </div>
  </el-card>
</template>

<style scoped>
.online { margin-top:12px; }
.hd { display:flex; justify-content:space-between; align-items:center; }
a { color:var(--el-color-primary); text-decoration:none; }
.more { font-size:13px; }
.muted { color:var(--el-text-color-secondary); font-size:12px; }
.grid { display:grid; grid-template-columns:1fr 1.4fr; gap:28px; }
.col { display:flex; flex-direction:column; gap:6px; min-width:0; }
.stats { display:flex; gap:24px; margin-bottom:6px; }
.stats div { display:flex; align-items:baseline; gap:6px; }
.stats b { font-size:22px; color:var(--el-text-color-primary); }
.stats span, .sub { font-size:12px; color:var(--el-text-color-secondary); }
.bar-row { display:grid; grid-template-columns:minmax(0, 13em) 1fr 3em; align-items:center; gap:8px; font-size:13px;
           color:var(--el-text-color-regular); line-height:24px; }
.bar-row:hover .lbl { color:var(--el-color-primary); }
.lbl, .ell { overflow:hidden; text-overflow:ellipsis; white-space:nowrap; }
.track { height:8px; background:var(--el-fill-color); border-radius:4px; overflow:hidden; }
.track i { display:block; height:100%; background:var(--el-color-primary); border-radius:4px; min-width:4px; }
.num { text-align:right; font-variant-numeric:tabular-nums; }
.u-row { display:grid; grid-template-columns:minmax(0, 7em) minmax(0, 1fr) auto 5.5em; gap:10px; align-items:center;
         font-size:13px; line-height:24px; }
.name { overflow:hidden; text-overflow:ellipsis; white-space:nowrap; }
.ip { font-family:ui-monospace, SFMono-Regular, Menlo, monospace; font-size:12px; }
.tr { text-align:right; font-variant-numeric:tabular-nums; }
@media (max-width:900px) { .grid { grid-template-columns:1fr; gap:16px; } }
</style>
