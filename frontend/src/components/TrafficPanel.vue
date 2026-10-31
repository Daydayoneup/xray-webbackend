<script setup>
import { ref, reactive, computed, onMounted, onBeforeUnmount, watch } from 'vue'
import { opsApi } from '../api/index.js'
import { fmtBytes, fmtTime } from '../utils/format.js'

const days = ref(7)
const rep = ref(null)
const hover = ref(-1)
let timer = null
// 按容器实际宽度绘制，文字与柱宽保持真实像素大小
const wrap = ref(null)
const W = ref(720)
let ro = null

async function load() {
  try { rep.value = (await opsApi.traffic(days.value)).data } catch (_) { rep.value = null }
}
onMounted(() => {
  load(); timer = setInterval(load, 30000)
  ro = new ResizeObserver(([e]) => { if (e.contentRect.width > 0) W.value = Math.round(e.contentRect.width) })
  watch(wrap, (el) => { if (el) ro.observe(el) }, { immediate: true })
})
onBeforeUnmount(() => { clearInterval(timer); ro?.disconnect() })
watch(days, load)

// 柱状图：单系列（每日合计），悬停显示上传/下载
const H = 180, PAD_L = 8, PAD_B = 22, PAD_T = 10
const bars = computed(() => {
  const d = rep.value?.daily || []
  const max = Math.max(1, ...d.map((x) => x.up + x.down))
  const slot = (W.value - PAD_L * 2) / Math.max(1, d.length)
  const bw = Math.max(4, Math.min(28, slot - 6))
  return d.map((x, i) => {
    const total = x.up + x.down
    const h = total ? Math.max(2, ((H - PAD_B - PAD_T) * total) / max) : 0
    return { ...x, total, x: PAD_L + i * slot + (slot - bw) / 2, w: bw, h, y: H - PAD_B - h, cx: PAD_L + i * slot + slot / 2 }
  })
})
const labelEvery = computed(() => Math.ceil(bars.value.length / Math.max(2, Math.floor(W.value / 60))))
const hovered = computed(() => bars.value[hover.value])
const shortDate = (s) => s.slice(5)

// 明细表：默认只显示流量最多的前 TOP 项，可展开（展开后表格内滚动，不拉长页面）
const TOP = 8
const TABLES = [
  { key: 'outbound', title: '按出口', col: '出口（节点 / 代理）', empty: '暂无数据' },
  { key: 'inbound', title: '按入站', col: '入站', empty: '暂无数据' },
]
const expanded = reactive({ outbound: false, inbound: false })
const rows = (key) => (expanded[key] ? rep.value[key] : rep.value[key].slice(0, TOP))
</script>

