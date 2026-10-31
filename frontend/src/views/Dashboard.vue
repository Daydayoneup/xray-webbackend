<script setup>
import { ref, onMounted, computed } from 'vue'
import { subscriptionApi, inboundApi, routingApi, xrayApi } from '../api/index.js'
import TrafficPanel from '../components/TrafficPanel.vue'
import OnlineSummary from '../components/OnlineSummary.vue'
import { usePanel } from '../stores/panel.js'
import { subName } from '../utils/format.js'

const panel = usePanel()
const nodes = ref([])
const inbounds = ref([])
const routing = ref({ rules: [], default_outbound: '' })
const subs = ref([])
const topo = ref({ applied: false, outbounds: [], routing: [] })

const failedSubs = computed(() => subs.value.filter((s) => s.last_error))
const outCount = computed(() => {
  const by = (k) => panel.outbounds.filter((o) => o.kind === k).length
  const r = { sub: by('node'), proxy: by('proxy'), nodes: by('node') + by('proxy'), groups: by('balancer'), builtin: by('builtin') }
  return { ...r, total: r.nodes + r.groups + r.builtin }
})

// 上手引导：按顺序检查必需的设置
const steps = computed(() => {
  const outs = panel.outbounds.filter((o) => (o.kind === 'node' || o.kind === 'proxy'))
  const def = panel.outboundMap[routing.value.default_outbound]
  return [
    { done: outs.length > 0, title: '添加出口', desc: '添加机场订阅，或手动添加自定义代理', to: '/outbound/subscription' },
    { done: !!def && def.available && def.tag !== 'direct', title: '设置默认出口',
      desc: def && !def.available ? `当前默认出口「${def.label}」不可用` : '建议先建一个节点组，再把默认出口设为它', to: '/routing' },
    { done: routing.value.rules.length > 0, title: '设置分流规则（可选）', desc: '套用「国内直连，国外走代理」模板，国内网站不绕路', to: '/routing' },
    { done: inbounds.value.length > 0, title: '确认入站端口', desc: '设备通过这些端口连接；外网使用建议用 VLESS + Reality', to: '/inbound' },
    { done: panel.status.applied && !panel.status.dirty, title: '应用配置', desc: '点击右上角「应用并重启」，修改才会生效' },
  ]
})
const doneCount = computed(() => steps.value.filter((x) => x.done).length)
const enabledRules = computed(() => routing.value.rules.filter((r) => r.enabled).length)
const defaultLabel = computed(() => {
  const o = panel.outbounds.find((x) => x.tag === routing.value.default_outbound)
  return o ? o.label : (routing.value.default_outbound || '—')
})

onMounted(async () => {
  await panel.refreshAll()
  const [n, ib, rt, sl, tp] = await Promise.all([
    subscriptionApi.nodes(), inboundApi.list(), routingApi.get(),
    subscriptionApi.list(), xrayApi.topology(),
  ]).catch(() => [{ data: [] }, { data: [] }, { data: { rules: [] } }, { data: [] }, { data: { routing: [] } }])
  nodes.value = n.data; inbounds.value = ib.data; routing.value = rt.data
  subs.value = sl.data; topo.value = tp.data
})
</script>

