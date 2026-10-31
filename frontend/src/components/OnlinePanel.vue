<script setup>
// 在线连接页：在线用户与上下线记录。搜索、按入站筛选、排序、分页均由后端处理，用户再多也只传一页。
// 概览页用的是固定高度的 OnlineSummary。
import { ref, computed, onMounted, onBeforeUnmount, watch } from 'vue'
import { useRoute } from 'vue-router'
import { opsApi } from '../api/index.js'
import { fmtBytes, fmtTime, fmtAgo, fmtDur, isPrivateIP } from '../utils/format.js'

const route = useRoute()

const rep = ref(null)
const now = ref(Date.now() / 1000)
const q = ref('')
const inbound = ref(route.query.inbound || '')
const sort = ref('traffic')
const page = ref(1)
const size = ref(20)
const eventKind = ref('')
const evPage = ref(1)
const evSize = ref(20)
let timer = null

const SORTS = [['traffic', '本次流量'], ['recent', '最近活动'], ['duration', '在线时长'], ['conns', '连接数'], ['name', '用户名']]
const EVENT_TEXT = { online: '上线', new_ip: '新地址', offline: '下线' }
const EVENT_TYPE = { online: 'success', new_ip: 'warning', offline: 'info' }

async function load() {
  const params = { q: q.value.trim(), inbound: inbound.value, sort: sort.value, offset: (page.value - 1) * size.value,
        limit: size.value, event_kind: eventKind.value, event_offset: (evPage.value - 1) * evSize.value, event_limit: evSize.value }
  try { rep.value = (await opsApi.online(params)).data } catch (_) {}
  now.value = Date.now() / 1000
}
onMounted(() => { load(); timer = setInterval(load, 10000) })
onBeforeUnmount(() => clearInterval(timer))
let qTimer = null
// 搜索和入站筛选同时作用于在线列表和上下线记录
watch(q, () => { clearTimeout(qTimer); qTimer = setTimeout(() => { page.value = 1; evPage.value = 1; load() }, 300) })
watch(inbound, () => { page.value = 1; evPage.value = 1; load() })
watch([sort, size], () => { page.value = 1; load() })
watch([eventKind, evSize], () => { evPage.value = 1; load() })
watch([page, evPage], load)

const users = computed(() => rep.value?.users || [])
const sum = computed(() => rep.value?.summary || { users: 0, addrs: 0, anonymous: 0, by_inbound: [] })
const idleMin = computed(() => Math.round((rep.value?.idle_seconds || 180) / 60))
const showTraffic = (row) => rep.value?.metrics_enabled && (row.up || row.down || row.has_traffic)
const ADDRS_SHOWN = 3
</script>

