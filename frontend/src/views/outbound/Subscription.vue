<script setup>
import { ref, reactive, onMounted, computed, h } from 'vue'
import { ElMessage, ElMessageBox, ElNotification } from 'element-plus'
import { subscriptionApi, routingApi } from '../../api/index.js'
import { apiError } from '../../api/http.js'
import { latClass, latText, fmtBytes, fmtAgo, fmtTime, subName, expireInfo } from '../../utils/format.js'
import { applyConfig } from '../../utils/apply.js'
import { DEFAULT_EXCLUDE, previewExcluded } from '../../utils/subfilter.js'
import { usePanel } from '../../stores/panel.js'

const emit = defineEmits(['changed'])
const panel = usePanel()

// ---------- 订阅 ----------
const addForm = reactive({ url: '', name: '', auto_update_hours: 24 })
const adding = ref(false)
const subs = ref([])
const busy = reactive({})        // id → 拉取中
const fetchingAll = ref(false)
const AUTO_OPTS = [[0, '关闭'], [6, '每 6 小时'], [12, '每 12 小时'], [24, '每天'], [72, '每 3 天']]
const autoLabel = (hrs) => (AUTO_OPTS.find(([v]) => v === hrs) || [0, `每 ${hrs} 小时`])[1]

const editDlg = ref(false)
const editForm = reactive({ id: null, name: '', url: '', auto_update_hours: 0, exclude: '', name_prefix: '' })
const editOrig = reactive({ exclude: '', name_prefix: '' })
const savingEdit = ref(false)

// 预览：当前节点里按新关键字会被排除哪些（比较的是去掉旧前缀后的原始名称）
const editPreview = computed(() => {
  if (editForm.id == null) return []
  const old = editOrig.name_prefix
  const names = nodes.value.filter((n) => n.sub_id === editForm.id && !n.missing)
    .map((n) => (old && n.name.startsWith(old) ? n.name.slice(old.length) : n.name))
  return previewExcluded(names, editForm.exclude)
})

// ---------- 节点 ----------
const nodes = ref([])
const filter = ref('')
const subFilter = ref('all')
const stateFilter = ref('all')
const testing = ref(false)
const selected = ref([])

const subMap = computed(() => Object.fromEntries(subs.value.map((s) => [s.id, s])))
const shown = computed(() => {
  const q = filter.value.trim().toLowerCase()
  return nodes.value.filter((n) => {
    if (q && !n.name.toLowerCase().includes(q) && !n.type.includes(q) && !n.host.toLowerCase().includes(q)) return false
    if (subFilter.value !== 'all' && n.sub_id !== subFilter.value) return false
    switch (stateFilter.value) {
      case 'ok': return !n.disabled && !n.missing && !n.invalid && n.latency != null
      case 'timeout': return !n.disabled && !n.missing && n.latency_mode && n.latency == null
      case 'disabled': return n.disabled
      case 'missing': return n.missing || n.invalid
    }
    return true
  })
})
const filtered = computed(() => shown.value.length !== nodes.value.length)

async function load() {
  try { subs.value = (await subscriptionApi.list()).data } catch (_) { subs.value = [] }
  try { nodes.value = (await subscriptionApi.nodes()).data } catch (_) { nodes.value = [] }
}
onMounted(load)
async function changed() { await load(); await panel.refreshAll(); emit('changed') }

function resultText(r) {
  const parts = [`新增 ${r.added}`, `更新 ${r.updated}`]
  if (r.removed) parts.push(`移除 ${r.removed}`)
  if (r.missing) parts.push(`${r.missing} 个已消失但仍被引用，标记为失效`)
  let s = parts.join('，')
  if (r.filtered?.length) s += `；按关键字排除 ${r.filtered.length} 个（${r.filtered.slice(0, 3).join('、')}${r.filtered.length > 3 ? ' 等' : ''}）`
  if (r.skipped) s += `；跳过 ${r.skipped} 个（${r.skipped_detail}）`
  return s
}

