<script setup>
import { ref, reactive, computed, onMounted, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { ruleSetApi } from '../api/index.js'
import { apiError } from '../api/http.js'
import { fmtAgo, fmtTime } from '../utils/format.js'

const emit = defineEmits(['changed'])
const tab = ref('custom')
const sets = ref([])

async function loadSets() {
  try { sets.value = (await ruleSetApi.list()).data } catch (e) { ElMessage.error(apiError(e)) }
}
onMounted(loadSets)

const preview = (g) => [...g.domains, ...g.ips].slice(0, 4).join('、') + (g.domain_count + g.ip_count > 4 ? ' …' : '')
const localSets = computed(() => sets.value.filter((g) => !g.source)) // 订阅分组的内容会被更新覆盖，不能手动加条目
// 引用它的规则：优先显示规则名称，没有名称时显示序号
const refName = (r) => r.name || `规则 #${r.index}`
const host = (u) => { try { return new URL(u).host } catch (_) { return u } }

// 订阅远程列表
const FORMATS = [['auto', '自动识别'], ['gfwlist', 'GFWList / AdBlock'], ['clash', 'Clash 规则集'], ['domains', '域名列表（每行一个）']]
const FORMAT_NAME = Object.fromEntries(FORMATS)
const UPDATE_OPTS = [[0, '不自动更新'], [6, '每 6 小时'], [12, '每 12 小时'], [24, '每天'], [72, '每 3 天'], [168, '每周']]
const PRESETS = [
  { name: 'GFWList', url: 'https://gcore.jsdelivr.net/gh/gfwlist/gfwlist/gfwlist.txt', format: 'gfwlist', remark: '被墙网站列表，建议走代理' },
]
function usePreset(p) {
  Object.assign(form, { url: p.url, format: p.format })
  if (!form.name.trim()) form.name = p.name
  if (!form.remark.trim()) form.remark = p.remark
}
const refreshing = reactive({})
async function refresh(g) {
  refreshing[g.tag] = true
  try {
    const { data } = await ruleSetApi.refresh(g.tag)
    await loadSets(); emit('changed')
    ElMessage.success(`「${g.name}」已更新：域名 ${data.domain_count} 条，IP ${data.ip_count} 条`)
  } catch (e) { await loadSets(); ElMessage.error(apiError(e)) }
  finally { refreshing[g.tag] = false }
}

// 查看完整内容（订阅分组可能有上万条，按需搜索加载）
const viewDlg = ref(false)
const viewSet = ref(null)
const viewQ = ref('')
const viewRes = ref({ entries: [], total: 0 })
async function loadView() {
  try { viewRes.value = (await ruleSetApi.setEntries(viewSet.value.tag, viewQ.value.trim())).data } catch (e) { ElMessage.error(apiError(e)) }
}
function openView(g) { viewSet.value = g; viewQ.value = ''; viewDlg.value = true; loadView() }
let vTimer = null
watch(viewQ, () => { clearTimeout(vTimer); vTimer = setTimeout(loadView, 300) })

// ---- 编辑自定义分组 ----
const dialog = ref(false)
const editing = ref(null)
const saving = ref(false)
const EMPTY = { mode: 'manual', name: '', remark: '', domains: '', ips: '', url: '', format: 'auto', update_hours: 24 }
const form = reactive({ ...EMPTY })
const lines = (s) => s.split(/[\n,，]+/).map((x) => x.trim()).filter(Boolean)
const count = computed(() => ({ d: lines(form.domains).length, i: lines(form.ips).length }))

function openCreate(prefill = {}) {
  editing.value = null
  Object.assign(form, { ...EMPTY }, prefill)
  dialog.value = true
}
function openEdit(g) {
  editing.value = g.tag
  Object.assign(form, { ...EMPTY, name: g.name, remark: g.remark || '' })
  if (g.source) Object.assign(form, { mode: 'remote', url: g.source.url, format: g.source.format || 'auto', update_hours: g.source.update_hours })
  else Object.assign(form, { domains: g.domains.join('\n'), ips: g.ips.join('\n') })
  dialog.value = true
}
async function save() {
  if (!form.name.trim()) return ElMessage.warning('请填写分组名称')
  const body = { name: form.name.trim(), remark: form.remark }
  if (form.mode === 'remote') {
    if (!form.url.trim()) return ElMessage.warning('请填写列表地址')
    body.source = { url: form.url.trim(), format: form.format, update_hours: form.update_hours }
  } else Object.assign(body, { domains: lines(form.domains), ips: lines(form.ips) })
  saving.value = true
  try {
    if (editing.value) await ruleSetApi.update(editing.value, body)
    else await ruleSetApi.create(body)
    dialog.value = false; await loadSets(); emit('changed')
    ElMessage.success(editing.value ? '已保存，应用后引用它的规则一起生效' : '已创建，可以在「分流规则」中选择类型「自定义分组」引用它')
    if (form.mode === 'remote') ElMessage.info('订阅分组的内容来自远程列表，会按设置的间隔自动更新')
  } catch (e) { ElMessage.error(apiError(e)) }
  finally { saving.value = false }
}
async function remove(g) {
  const msg = g.ref_rules.length ? `分组「${g.name}」被规则 ${g.ref_rules.map(refName).join('、')} 引用，删除后这些规则也会一起删除。` : `删除分组「${g.name}」？`
  try { await ElMessageBox.confirm(msg, '确认删除', { type: 'warning' }) } catch (_) { return }
  try { await ruleSetApi.remove(g.tag); await loadSets(); emit('changed') } catch (e) { ElMessage.error(apiError(e)) }
}

// ---- 内置分组（geosite / geoip）----
const kind = ref('site')
const catalog = ref({ available: true, codes: [] })
const codeQ = ref('')
const code = ref('')
const entryQ = ref('')
const entries = ref({ entries: [], total: 0 })
const loadingEntries = ref(false)
const POPULAR = {
  site: ['cn', 'geolocation-!cn', 'google', 'youtube', 'netflix', 'telegram', 'openai', 'github', 'apple', 'microsoft', 'category-ads-all', 'private'],
  ip: ['cn', 'private', 'us', 'jp', 'hk', 'telegram', 'google', 'netflix'],
}
const codeSet = computed(() => new Set(catalog.value.codes.map((c) => c.code)))
const popular = computed(() => POPULAR[kind.value].filter((c) => codeSet.value.has(c)))
const filteredCodes = computed(() => {
  const q = codeQ.value.trim().toLowerCase()
  const all = catalog.value.codes
  return (q ? all.filter((c) => c.code.includes(q)) : all).slice(0, 300)
})
const ref_ = computed(() => (kind.value === 'site' ? 'geosite:' : 'geoip:') + code.value)

async function loadCatalog() {
  code.value = ''; entries.value = { entries: [], total: 0 }
  try { catalog.value = (await ruleSetApi.catalog(kind.value)).data } catch (e) { catalog.value = { available: false, error: apiError(e), codes: [] } }
  if (popular.value.length) pick(popular.value[0])
}
async function loadEntries() {
  if (!code.value) return
  loadingEntries.value = true
  try { entries.value = (await ruleSetApi.entries(kind.value, code.value, entryQ.value.trim())).data }
  catch (e) { entries.value = { entries: [], total: 0 }; ElMessage.error(apiError(e)) }
  finally { loadingEntries.value = false }
}
function pick(c) { code.value = c; entryQ.value = ''; loadEntries() }
watch(kind, loadCatalog)
watch(tab, (t) => { if (t === 'builtin' && !catalog.value.codes.length) loadCatalog() })
let qTimer = null
watch(entryQ, () => { clearTimeout(qTimer); qTimer = setTimeout(loadEntries, 300) })

async function copy(text) {
  try { await navigator.clipboard.writeText(text); ElMessage.success('已复制 ' + text) } catch (_) { ElMessage.warning('复制失败') }
}
async function addTo(target) {
  const entry = ref_.value
  if (target === '__new__') {
    tab.value = 'custom'
    return openCreate(kind.value === 'site' ? { name: code.value, domains: entry } : { name: code.value, ips: entry })
  }
  const g = localSets.value.find((x) => x.tag === target)
  const key = kind.value === 'site' ? 'domains' : 'ips'
  if (g[key].includes(entry)) return ElMessage.info(`「${g.name}」已包含 ${entry}`)
  try {
    await ruleSetApi.update(g.tag, { name: g.name, remark: g.remark, domains: g.domains, ips: g.ips, [key]: [...g[key], entry] })
    await loadSets(); emit('changed'); ElMessage.success(`已把 ${entry} 加入「${g.name}」`)
  } catch (e) { ElMessage.error(apiError(e)) }
}
</script>

<template>
  <el-card>
    <el-tabs v-model="tab">
      <el-tab-pane label="自定义分组" name="custom">
        <div class="bar">
          <span class="muted">把常用的域名和 IP 收进一个分组，在「分流规则」里选择类型「自定义分组」引用；改分组后，所有引用它的规则一起生效。</span>
          <el-button type="primary" @click="openCreate()">+ 新建分组</el-button>
        </div>
        <el-table :data="sets" empty-text="还没有分组">
          <el-table-column label="名称" min-width="230"><template #default="{ row }">
            <b>{{ row.name }}</b><el-tag v-if="row.source" size="small" type="warning" effect="plain" class="src-tag">订阅</el-tag>
            <div v-if="row.remark" class="muted">{{ row.remark }}</div>
            <div v-if="row.source" class="muted" :title="row.source.url">
              {{ host(row.source.url) }} · {{ FORMAT_NAME[row.source.detected || row.source.format] }}
              · {{ row.source.updated_at ? '更新于 ' + fmtAgo(row.source.updated_at) : '未更新' }}
              <template v-if="row.source.update_hours">（自动）</template></div>
            <div v-if="row.source?.last_error" class="err" :title="row.source.last_error">
              上次更新失败（{{ fmtTime(row.source.tried_at) }}），仍在使用之前的内容：{{ row.source.last_error }}</div>
          </template></el-table-column>
          <el-table-column label="内容" min-width="260"><template #default="{ row }">
            <div><el-tag size="small" effect="plain">域名 {{ row.domain_count }}</el-tag>
              <el-tag size="small" effect="plain" type="success" style="margin-left:6px">IP {{ row.ip_count }}</el-tag>
              <span v-if="row.source?.skipped" class="muted" style="margin-left:6px"
                    title="白名单（@@）、正则、带通配符等无法转换成域名规则的条目">跳过 {{ row.source.skipped }} 条</span></div>
            <div class="muted mono">{{ preview(row) }}</div>
          </template></el-table-column>
          <el-table-column label="被引用" min-width="140"><template #default="{ row }">
            <template v-if="row.ref_rules.length">
              <router-link v-for="r in row.ref_rules" :key="r.index" to="/routing" class="ref"
                           :title="`分流规则第 ${r.index} 条`">{{ refName(r) }}</router-link>
            </template>
            <span v-else class="muted">未被引用</span>
          </template></el-table-column>
          <el-table-column label="操作" width="270"><template #default="{ row }">
            <template v-if="row.source">
              <el-button size="small" :loading="refreshing[row.tag]" @click="refresh(row)">更新</el-button>
              <el-button size="small" @click="openView(row)">查看</el-button>
            </template>
            <el-button size="small" @click="openEdit(row)">编辑</el-button>
            <el-button size="small" type="danger" @click="remove(row)">删</el-button>
          </template></el-table-column>
        </el-table>
      </el-tab-pane>

      <el-tab-pane label="内置分组（规则数据库）" name="builtin">
        <div class="bar">
          <el-radio-group v-model="kind" size="small">
            <el-radio-button value="site">geosite 域名</el-radio-button>
            <el-radio-button value="ip">geoip IP</el-radio-button>
          </el-radio-group>
          <span class="muted">规则数据库自带的分类，只读。可以在规则中直接用（类型「{{ kind === 'site' ? '预置域名集合' : '地区IP' }}」），也可以加入自定义分组。</span>
        </div>
        <el-empty v-if="!catalog.available" :description="catalog.error || '没有找到规则数据库'">
          <el-button @click="$router.push('/settings')">去「设置 → 规则数据库」下载</el-button>
        </el-empty>
        <div v-else class="geo">
          <div class="codes">
            <el-input v-model="codeQ" size="small" clearable :placeholder="`搜索 ${catalog.codes.length} 个分类`" />
            <div v-if="popular.length && !codeQ" class="pop">
              <el-tag v-for="c in popular" :key="c" size="small" class="clickable" :effect="code === c ? 'dark' : 'plain'" @click="pick(c)">{{ c }}</el-tag>
            </div>
            <div class="code-list">
              <div v-for="c in filteredCodes" :key="c.code" class="code" :class="{ on: code === c.code }" @click="pick(c.code)">
                <span>{{ c.code }}</span><span class="muted">{{ c.count }}</span>
              </div>
              <div v-if="filteredCodes.length === 300" class="muted pad">只显示前 300 个，请输入关键字</div>
            </div>
          </div>
          <div class="entries">
            <template v-if="code">
              <div class="ent-hd">
                <b class="mono">{{ ref_ }}</b>
                <el-button size="small" @click="copy(ref_)">复制</el-button>
                <el-select size="small" placeholder="加入自定义分组…" style="width:170px" :model-value="''" @change="addTo">
                  <el-option label="+ 新建分组" value="__new__" />
                  <el-option v-for="g in localSets" :key="g.tag" :label="g.name" :value="g.tag" />
                </el-select>
                <el-input v-model="entryQ" size="small" clearable placeholder="在此分类中搜索" style="flex:1;min-width:140px" />
              </div>
              <div class="muted">共 {{ entries.total }} 条{{ entries.total > entries.entries.length ? `，显示前 ${entries.entries.length} 条` : '' }}
                <template v-if="kind === 'site'"> · domain: 后缀，full: 完整域名，keyword: 关键字，regexp: 正则；@xx 为属性标记</template></div>
              <div v-loading="loadingEntries" class="ent-list mono">
                <div v-for="e in entries.entries" :key="e">{{ e }}</div>
              </div>
            </template>
            <el-empty v-else description="选择左侧的分类查看内容" :image-size="60" />
          </div>
        </div>
      </el-tab-pane>
    </el-tabs>
  </el-card>

  <el-dialog v-model="dialog" :title="editing ? '编辑分组' : '新建分组'" width="min(760px, 96vw)">
    <el-form label-width="70px">
      <el-form-item label="名称"><el-input v-model="form.name" maxlength="40" placeholder="如 公司内网、AI 服务" /></el-form-item>
      <el-form-item label="备注"><el-input v-model="form.remark" maxlength="80" placeholder="可选" /></el-form-item>
      <el-form-item label="来源">
        <el-radio-group v-model="form.mode" :disabled="!!editing">
          <el-radio-button value="manual">手动填写</el-radio-button>
          <el-radio-button value="remote">订阅远程列表</el-radio-button>
        </el-radio-group>
      </el-form-item>
      <template v-if="form.mode === 'remote'">
        <el-form-item label="地址">
          <el-input v-model="form.url" placeholder="https://…（如 GFWList、Clash 规则集）" />
          <div class="presets">常用：<el-button v-for="p in PRESETS" :key="p.name" link type="primary" size="small" @click="usePreset(p)">{{ p.name }}</el-button></div>
        </el-form-item>
        <el-form-item label="格式">
          <el-select v-model="form.format" style="width:220px">
            <el-option v-for="[v, l] in FORMATS" :key="v" :value="v" :label="l" />
          </el-select>
        </el-form-item>
        <el-form-item label="更新">
          <el-select v-model="form.update_hours" style="width:220px">
            <el-option v-for="[v, l] in UPDATE_OPTS" :key="v" :value="v" :label="l" />
          </el-select>
        </el-form-item>
        <div class="muted help remote-help">
          保存时会先下载并解析，成功才保存；之后按设置的间隔自动更新，更新失败会保留之前的内容。
          支持 GFWList（含 base64 编码）/ AdBlock 规则、Clash 规则集（payload 或 DOMAIN-SUFFIX 写法）、每行一个域名或 IP 的列表。
          直连下载失败时会经本机 HTTP 入站代理重试。白名单（@@）、正则等无法转换的条目会被跳过。
        </div>
      </template>
      <div v-else class="two">
        <div>
          <div class="lab">域名 <span class="muted">{{ count.d }} 条</span></div>
          <el-input v-model="form.domains" type="textarea" :rows="12" class="mono"
                    placeholder="每行一个&#10;example.com&#10;full:www.example.com&#10;keyword:google&#10;regexp:^ads\d+\.&#10;geosite:netflix" />
          <div class="muted help">直接写域名 = 匹配它及所有子域名；<code>full:</code> 只匹配这个域名；<code>keyword:</code> 包含即匹配；
            <code>regexp:</code> 正则；<code>geosite:</code> 引用内置分组。</div>
        </div>
        <div>
          <div class="lab">IP <span class="muted">{{ count.i }} 条</span></div>
          <el-input v-model="form.ips" type="textarea" :rows="12" class="mono"
                    placeholder="每行一个&#10;8.8.8.8&#10;10.0.0.0/8&#10;2001:db8::/32&#10;geoip:telegram" />
          <div class="muted help">IP、CIDR 网段，或 <code>geoip:</code> 引用内置分组。域名策略为默认的 IPIfNonMatch 时，
            域名请求在没有命中域名规则后也会解析成 IP 来匹配这里。</div>
        </div>
      </div>
    </el-form>
    <template #footer><el-button @click="dialog = false">取消</el-button>
      <el-button type="primary" :loading="saving" @click="save">{{ form.mode === 'remote' && !editing ? '下载并保存' : '保存' }}</el-button></template>
  </el-dialog>

  <el-dialog v-model="viewDlg" :title="`分组内容：${viewSet?.name || ''}`" width="min(640px, 96vw)">
    <el-input v-model="viewQ" clearable placeholder="搜索域名或 IP" />
    <div class="muted" style="margin:8px 0">共 {{ viewRes.total }} 条{{ viewRes.total > viewRes.entries.length ? `，显示前 ${viewRes.entries.length} 条` : '' }}
      · 不带前缀的是域名后缀（含所有子域名），full: 为完整域名</div>
    <div class="ent-list mono">
      <div v-for="e in viewRes.entries" :key="e">{{ e }}</div>
    </div>
  </el-dialog>
</template>

<style scoped>
.bar { display:flex; justify-content:space-between; align-items:center; gap:12px; margin-bottom:12px; flex-wrap:wrap; }
.muted { color:var(--el-text-color-secondary); font-size:12px; }
.mono, .mono :deep(textarea) { font-family:ui-monospace, SFMono-Regular, Menlo, monospace; font-size:12px; }
.clickable { cursor:pointer; }
a { color:var(--el-color-primary); text-decoration:none; }
a:hover { text-decoration:underline; }
.geo { display:grid; grid-template-columns:260px 1fr; gap:16px; min-height:420px; }
.codes { display:flex; flex-direction:column; gap:8px; min-width:0; }
.pop { display:flex; flex-wrap:wrap; gap:4px; }
.code-list { border:1px solid var(--el-border-color-lighter); border-radius:4px; overflow:auto; max-height:460px; }
.code { display:flex; justify-content:space-between; padding:5px 10px; cursor:pointer; font-size:13px; }
.code:hover { background:var(--el-fill-color-light); }
.code.on { background:var(--el-color-primary-light-9); color:var(--el-color-primary); }
.pad { padding:6px 10px; }
.entries { min-width:0; display:flex; flex-direction:column; gap:8px; }
.ent-hd { display:flex; gap:8px; align-items:center; flex-wrap:wrap; }
.ent-list { border:1px solid var(--el-border-color-lighter); border-radius:4px; padding:8px 10px; overflow:auto; max-height:460px; line-height:1.7; }
.src-tag { margin-left:6px; }
.ref { display:block; line-height:22px; overflow:hidden; text-overflow:ellipsis; white-space:nowrap; }
.err { color:var(--el-color-danger); font-size:12px; overflow:hidden; text-overflow:ellipsis; white-space:nowrap; max-width:420px; }
.presets { font-size:12px; color:var(--el-text-color-secondary); margin-top:4px; }
.remote-help { padding-left:70px; margin-top:-4px; }
.two { display:grid; grid-template-columns:1fr 1fr; gap:16px; }
.lab { font-size:13px; margin-bottom:6px; }
.help { margin-top:6px; line-height:1.6; }
code { background:var(--el-fill-color); padding:0 3px; border-radius:3px; }
@media (max-width:760px) { .geo, .two { grid-template-columns:1fr; } }
</style>