<template>
  <el-card class="online">
    <template #header>
      <div class="hd">
        <span>在线用户
          <el-tooltip placement="top" :content="`根据 Xray 访问日志识别：每个连接的来源地址、入站和用户。Xray 不报告断开，${idleMin} 分钟内没有新连接也没有流量即视为掉线。`">
            <span class="q">ⓘ</span>
          </el-tooltip>
        </span>
      </div>
    </template>

    <el-alert v-if="rep && !rep.running" type="info" :closable="false" show-icon title="Xray 未运行" />
    <template v-else-if="rep">
      <div class="sum">
        <div class="stat"><b>{{ sum.users }}</b><span>在线用户</span></div>
        <div class="stat"><b>{{ sum.addrs }}</b><span>来源地址</span></div>
        <div v-if="sum.anonymous" class="stat"><b>{{ sum.anonymous }}</b><span>未鉴权来源</span></div>
        <div class="by">
          <el-tag v-for="b in sum.by_inbound.slice(0, 50)" :key="b.inbound" size="small" class="clickable"
                  :effect="inbound === b.inbound ? 'dark' : 'plain'"
                  @click="inbound = inbound === b.inbound ? '' : b.inbound">{{ b.label }} · {{ b.users }}</el-tag>
        </div>
      </div>

      <div class="filters">
        <el-input v-model="q" clearable placeholder="搜索用户名 / IP / 入站" style="width:240px" />
        <el-select v-model="inbound" clearable placeholder="全部入站" style="width:180px">
          <el-option v-for="b in sum.by_inbound" :key="b.inbound" :value="b.inbound" :label="`${b.label}（${b.users}）`" />
        </el-select>
        <el-select v-model="sort" style="width:140px">
          <el-option v-for="[v, l] in SORTS" :key="v" :value="v" :label="'按' + l" />
        </el-select>
        <span class="muted">筛选后 {{ rep.total }} 位</span>
      </div>

      <el-empty v-if="!sum.users" :image-size="60" description="当前没有设备连接到入站" />
      <el-empty v-else-if="!users.length" :image-size="60" description="没有符合条件的在线用户" />
      <el-table v-else :data="users" size="small">
        <el-table-column label="用户" min-width="110">
          <template #default="{ row }">
            <b v-if="row.user">{{ row.user }}</b>
            <el-tooltip v-else content="该入站没有开启用户鉴权，按来源地址区分" placement="top"><span class="muted">未鉴权</span></el-tooltip>
          </template>
        </el-table-column>
        <el-table-column label="入站 / 协议" min-width="140">
          <template #default="{ row }"><div>{{ row.inbound_label }}</div><div class="muted">{{ row.protocol }}</div></template>
        </el-table-column>
        <el-table-column label="来源地址" min-width="200">
          <template #default="{ row }">
            <div v-for="a in row.addrs.slice(0, ADDRS_SHOWN)" :key="a.ip" class="addr">
              <el-tooltip placement="top">
                <template #content>
                  首次连接 {{ fmtTime(a.first_seen) }}<br>最近活动 {{ fmtAgo(a.last_seen, now) }} · {{ a.conns }} 个连接<br>
                  最近访问 {{ a.last_dest }} → {{ a.outbound }}
                </template>
                <span class="ip">{{ a.ip }}</span>
              </el-tooltip>
              <el-tag size="small" :type="isPrivateIP(a.ip) ? 'info' : 'primary'" effect="plain">{{ isPrivateIP(a.ip) ? '内网' : '公网' }}</el-tag>
              <span class="muted">{{ a.networks.join('/').toUpperCase() }}</span>
            </div>
            <el-tooltip v-if="row.addrs.length > ADDRS_SHOWN" placement="top">
              <template #content><div v-for="a in row.addrs" :key="a.ip">{{ a.ip }}</div></template>
              <span class="muted">另有 {{ row.addrs.length - ADDRS_SHOWN }} 个地址</span>
            </el-tooltip>
          </template>
        </el-table-column>
        <el-table-column label="在线时长" width="105"><template #default="{ row }">{{ fmtDur(now - row.first_seen) }}</template></el-table-column>
        <el-table-column label="连接数" prop="conns" width="70" align="right" />
        <el-table-column label="本次流量" width="120" align="right">
          <template #default="{ row }">
            <template v-if="showTraffic(row)"><div>↑ {{ fmtBytes(row.up) }}</div><div>↓ {{ fmtBytes(row.down) }}</div></template>
            <el-tooltip v-else placement="top" :content="rep.metrics_enabled ? '未鉴权入站只有入站总流量，多个来源同时在线时无法区分' : '流量统计未开启（METRICS_PORT=0）'">
              <span class="muted">—</span>
            </el-tooltip>
          </template>
        </el-table-column>
        <el-table-column label="今日流量" width="100" align="right">
          <template #default="{ row }">
            <span v-if="row.has_traffic && rep.metrics_enabled">{{ fmtBytes(row.today_up + row.today_down) }}</span>
            <span v-else class="muted">—</span>
          </template>
        </el-table-column>
      </el-table>

      <el-pagination v-if="rep.total > size" v-model:current-page="page" v-model:page-size="size" class="pager"
                     :total="rep.total" :page-sizes="[20, 50, 100]" layout="total, sizes, prev, pager, next" background small />

      <div v-if="rep.events_total || eventKind" class="events">
        <div class="ev-hd">
          <span>最近上下线（保留最近 500 条，面板重启后清空）</span>
          <el-radio-group v-model="eventKind" size="small">
            <el-radio-button value="">全部</el-radio-button>
            <el-radio-button value="online">上线</el-radio-button>
            <el-radio-button value="offline">下线</el-radio-button>
          </el-radio-group>
        </div>
        <div v-for="(e, i) in rep.events" :key="i" class="ev">
          <el-tag size="small" :type="EVENT_TYPE[e.kind]">{{ EVENT_TEXT[e.kind] }}</el-tag>
          <span class="t">{{ fmtTime(e.time) }}</span>
          <b>{{ e.user || '未鉴权' }}</b>
          <span class="muted">{{ e.inbound_label }}</span>
          <span class="ip">{{ e.ip }}</span>
          <span v-if="e.kind === 'offline'" class="muted">在线 {{ fmtDur(e.duration) }}<template v-if="e.up || e.down"> · ↑ {{ fmtBytes(e.up) }} ↓ {{ fmtBytes(e.down) }}</template></span>
        </div>
        <el-empty v-if="!rep.events.length" :image-size="40" description="没有符合条件的记录" />
        <el-pagination v-if="rep.events_total > evSize" v-model:current-page="evPage" v-model:page-size="evSize" class="pager"
                       :total="rep.events_total" :page-sizes="[20, 50, 100]" layout="total, sizes, prev, pager, next" background small />
      </div>
    </template>
  </el-card>
</template>

<style scoped>
.online { margin-top:12px; }
.hd { display:flex; justify-content:space-between; align-items:center; gap:8px; }
a { color:var(--el-color-primary); text-decoration:none; }
.q { margin-left:4px; font-size:13px; color:var(--el-text-color-secondary); cursor:help; }
.muted { color:var(--el-text-color-secondary); font-size:12px; }
.clickable { cursor:pointer; }
.sum { display:flex; align-items:center; gap:24px; flex-wrap:wrap; margin-bottom:12px; }
.stat { display:flex; align-items:baseline; gap:6px; }
.stat b { font-size:22px; }
.stat span { font-size:12px; color:var(--el-text-color-secondary); }
.by { display:flex; flex-wrap:wrap; gap:6px; flex:1; }
.filters { display:flex; gap:8px; align-items:center; flex-wrap:wrap; margin-bottom:12px; }
.addr { display:flex; align-items:center; gap:6px; line-height:24px; }
.ip { font-family:ui-monospace, SFMono-Regular, Menlo, monospace; font-size:12px; }
.pager { margin-top:12px; justify-content:flex-end; }
.events { margin-top:12px; border-top:1px solid var(--el-border-color-lighter); padding-top:10px; }
.ev-hd { display:flex; justify-content:space-between; align-items:center; font-size:12px; color:var(--el-text-color-secondary); margin-bottom:6px; }
.ev { display:flex; flex-wrap:wrap; align-items:center; gap:8px; font-size:13px; line-height:26px; }
.ev .el-tag { min-width:52px; justify-content:center; }
.ev .t { color:var(--el-text-color-secondary); font-size:12px; min-width:140px; }
</style>
