<script setup>
import { ref, onMounted, computed } from 'vue'
import { subscriptionApi, inboundApi, routingApi, xrayApi } from '../api/index.js'
import { usePanel } from '../stores/panel.js'
import { aliveStats } from '../utils/format.js'

const panel = usePanel()
const nodes = ref([])
const inbounds = ref([])
const routing = ref({ rules: [], default_outbound: '' })
const subs = ref([])
const topo = ref({ applied: false, outbounds: [], routing: [] })

const stats = computed(() => aliveStats(nodes.value))
const enabledRules = computed(() => routing.value.rules.filter((r) => r.enabled).length)
const defaultLabel = computed(() => {
  const o = panel.outbounds.find((x) => x.tag === routing.value.default_outbound)
  return o ? o.label : (routing.value.default_outbound || '—')
})
const subLabel = computed(() => {
  if (!subs.value.length) return '未配置'
  return subs.value.map(s => s.remarks || s.url?.slice(0, 20)).join(', ')
})

onMounted(async () => {
  await panel.refreshAll()
  const [n, ib, rt, sl, tp] = await Promise.all([
    subscriptionApi.nodes(), inboundApi.list(), routingApi.get(),
    subscriptionApi.list(), xrayApi.topology(),
  ])
  nodes.value = n.data; inbounds.value = ib.data; routing.value = rt.data
  subs.value = sl.data; topo.value = tp.data
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
    <el-card><div class="lab">订阅</div><div class="val sm">{{ subLabel }}</div>
      <div class="sub" v-if="subs.length">共 {{ subs.length }} 个</div></el-card>
  </div>

  <div class="tables">
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
.tables { display:grid; grid-template-columns:1fr; gap:12px; margin-top:16px; }
.lab { font-size:12px; color:var(--el-text-color-secondary); }
.val { font-size:22px; font-weight:700; margin-top:4px; }
.val.sm { font-size:15px; } .val.ok { color:var(--el-color-success); }
.val.warn { color:var(--el-color-warning); } .val.err { color:var(--el-color-danger); }
.val.acc { color:var(--el-color-primary); }
.sub { font-size:12px; color:var(--el-text-color-secondary); margin-top:4px; }
@media (max-width:900px){ .grid{ grid-template-columns:repeat(2,1fr);} .tables{ grid-template-columns:1fr;} }
</style>