async function addSub() {
  if (!addForm.url.trim()) return ElMessage.warning('请输入订阅链接')
  adding.value = true
  try {
    const { data } = await subscriptionApi.create({ ...addForm, url: addForm.url.trim() })
    ElNotification({ type: 'success', title: `已添加「${subName(data.subscription)}」`, message: resultText(data.result), duration: 6000 })
    Object.assign(addForm, { url: '', name: '' })
    await changed()
  } catch (e) { ElMessage.error(apiError(e)) }
  finally { adding.value = false }
}

async function fetchSub(s) {
  busy[s.id] = true
  try {
    const { data } = await subscriptionApi.fetch(s.id)
    ElNotification({ type: 'success', title: `「${subName(s)}」已更新`, message: resultText(data), duration: 5000 })
  } catch (e) { ElMessage.error(apiError(e)) }
  finally { busy[s.id] = false; await changed() }
}

async function fetchAll() {
  fetchingAll.value = true
  try {
    const { data } = await subscriptionApi.fetchAll()
    const failed = data.results.filter((r) => !r.ok)
    ElNotification({
      type: failed.length ? 'warning' : 'success',
      title: failed.length ? `${failed.length} 个订阅更新失败（已保留原节点）` : '全部订阅已更新',
      message: h('div', data.results.map((r) => h('div', { style: 'margin-bottom:4px' },
        `${r.ok ? '✅' : '❌'} ${r.name}：${r.ok ? resultText(r) : r.error}`))),
      duration: failed.length ? 0 : 6000,
    })
  } catch (e) { ElMessage.error(apiError(e)) }
  finally { fetchingAll.value = false; await changed() }
}

function openEdit(s) {
  Object.assign(editForm, { id: s.id, name: s.name || '', url: s.url, auto_update_hours: s.auto_update_hours || 0,
    exclude: s.exclude || '', name_prefix: s.name_prefix || '' })
  Object.assign(editOrig, { exclude: s.exclude || '', name_prefix: s.name_prefix || '', url: s.url })
  editDlg.value = true
}
async function saveEdit() {
  savingEdit.value = true
  try {
    const { id, ...body } = editForm
    await subscriptionApi.update(id, body)
    editDlg.value = false
    // 过滤条件/前缀/链接变了：立即重新拉取，让修改马上生效
    const needRefetch = body.exclude.trim() !== editOrig.exclude || body.name_prefix.trim() !== editOrig.name_prefix
      || body.url.trim() !== editOrig.url
    if (needRefetch) await fetchSub(subs.value.find((x) => x.id === id) || { id, name: body.name })
    else { ElMessage.success('已保存'); await load() }
  } catch (e) { ElMessage.error(apiError(e)) }
  finally { savingEdit.value = false }
}

async function removeSub(s) {
  try {
    await ElMessageBox.confirm(
      `删除订阅「${subName(s)}」？它的 ${s.node_count} 个节点将一并删除；仍被规则/节点组引用的节点会保留为“失效”，方便你改用其他出口。`,
      '确认删除', { type: 'warning', confirmButtonText: '删除' })
  } catch (_) { return }
  try { await subscriptionApi.remove(s.id); ElMessage.success('已删除'); await changed() }
  catch (e) { ElMessage.error(apiError(e)) }
}

function usagePct(u) {
  if (!u?.total) return 0
  return Math.min(100, Math.round(((u.upload + u.download) / u.total) * 100))
}

// ---------- 节点操作 ----------
async function testSpeed(mode) {
  testing.value = true
  try {
    const tags = filtered.value ? shown.value.map((n) => n.tag) : []
    const { data } = await subscriptionApi.test({ mode, tags })
    nodes.value = data.nodes
    if (data.fallback) ElMessage.warning(data.fallback)
    else ElMessage.success(`测速完成（${data.mode === 'real' ? '真实延迟' : 'TCP'}${tags.length ? `，${tags.length} 个节点` : ''}）`)
  } catch (e) { ElMessage.error(apiError(e)) }
  finally { testing.value = false }
}

