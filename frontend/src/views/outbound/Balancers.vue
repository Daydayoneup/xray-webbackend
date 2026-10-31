<script setup>
import { ref, onMounted, onBeforeUnmount, reactive, computed, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { balancerApi, subscriptionApi } from '../../api/index.js'
import { apiError } from '../../api/http.js'
import { usePanel } from '../../stores/panel.js'
import { subName } from '../../utils/format.js'
import { showApplyError } from '../../utils/apply.js'

const emit = defineEmits(['changed'])
const panel = usePanel()
const list = ref([])
const status = ref({})
const subs = ref([])
const dialog = ref(false)
const saving = ref(false)
const editing = ref(null)
const form = reactive({ name: '', strategy: 'leastPing', filter: '', filter_sub_id: null, nodes: [], selected: '' })
const showStatic = ref(false)
const preview = ref([])
const STRATEGIES = [
  ['leastPing', '自动选最快', '自动使用延迟最低的可用节点，故障时自动切换（推荐）'],
  ['select', '手动选择', '由你指定使用哪个节点，可随时一键切换'],
  ['random', '随机', '每个连接随机挑一个可用节点'],
  ['roundRobin', '轮询', '依次轮流使用各节点，分摊流量'],
]
const strategyLabel = (s) => (STRATEGIES.find(([v]) => v === s) || [s, s])[1]
let timer = null

async function load() {
  list.value = (await balancerApi.list()).data
  try { subs.value = (await subscriptionApi.list()).data } catch (_) { subs.value = [] }
  await panel.refreshOutbounds()
  await loadStatus()
}
async function loadStatus() {
  try { status.value = Object.fromEntries((await balancerApi.status()).data.map((g) => [g.tag, g])) } catch (_) {}
}
onMounted(() => { load(); timer = setInterval(loadStatus, 30000) })
onBeforeUnmount(() => clearInterval(timer))

const candidates = computed(() => panel.outbounds.filter((o) => o.kind === 'node' || o.kind === 'proxy'))
const transferData = computed(() => candidates.value.map((o) => ({ key: o.tag, label: `${o.label}${o.available ? '' : `（${o.reason}）`}` })))

let pvTimer = null
watch(() => [form.filter, form.filter_sub_id, form.nodes.length], () => {
  clearTimeout(pvTimer)
  pvTimer = setTimeout(async () => {
    if (!dialog.value) return
    try { preview.value = (await balancerApi.preview({ filter: form.filter, filter_sub_id: form.filter_sub_id, nodes: form.nodes })).data }
    catch (_) { preview.value = [] }
  }, 250)
})

function openCreate() {
  editing.value = null
  Object.assign(form, { name: '', strategy: 'leastPing', filter: '', filter_sub_id: null, nodes: [], selected: '' })
  showStatic.value = false; preview.value = []
  dialog.value = true
}
function openEdit(row) {
  editing.value = row.tag
  Object.assign(form, { name: row.name, strategy: row.strategy || 'leastPing', filter: row.filter || '',
    filter_sub_id: row.filter_sub_id ?? null, nodes: [...(row.nodes || [])], selected: row.selected || '' })
  showStatic.value = form.nodes.length > 0
  dialog.value = true
}
async function save() {
  saving.value = true
  try {
    const body = { ...form, nodes: showStatic.value ? form.nodes : [] }
    if (editing.value) await balancerApi.update(editing.value, body)
    else await balancerApi.create(body)
    dialog.value = false; await load(); await panel.refreshAll(); emit('changed'); ElMessage.success('已保存，应用后生效')
  } catch (e) { ElMessage.error(apiError(e)) }
  finally { saving.value = false }
}
async function remove(row) {
  const refs = panel.outboundMap[row.tag]?.refs || 0
  const msg = refs
    ? `节点组「${row.name}」正被 ${refs} 处规则/默认出口使用，删除后这些规则会被移除（默认出口会改为第一个可用节点）。确定删除？`
    : `删除节点组「${row.name}」？`
  try { await ElMessageBox.confirm(msg, '确认删除', { type: 'warning', confirmButtonText: '删除' }) } catch (_) { return }
  try { await balancerApi.remove(row.tag); await load(); await panel.refreshAll(); emit('changed') }
  catch (e) { ElMessage.error(apiError(e)) }
}

const switching = ref('')
async function switchTo(row, tag) {
  switching.value = row.tag
  try {
    const { data } = await balancerApi.select(row.tag, tag)
    ElMessage.success(data.xray_running ? `已切换到 ${panel.label(tag)}` : '已切换，但 Xray 未运行')
    await load(); await panel.refreshAll(); emit('changed')
  } catch (e) {
    await panel.refreshStatus().catch(() => {})
    await showApplyError(e, '切换失败')
  } finally { switching.value = '' }
}
function memberText(row) {
  const parts = []
  if (row.filter) parts.push(`关键字「${row.filter}」${row.filter_sub_id != null ? `（${subName(subs.value.find((s) => s.id === row.filter_sub_id))}）` : ''}`)
  if (row.nodes?.length) parts.push(`固定 ${row.nodes.length} 个`)
  return parts.join(' + ')
}
const delayClass = (m) => (!m.probed ? '' : !m.alive ? 'bad' : m.delay < 200 ? 'good' : m.delay < 500 ? 'mid' : 'bad')
</script>

<template>
  <el-card>
    <template #header>
      <div class="hd"><span>节点组（多个节点合成一个出口）</span>
        <el-button type="primary" @click="openCreate">+ 新建节点组</el-button></div>
    </template>
    <p class="muted" style="margin-top:0">把多个节点（或自定义代理）组成一组，在路由里当作一个出口使用。用关键字收纳时，订阅更新出现的新节点会自动加入。</p>
    <el-empty v-if="!list.length" description="还没有节点组。例如用关键字「香港|HK」建一个「香港」组，路由里选它，就会自动使用最快的香港节点。" />
    <el-table v-else :data="list" row-key="tag">
      <el-table-column type="expand">
        <template #default="{ row }">
          <div class="members">
            <div v-if="!status[row.tag]?.members?.length" class="muted">没有可用成员</div>
            <div v-for="m in status[row.tag]?.members || []" :key="m.tag" class="member" :class="{ cur: m.tag === status[row.tag]?.current }">
              <span>{{ m.label }}</span>
              <span :class="'lat-' + delayClass(m)">{{ !m.probed ? '未探测' : m.alive ? `${m.delay}ms` : '不可用' }}</span>
              <el-button v-if="row.strategy === 'select' && m.tag !== status[row.tag]?.current" size="small" text type="primary"
                         :loading="switching === row.tag" @click="switchTo(row, m.tag)">切换到此</el-button>
            </div>
          </div>
        </template>
      </el-table-column>
      <el-table-column prop="name" label="名称" min-width="110" />
      <el-table-column label="类型" width="110"><template #default="{ row }">{{ strategyLabel(row.strategy || 'leastPing') }}</template></el-table-column>
      <el-table-column label="成员" min-width="200"><template #default="{ row }">
        <div>{{ memberText(row) }}</div>
        <div v-if="status[row.tag]" class="muted">
          可用 {{ status[row.tag].members?.length || 0 }} / {{ status[row.tag].candidates }}
          <template v-if="status[row.tag].probed"> · 探测在线 {{ status[row.tag].alive }}</template>
        </div>
      </template></el-table-column>
      <el-table-column label="当前使用" min-width="190"><template #default="{ row }">
        <template v-if="row.strategy === 'select' && status[row.tag]?.members?.length">
          <el-select :model-value="status[row.tag].current" size="small" filterable style="width:170px"
                     :loading="switching === row.tag" @change="(v) => switchTo(row, v)">
            <el-option v-for="m in status[row.tag].members" :key="m.tag" :label="m.label" :value="m.tag" />
          </el-select>
        </template>
        <template v-else-if="status[row.tag]">
          <div>{{ status[row.tag].current ? panel.label(status[row.tag].current) : '—' }}</div>
          <div class="muted">{{ status[row.tag].reason || status[row.tag].current_tip }}</div>
        </template>
      </template></el-table-column>
      <el-table-column label="操作" width="140"><template #default="{ row }">
        <el-button size="small" @click="openEdit(row)">编辑</el-button>
        <el-button size="small" type="danger" @click="remove(row)">删</el-button></template></el-table-column>
    </el-table>
  </el-card>

  <el-dialog v-model="dialog" :title="editing ? '编辑节点组' : '新建节点组'" width="min(760px, 96vw)">
    <el-form label-width="90px">
      <el-form-item label="名称"><el-input v-model="form.name" placeholder="例如：香港" /></el-form-item>
      <el-form-item label="类型">
        <el-radio-group v-model="form.strategy" class="strat">
          <el-radio v-for="[v, l, d] in STRATEGIES" :key="v" :value="v" border><b>{{ l }}</b><div class="muted">{{ d }}</div></el-radio>
        </el-radio-group>
      </el-form-item>
      <el-form-item label="按关键字">
        <div class="row" style="width:100%">
          <el-input v-model="form.filter" placeholder="节点名包含任一关键字即加入，用 | 分隔，如 香港|HK" class="grow" />
          <el-select v-model="form.filter_sub_id" clearable placeholder="全部订阅" style="width:150px">
            <el-option v-for="s in subs" :key="s.id" :label="subName(s)" :value="s.id" />
          </el-select>
        </div>
        <div class="muted" style="width:100%">订阅更新后新出现的匹配节点会自动加入，消失的自动移出。</div>
      </el-form-item>
      <el-form-item label="固定成员">
        <el-checkbox v-model="showStatic">另外手动挑选节点 / 自定义代理</el-checkbox>
        <el-transfer v-if="showStatic" v-model="form.nodes" :data="transferData" :titles="['可选', `已选 ${form.nodes.length}`]"
                     filterable filter-placeholder="搜索" style="margin-top:8px" />
      </el-form-item>
      <el-form-item label="成员预览">
        <div style="width:100%">
          <div v-if="!preview.length" class="muted">{{ form.filter || form.nodes.length ? '没有匹配的节点' : '填写关键字或挑选成员后显示' }}</div>
          <template v-else>
            <div class="muted">共 {{ preview.length }} 个</div>
            <div class="chips"><el-tag v-for="p in preview.slice(0, 40)" :key="p.tag" size="small" type="info">{{ p.label }}</el-tag>
              <span v-if="preview.length > 40" class="muted">…</span></div>
          </template>
        </div>
      </el-form-item>
      <el-form-item v-if="form.strategy === 'select' && preview.length" label="默认使用">
        <el-select v-model="form.selected" filterable clearable placeholder="第一个成员" style="width:260px">
          <el-option v-for="p in preview" :key="p.tag" :label="p.label" :value="p.tag" />
        </el-select>
      </el-form-item>
    </el-form>
    <template #footer><el-button @click="dialog = false">取消</el-button>
      <el-button type="primary" :loading="saving" :disabled="!preview.length" @click="save">保存</el-button></template>
  </el-dialog>
</template>

<style scoped>
.hd { display:flex; justify-content:space-between; align-items:center; }
.row { display:flex; gap:8px; flex-wrap:wrap; }
.grow { flex:1; min-width:200px; }
.strat { display:grid; grid-template-columns:1fr 1fr; gap:8px; width:100%; }
.strat :deep(.el-radio) { height:auto; padding:8px 12px; margin-right:0; align-items:flex-start; }
.strat :deep(.el-radio__label) { white-space:normal; }
.chips { display:flex; flex-wrap:wrap; gap:4px; margin-top:4px; }
.members { padding:4px 48px; display:grid; grid-template-columns:repeat(auto-fill, minmax(260px, 1fr)); gap:4px 16px; }
.member { display:flex; gap:10px; align-items:center; justify-content:space-between; padding:2px 6px; border-radius:4px; }
.member.cur { background:var(--el-color-primary-light-9); font-weight:600; }
.lat-good { color:var(--el-color-success); } .lat-mid { color:var(--el-color-warning); } .lat-bad { color:var(--el-color-danger); }
:deep(.el-transfer-panel) { width:250px; }
@media (max-width:600px) { .strat { grid-template-columns:1fr; } }
</style>