<template>
  <el-card v-if="doneCount < steps.length" class="guide">
    <template #header>上手指引（{{ doneCount }} / {{ steps.length }}）</template>
    <div class="steps">
      <div v-for="(st, i) in steps" :key="i" class="step" :class="{ done: st.done }">
        <span class="dot">{{ st.done ? '✓' : i + 1 }}</span>
        <div class="grow"><b>{{ st.title }}</b><div class="muted">{{ st.desc }}</div></div>
        <el-button v-if="!st.done && st.to" size="small" @click="$router.push(st.to)">去设置</el-button>
      </div>
    </div>
  </el-card>

  <div class="grid">
    <el-card class="clickable" @click="$router.push('/inbound')"><div class="lab">入站</div>
      <div class="val">{{ inbounds.length }}</div>
      <div class="sub">{{ inbounds.map(i => i.port).join(' · ') || '—' }}</div></el-card>
    <el-card class="clickable" @click="$router.push('/outbound/subscription')"><div class="lab">出站</div>
      <div class="val">{{ outCount.total }}</div>
      <div class="sub" :title="`订阅节点 ${outCount.sub} · 自定义代理 ${outCount.proxy}`">
        节点 {{ outCount.nodes }}（订阅 {{ outCount.sub }} · 自定义 {{ outCount.proxy }}）· 节点组 {{ outCount.groups }} · 内置 {{ outCount.builtin }}</div></el-card>
    <el-card class="clickable" @click="$router.push('/routing')"><div class="lab">分流规则</div>
      <div class="val">{{ enabledRules }}<span class="of"> / {{ routing.rules.length }}</span></div>
      <div class="sub">{{ routing.rules.length ? `已启用 ${enabledRules} 条` + (routing.rules.length > enabledRules ? `，停用 ${routing.rules.length - enabledRules} 条` : '') : '未设置，全部流量走默认出口' }}</div></el-card>
    <el-card class="clickable" @click="$router.push('/routing')"><div class="lab">默认出口</div>
      <div class="val sm">{{ defaultLabel }}</div>
      <div class="sub">未命中分流规则的流量走这里</div></el-card>
  </div>

  <el-alert v-if="!panel.status.applied" type="info" show-icon :closable="false" class="tip"
            title="还没有应用过配置：添加订阅或自定义代理、设置路由后，点击右上角「应用并重启」启动 Xray。" />
  <el-alert v-if="panel.status.warnings?.length" type="warning" show-icon :closable="false" class="tip"
            :title="`当前配置有 ${panel.status.warnings.length} 条提示`">
    <ul class="warn"><li v-for="(w, i) in panel.status.warnings" :key="i">{{ w }}</li></ul>
  </el-alert>
  <el-alert v-if="failedSubs.length" type="error" show-icon :closable="false" class="tip"
            :title="`${failedSubs.length} 个订阅上次更新失败（已保留原有节点）`">
    <div v-for="s in failedSubs" :key="s.id">{{ subName(s) }}：{{ s.last_error }}</div>
  </el-alert>

  <OnlineSummary />
  <TrafficPanel />

  <div class="tables">
    <el-card>
      <template #header>当前生效的路由（自上而下，先命中先生效）</template>
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
.clickable { cursor:pointer; }
.tip { margin-top:12px; }
.guide { margin-bottom:12px; }
.steps { display:grid; gap:10px; }
.step { display:flex; gap:12px; align-items:center; }
.step .dot { width:24px; height:24px; border-radius:50%; display:inline-flex; align-items:center; justify-content:center; font-size:12px;
             background:var(--el-fill-color); color:var(--el-text-color-secondary); flex-shrink:0; }
.step.done .dot { background:var(--el-color-success); color:#fff; }
.step.done b { color:var(--el-text-color-secondary); font-weight:500; }
.grow { flex:1; }

.warn { margin:4px 0 0; padding-left:18px; }
.tables { display:grid; grid-template-columns:1fr; gap:12px; margin-top:16px; }
.lab { font-size:12px; color:var(--el-text-color-secondary); }
.val { font-size:22px; font-weight:700; margin-top:4px; }
.val.sm { font-size:15px; } .val.ok { color:var(--el-color-success); }
.val.warn { color:var(--el-color-warning); } .val.err { color:var(--el-color-danger); }
.val.acc { color:var(--el-color-primary); }
.val .of { font-size:14px; font-weight:400; color:var(--el-text-color-secondary); }
.sub { font-size:12px; color:var(--el-text-color-secondary); margin-top:4px; }
@media (max-width:900px){ .grid{ grid-template-columns:repeat(2,1fr);} .tables{ grid-template-columns:1fr;} }
</style>