async function toggleNode(n, enabled) {
  try { await subscriptionApi.setNode(n.tag, { disabled: !enabled }); n.disabled = !enabled; await panel.refreshAll(); emit('changed') }
  catch (e) { ElMessage.error(apiError(e)) }
}
async function batch(disabled) {
  if (!selected.value.length) return
  try {
    await subscriptionApi.batchNodes(selected.value.map((n) => n.tag), disabled)
    ElMessage.success(`已${disabled ? '禁用' : '启用'} ${selected.value.length} 个节点`)
    await changed()
  } catch (e) { ElMessage.error(apiError(e)) }
}

async function pickFastest() {
  const alive = shown.value.filter((n) => n.latency != null && !n.disabled && !n.missing)
    .sort((a, b) => a.latency - b.latency)
  if (!alive.length) return ElMessage.warning('先测速，或当前没有可用节点')
  const best = alive[0]
  try {
    const rt = (await routingApi.get()).data
    await routingApi.put({ default_outbound: best.tag, domain_strategy: rt.domain_strategy, rules: rt.rules })
    await panel.refreshAll(); emit('changed')
  } catch (e) { return ElMessage.error(apiError(e)) }
  try {
    await ElMessageBox.confirm(`默认出口已设为「${best.name}」（${best.latency}ms），是否立即应用生效？`, '已修改默认出口',
      { confirmButtonText: '立即应用', cancelButtonText: '稍后', type: 'success' })
    await applyConfig(panel)
  } catch (_) { /* 稍后 */ }
}

function protoText(n) {
  const extra = [n.network && n.network !== 'tcp' ? n.network : '', n.security].filter(Boolean).join('+')
  return extra ? `${n.type} · ${extra}` : n.type
}
const latSort = (a, b) => (a.latency ?? 1e9) - (b.latency ?? 1e9)
</script>