<template>
  <div class="traffic">
  <el-card>
    <template #header>
      <div class="hd"><span>流量统计</span>
        <el-radio-group v-model="days" size="small">
          <el-radio-button :value="1">今天</el-radio-button>
          <el-radio-button :value="7">近 7 天</el-radio-button>
          <el-radio-button :value="30">近 30 天</el-radio-button>
        </el-radio-group></div>
    </template>
    <el-alert v-if="rep && !rep.enabled" type="info" :closable="false" show-icon title="流量统计未开启（环境变量 METRICS_PORT=0）" />
    <template v-else-if="rep">
      <el-alert v-if="rep.error" type="warning" :closable="false" show-icon :title="rep.error" style="margin-bottom:12px" />
      <div class="tiles">
        <div class="tile"><div class="lab">合计</div><div class="val">{{ fmtBytes(rep.total.up + rep.total.down) }}</div></div>
        <div class="tile"><div class="lab">上传</div><div class="val sm">{{ fmtBytes(rep.total.up) }}</div></div>
        <div class="tile"><div class="lab">下载</div><div class="val sm">{{ fmtBytes(rep.total.down) }}</div></div>
        <div class="tile"><div class="lab">最近采样</div><div class="val sm">{{ rep.live_at > 0 ? fmtTime(rep.live_at) : '—' }}</div></div>
      </div>

      <div v-if="days > 1" ref="wrap" class="chart-wrap" @mouseleave="hover = -1">
        <div class="chart-title">每日流量</div>
        <svg :width="W" :height="H" class="chart" role="img" aria-label="每日流量柱状图">
          <line :x1="PAD_L" :x2="W - PAD_L" :y1="H - PAD_B" :y2="H - PAD_B" class="axis" />
          <g v-for="(b, i) in bars" :key="b.date">
            <rect :x="b.cx - (W / bars.length) / 2" :y="PAD_T" :width="W / bars.length" :height="H - PAD_B - PAD_T"
                  fill="transparent" @mouseenter="hover = i" />
            <path v-if="b.h" :d="`M${b.x},${H - PAD_B} V${b.y + Math.min(4, b.h)} Q${b.x},${b.y} ${b.x + Math.min(4, b.w / 2)},${b.y} H${b.x + b.w - Math.min(4, b.w / 2)} Q${b.x + b.w},${b.y} ${b.x + b.w},${b.y + Math.min(4, b.h)} V${H - PAD_B} Z`"
                  class="bar" :class="{ dim: hover >= 0 && hover !== i }" pointer-events="none" />
            <text v-if="i % labelEvery === 0 || i === bars.length - 1" :x="b.cx" :y="H - 6" class="tick">{{ shortDate(b.date) }}</text>
          </g>
        </svg>
        <div v-if="hovered" class="tip" :style="{ left: `${(hovered.cx / W) * 100}%` }">
          <b>{{ hovered.date }}</b>
          <div>合计 {{ fmtBytes(hovered.total) }}</div>
          <div class="muted">上传 {{ fmtBytes(hovered.up) }} · 下载 {{ fmtBytes(hovered.down) }}</div>
        </div>
      </div>
    </template>
  </el-card>

  <div v-if="rep?.enabled" class="tables">
    <el-card v-for="t in TABLES" :key="t.key">
      <template #header>
        {{ t.title }}
      </template>
      <el-table :data="rows(t.key)" size="small" :empty-text="t.empty" :max-height="expanded[t.key] ? 420 : undefined">
        <el-table-column prop="label" :label="t.col" min-width="110" show-overflow-tooltip />
        <el-table-column label="上传" width="88" align="right"><template #default="{ row }">{{ fmtBytes(row.up) }}</template></el-table-column>
        <el-table-column label="下载" width="88" align="right"><template #default="{ row }">{{ fmtBytes(row.down) }}</template></el-table-column>
      </el-table>
      <el-button v-if="rep[t.key].length > TOP" link type="primary" size="small" class="more"
                 @click="expanded[t.key] = !expanded[t.key]">
        {{ expanded[t.key] ? '收起' : `展开全部 ${rep[t.key].length} 项` }}</el-button>
    </el-card>
  </div>
  </div>
</template>

<style scoped>
.traffic { margin-top:16px; }
.hd { display:flex; justify-content:space-between; align-items:center; gap:8px; flex-wrap:wrap; }
.tiles { display:grid; grid-template-columns:repeat(4, 1fr); gap:12px; }
.tile { padding:4px 0; }
.lab { font-size:12px; color:var(--el-text-color-secondary); }
.val { font-size:22px; font-weight:700; margin-top:4px; color:var(--el-text-color-primary); }
.val.sm { font-size:15px; }
.chart-wrap { position:relative; margin-top:16px; }
.chart-title { font-size:13px; color:var(--el-text-color-secondary); margin-bottom:4px; }
.chart { display:block; max-width:100%; }
.axis { stroke:var(--el-border-color); stroke-width:1; }
.bar { fill:var(--el-color-primary); transition:opacity .15s; }
.bar.dim { opacity:.45; }
.tick { font-size:11px; fill:var(--el-text-color-secondary); text-anchor:middle; }
.tip { position:absolute; top:24px; transform:translateX(-50%); background:var(--el-bg-color-overlay); border:1px solid var(--el-border-color);
       border-radius:6px; padding:6px 10px; font-size:12px; color:var(--el-text-color-primary); pointer-events:none; white-space:nowrap;
       box-shadow:var(--el-box-shadow-light); }
/* 两张表并排，各自只显示前 TOP 项；align-items:start 避免短表被长表拉高。按用户的流量在「用户」页 */
.tables { display:grid; grid-template-columns:repeat(2, minmax(0, 1fr)); gap:16px; margin-top:16px; align-items:start; }
.more { margin-top:8px; }
@media (max-width:900px) { .tiles { grid-template-columns:repeat(2, 1fr); } .tables { grid-template-columns:1fr; } }
</style>