<template>
  <el-card>
    <template #header>
      <div class="hd"><span>订阅列表（{{ subs.length }}）</span>
        <el-button size="small" :loading="fetchingAll" :disabled="!subs.length" @click="fetchAll">全部更新</el-button></div>
    </template>
    <div class="row wrap" style="margin-bottom:12px">
      <el-input v-model="addForm.url" placeholder="粘贴订阅链接（http/https，通用/V2Ray 格式）" class="grow" @keyup.enter="addSub" />
      <el-input v-model="addForm.name" placeholder="名称（可选）" style="width:150px" />
      <el-select v-model="addForm.auto_update_hours" style="width:170px">
        <el-option v-for="[v, l] in AUTO_OPTS" :key="v" :label="`自动更新：${l}`" :value="v" />
      </el-select>
      <el-button type="primary" :loading="adding" @click="addSub">添加</el-button>
    </div>
    <el-table v-if="subs.length" :data="subs" size="small">
      <el-table-column label="名称" min-width="140">
        <template #default="{ row }">
          <div>{{ subName(row) }}</div>
          <div class="muted ellipsis" :title="row.url">{{ row.url }}</div>
        </template>
      </el-table-column>
      <el-table-column label="节点" width="110">
        <template #default="{ row }">
          {{ row.node_count }}<span v-if="row.missing_count" class="muted"> +{{ row.missing_count }} 失效</span>
          <el-tooltip v-if="row.filtered" :content="`排除关键字：${row.exclude}`">
            <div class="muted">已排除 {{ row.filtered }} 个</div>
          </el-tooltip>
        </template>
      </el-table-column>
      <el-table-column label="流量 / 到期" min-width="170">
        <template #default="{ row }">
          <template v-if="row.usage">
            <el-progress v-if="row.usage.total" :percentage="usagePct(row.usage)" :stroke-width="6"
                         :status="usagePct(row.usage) >= 90 ? 'exception' : ''" />
            <div class="muted">{{ fmtBytes(row.usage.upload + row.usage.download) }} / {{ row.usage.total ? fmtBytes(row.usage.total) : '不限' }}</div>
            <div v-if="expireInfo(row.usage.expire)" class="muted" :class="'lv-' + expireInfo(row.usage.expire).level">
              到期 {{ expireInfo(row.usage.expire).text }}</div>
          </template>
          <span v-else class="muted">订阅未提供</span>
        </template>
      </el-table-column>
      <el-table-column label="更新" min-width="150">
        <template #default="{ row }">
          <el-tooltip :content="`上次成功：${fmtTime(row.fetched_at)}`"><span>{{ fmtAgo(row.fetched_at) }}</span></el-tooltip>
          <div class="muted">{{ autoLabel(row.auto_update_hours) }}</div>
          <el-tooltip v-if="row.last_error" :content="row.last_error">
            <el-tag type="danger" size="small">上次更新失败</el-tag>
          </el-tooltip>
        </template>
      </el-table-column>
      <el-table-column label="操作" width="190">
        <template #default="{ row }">
          <el-button size="small" :loading="busy[row.id]" @click="fetchSub(row)">更新</el-button>
          <el-button size="small" @click="openEdit(row)">编辑</el-button>
          <el-button size="small" type="danger" @click="removeSub(row)">删</el-button>
        </template>
      </el-table-column>
    </el-table>
    <el-empty v-else description="还没有订阅。粘贴机场提供的订阅链接即可导入节点。" :image-size="60" />
  </el-card>

  <el-card style="margin-top:16px;">
    <template #header>
      <div class="hd">
        <span>节点（{{ filtered ? `${shown.length} / ` : '' }}{{ nodes.length }}）</span>
        <div class="row wrap">
          <el-input v-model="filter" placeholder="搜索 名称/协议/地址" clearable style="width:170px" />
          <el-select v-model="subFilter" style="width:130px">
            <el-option label="全部订阅" value="all" />
            <el-option v-for="s in subs" :key="s.id" :label="subName(s)" :value="s.id" />
          </el-select>
          <el-select v-model="stateFilter" style="width:110px">
            <el-option label="全部状态" value="all" /><el-option label="可用" value="ok" />
            <el-option label="超时" value="timeout" /><el-option label="已禁用" value="disabled" />
            <el-option label="失效" value="missing" />
          </el-select>
          <el-dropdown split-button :loading="testing" :disabled="testing || !nodes.length" @click="testSpeed('real')" @command="testSpeed">
            {{ testing ? '测速中…' : (filtered ? '测速（筛选结果）' : '测速') }}
            <template #dropdown><el-dropdown-menu>
              <el-dropdown-item command="real">真实延迟（经代理访问，推荐）</el-dropdown-item>
              <el-dropdown-item command="tcp">TCP 延迟（仅测服务器连通）</el-dropdown-item>
            </el-dropdown-menu></template>
          </el-dropdown>
          <el-button @click="pickFastest">设最快为默认出口</el-button>
        </div>
      </div>
    </template>
    <div v-if="selected.length" class="row" style="margin-bottom:8px">
      <span class="muted">已选 {{ selected.length }} 个</span>
      <el-button size="small" @click="batch(false)">启用</el-button>
      <el-button size="small" @click="batch(true)">禁用</el-button>
    </div>
    <el-table :data="shown" max-height="560" row-key="tag" @selection-change="(v) => (selected = v)"
              :default-sort="{ prop: 'latency', order: null }">
      <el-table-column type="selection" width="40" />
      <el-table-column label="启用" width="70">
        <template #default="{ row }">
          <el-switch :model-value="!row.disabled" size="small" :disabled="row.missing" @change="(v) => toggleNode(row, v)" />
        </template>
      </el-table-column>
      <el-table-column label="名称" min-width="200">
        <template #default="{ row }">
          <span :class="{ dim: row.disabled || row.missing }">{{ row.name }}</span>
          <el-tag v-if="row.missing" type="danger" size="small" class="ml">订阅中已消失</el-tag>
          <el-tag v-else-if="row.invalid" type="danger" size="small" class="ml">缺少凭据</el-tag>
          <el-tooltip v-if="row.refs" :content="`被 ${row.refs} 处规则/节点组/默认出口使用`">
            <el-tag size="small" class="ml">使用中</el-tag>
          </el-tooltip>
        </template>
      </el-table-column>
      <el-table-column label="延迟" prop="latency" width="100" sortable :sort-method="latSort">
        <template #default="{ row }">
          <span v-if="!row.latency_mode" class="muted">未测</span>
          <el-tooltip v-else :content="row.latency_mode === 'real' ? '真实延迟' : 'TCP 延迟'">
            <span :class="'lat-' + latClass(row.latency)">{{ latText(row.latency) }}</span>
          </el-tooltip>
        </template>
      </el-table-column>
      <el-table-column label="协议" width="150"><template #default="{ row }">{{ protoText(row) }}</template></el-table-column>
      <el-table-column label="订阅" width="120">
        <template #default="{ row }"><span class="muted">{{ subMap[row.sub_id] ? subName(subMap[row.sub_id]) : '—' }}</span></template>
      </el-table-column>
    </el-table>
  </el-card>

  <el-dialog v-model="editDlg" title="编辑订阅" width="min(600px, 94vw)">
    <el-form label-width="90px">
      <el-form-item label="名称"><el-input v-model="editForm.name" placeholder="留空则使用订阅自带的备注" /></el-form-item>
      <el-form-item label="订阅链接"><el-input v-model="editForm.url" type="textarea" :rows="2" /></el-form-item>
      <el-form-item label="排除关键字">
        <el-input v-model="editForm.exclude" placeholder="留空则不过滤" clearable>
          <template #append><el-button @click="editForm.exclude = DEFAULT_EXCLUDE">恢复默认</el-button></template>
        </el-input>
        <div class="muted" style="width:100%">节点名包含任一关键字就不导入，用 | 分隔，不区分大小写。默认过滤「剩余流量」「到期时间」「官网」这类信息节点。</div>
        <div v-if="editPreview.length" class="preview" style="width:100%">
          当前节点中将被排除 {{ editPreview.length }} 个：{{ editPreview.slice(0, 5).join('、') }}{{ editPreview.length > 5 ? ' 等' : '' }}
        </div>
      </el-form-item>
      <el-form-item label="名称前缀">
        <el-input v-model="editForm.name_prefix" placeholder="可选，如 A-" maxlength="20" style="width:160px" />
        <span class="muted" style="margin-left:8px">有多个机场时便于区分，如「A-香港 01」</span>
      </el-form-item>
      <el-form-item label="自动更新">
        <el-select v-model="editForm.auto_update_hours">
          <el-option v-for="[v, l] in AUTO_OPTS" :key="v" :label="l" :value="v" />
        </el-select>
        <div class="muted" style="width:100%">若更新前配置已生效，更新后会自动应用；否则只更新节点，等你确认后再应用。</div>
      </el-form-item>
    </el-form>
    <template #footer><el-button @click="editDlg = false">取消</el-button>
      <el-button type="primary" :loading="savingEdit" @click="saveEdit">保存</el-button></template>
  </el-dialog>
</template>

<style scoped>
.row { display:flex; gap:8px; align-items:center; }
.wrap { flex-wrap:wrap; }
.grow { flex:1; min-width:220px; }
.hd { display:flex; justify-content:space-between; align-items:center; gap:8px; flex-wrap:wrap; }
.ellipsis { max-width:320px; overflow:hidden; text-overflow:ellipsis; white-space:nowrap; }
.ml { margin-left:6px; }
.dim { color:var(--el-text-color-placeholder); text-decoration:line-through; }
.lat-good { color:var(--el-color-success); } .lat-mid { color:var(--el-color-warning); }
.lat-bad { color:var(--el-color-danger); }
.preview { color:var(--el-color-warning); font-size:12px; margin-top:4px; }
.lv-warn { color:var(--el-color-warning); } .lv-danger { color:var(--el-color-danger); }
</style>
