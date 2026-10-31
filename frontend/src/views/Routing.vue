<script setup>
import { ref, onMounted, onBeforeUnmount, computed, reactive } from 'vue'
import { onBeforeRouteLeave } from 'vue-router'
import draggable from 'vuedraggable'
import { ElMessage, ElMessageBox } from 'element-plus'
import { routingApi, ruleSetApi } from '../api/index.js'
import { apiError } from '../api/http.js'
import { usePanel } from '../stores/panel.js'
import { applyConfig } from '../utils/apply.js'
import { RULE_TYPES, RULE_PLACEHOLDER, DOMAIN_STRATEGIES, cleanRules, applyTemplate, ruleError } from './rules-helpers.js'

const emit = defineEmits(['changed'])
const panel = usePanel()
const defaultOut = ref('')
const domainStrategy = ref('IPIfNonMatch')
const rules = ref([])
const templates = ref([])
const sets = ref([])
const setMap = computed(() => Object.fromEntries(sets.value.map((x) => [x.tag, x])))
const saving = ref(false)
const snapshot = ref('')

let _kSeq = 0
const withKey = (r) => ({ ...r, _k: r._k ?? `k${_kSeq++}` })
const current = () => JSON.stringify({ d: defaultOut.value, s: domainStrategy.value, r: cleanRules(rules.value) })
const unsaved = computed(() => snapshot.value !== '' && current() !== snapshot.value)

const GROUPS = [['balancer', '节点组'], ['proxy', '自定义代理'], ['node', '订阅节点'], ['builtin', '内置']]
const grouped = computed(() => GROUPS
  .map(([kind, label]) => ({ label, options: panel.outbounds.filter((o) => o.kind === kind) }))
  .filter((g) => g.options.length))
function outState(tag) {
  const o = panel.outboundMap[tag]
  if (!o) return { bad: true, text: '出口已删除' }
  if (!o.available) return { bad: true, text: o.reason }
  return { bad: false }
}
const errors = computed(() => rules.value.map((r) => ruleError(r)))
const hasErrors = computed(() => errors.value.some(Boolean))

async function load() {
  await panel.refreshOutbounds()
  const { data } = await routingApi.get()
  defaultOut.value = data.default_outbound
  domainStrategy.value = data.domain_strategy || 'IPIfNonMatch'
  rules.value = data.rules.map((r) => withKey(r))
  templates.value = (await routingApi.templates()).data
  try { sets.value = (await ruleSetApi.list()).data } catch (_) { sets.value = [] }
  snapshot.value = current()
}
onMounted(load)

function beforeUnload(e) { if (unsaved.value) { e.preventDefault(); e.returnValue = '' } }
window.addEventListener('beforeunload', beforeUnload)
onBeforeUnmount(() => window.removeEventListener('beforeunload', beforeUnload))
onBeforeRouteLeave(async () => {
  if (!unsaved.value) return true
  try {
    await ElMessageBox.confirm('路由规则有未保存的修改，离开后将丢失。', '未保存', { confirmButtonText: '放弃修改并离开', cancelButtonText: '留在此页', type: 'warning' })
    return true
  } catch (_) { return false }
})

function defaultProxyTag() {
  const d = defaultOut.value
  if (d && d !== 'direct' && d !== 'block' && !outState(d).bad) return d
  return panel.outbounds.find((o) => o.kind !== 'builtin' && o.available)?.tag || 'direct'
}
function addRule() {
  rules.value.push(withKey({ name: '', type: 'domain-suffix', value: '', outbound: defaultProxyTag(), enabled: true }))
}
function move(i, d) {
  const j = i + d
  if (j < 0 || j >= rules.value.length) return
  const a = rules.value
  ;[a[i], a[j]] = [a[j], a[i]]
}

// ---- 模板 ----
const tplDlg = ref(false)
const tplForm = reactive({ id: '', proxy: '' })
const tplSel = computed(() => templates.value.find((t) => t.id === tplForm.id))
const tplNeedsProxy = computed(() => tplSel.value?.rules.some((r) => r.outbound === '__PROXY__'))
function openTemplates() {
  Object.assign(tplForm, { id: templates.value[0]?.id || '', proxy: defaultProxyTag() })
  tplDlg.value = true
}
function doTemplate() {
  const tpl = tplSel.value
  if (!tpl) return
  const add = applyTemplate(tpl.rules, tplForm.proxy, rules.value)
  if (!add.length) { ElMessage.info('这些规则已经存在'); tplDlg.value = false; return }
  rules.value.push(...add.map(withKey))
  tplDlg.value = false
  ElMessage.success(`已追加 ${add.length} 条规则，检查后保存`)
}

// ---- 规则测试 ----
const testDlg = ref(false)
const testForm = reactive({ target: '', source: 'draft' })
const testing = ref(false)
const testRes = ref(null)
const highlight = ref(0)
const EXAMPLES = ['www.google.com', 'www.baidu.com', 'www.netflix.com', 'chatgpt.com']
async function runTest(t) {
  if (t) testForm.target = t
  if (!testForm.target.trim()) return
  testing.value = true
  try { testRes.value = (await routingApi.test(testForm.target, testForm.source)).data }
  catch (e) { testRes.value = null; ElMessage.error(apiError(e)) }
  finally { testing.value = false }
}
function gotoRule(n) {
  testDlg.value = false
  highlight.value = n
  setTimeout(() => (highlight.value = 0), 2500)
}
const KIND_TEXT = { user: '分流规则', private: '内网地址自动直连', default: '默认出口', none: '未命中' }

async function save(andApply = false) {
  if (hasErrors.value) return ElMessage.error('有规则格式错误，请先修正标红的规则')
  saving.value = true
  try {
    await routingApi.put({ default_outbound: defaultOut.value, domain_strategy: domainStrategy.value, rules: cleanRules(rules.value) })
    await panel.refreshAll(); emit('changed')
    await load()
    if (andApply) await applyConfig(panel)
    else ElMessage.success('已保存，点击右上角「应用并重启」生效')
  } catch (e) { ElMessage.error(apiError(e)) }
  finally { saving.value = false }
}
function exportRules() {
  const blob = new Blob([JSON.stringify({ rules: cleanRules(rules.value) }, null, 2)], { type: 'application/json' })
  const a = document.createElement('a'); a.href = URL.createObjectURL(blob); a.download = 'xray-rules.json'; a.click()
}
function importRules(ev) {
  const f = ev.target.files[0]; if (!f) return
  const rd = new FileReader()
  rd.onload = () => {
    try {
      const list = JSON.parse(rd.result).rules
      if (!Array.isArray(list)) throw new Error('文件中没有 rules 数组')
      const valid = new Set(RULE_TYPES.map(([v]) => v))
      const imported = list.filter((r) => valid.has(r.type) && r.value).map((r) => withKey({ ...r, enabled: r.enabled !== false }))
      rules.value = imported
      const bad = imported.filter((r) => !panel.outboundMap[r.outbound]).length
      ElMessage[bad ? 'warning' : 'success'](bad ? `已导入 ${imported.length} 条，其中 ${bad} 条的出口在本机不存在，请重新选择` : `已导入 ${imported.length} 条，检查后保存`)
    } catch (e) { ElMessage.error('导入失败: ' + e.message) }
  }
  rd.readAsText(f); ev.target.value = ''
}
</script>

<template>
  <el-card>
    <template #header>基础设置</template>
    <el-form label-width="110px">
      <el-form-item label="默认出口">
        <el-select v-model="defaultOut" filterable style="width:min(360px, 100%)">
          <el-option-group v-for="g in grouped" :key="g.label" :label="g.label">
            <el-option v-for="o in g.options" :key="o.tag" :label="o.label" :value="o.tag">
              <span>{{ o.label }}</span><span v-if="!o.available" class="opt-bad">{{ o.reason }}</span>
            </el-option>
          </el-option-group>
        </el-select>
        <span class="muted" style="margin-left:8px">未命中任何规则的流量走这里</span>
        <div v-if="defaultOut && outState(defaultOut).bad" class="err-text" style="width:100%">⚠ 默认出口不可用（{{ outState(defaultOut).text }}），应用时会临时改用第一个可用节点</div>
      </el-form-item>
      <el-form-item label="域名匹配策略">
        <el-select v-model="domainStrategy" style="width:min(360px, 100%)">
          <el-option v-for="[v, l] in DOMAIN_STRATEGIES" :key="v" :label="`${v} — ${l}`" :value="v" />
        </el-select>
      </el-form-item>
    </el-form>
  </el-card>

  <el-card style="margin-top:16px;">
    <template #header>
      <div class="hd"><span>分流规则（自上而下，先命中先生效 · 可拖拽排序 · 一条规则可填多个值，用逗号分隔）</span>
        <div class="tools">
          <el-button @click="testDlg = true">规则测试</el-button>
          <el-button @click="openTemplates">套用模板</el-button>
          <el-button @click="exportRules">导出</el-button>
          <el-button @click="$refs.imp.click()">导入</el-button>
          <input ref="imp" type="file" accept="application/json" hidden @change="importRules" />
          <el-button type="primary" @click="addRule">+ 添加规则</el-button>
        </div></div>
    </template>

    <el-empty v-if="!rules.length" description="还没有规则，所有流量都走默认出口。可以先套用「国内直连，国外走代理」模板。" :image-size="60" />
    <div v-else class="table-wrap">
      <table class="rules">
        <thead><tr><th>#</th><th>排序</th><th>启用</th><th>名称</th><th>匹配类型</th><th>匹配值</th><th>出口</th><th></th></tr></thead>
        <draggable v-model="rules" tag="tbody" item-key="_k" handle=".grip">
          <template #item="{ element, index }">
            <tr :class="{ off: !element.enabled, hl: highlight === index + 1 }">
              <td class="grip">⠿ {{ index + 1 }}</td>
              <td><el-button-group>
                <el-button size="small" :disabled="index === 0" @click="move(index, -1)">↑</el-button>
                <el-button size="small" :disabled="index === rules.length - 1" @click="move(index, 1)">↓</el-button>
              </el-button-group></td>
              <td><el-switch v-model="element.enabled" /></td>
              <td><el-input v-model="element.name" size="small" maxlength="40" placeholder="可选" style="width:130px" /></td>
              <td><el-select v-model="element.type" size="small" style="width:130px"
                             @change="(t) => { if (t === 'set' || element.value.startsWith('set-')) element.value = '' }">
                <el-option v-for="[v, l] in RULE_TYPES" :key="v" :label="l" :value="v" /></el-select></td>
              <td class="val">
                <template v-if="element.type === 'set'">
                  <el-select v-model="element.value" size="small" filterable style="width:100%" placeholder="选择分组"
                             :no-data-text="'还没有分组'">
                    <el-option v-for="g in sets" :key="g.tag" :value="g.tag" :label="g.name">
                      <span>{{ g.name }}</span><span class="muted"> · 域名 {{ g.domain_count }} · IP {{ g.ip_count }}{{ g.source ? ' · 订阅' : '' }}</span>
                    </el-option>
                  </el-select>
                  <div v-if="element.value && !setMap[element.value]" class="err-text">分组已删除</div>
                  <div v-else-if="element.value" class="muted">域名 {{ setMap[element.value].domain_count }} 条 · IP {{ setMap[element.value].ip_count }} 条
                    · <router-link to="/routing/sets">编辑分组</router-link></div>
                  <div v-else class="muted"><router-link to="/routing/sets">管理域名 / IP 分组</router-link></div>
                </template>
                <el-input v-else v-model="element.value" size="small" :placeholder="RULE_PLACEHOLDER[element.type]" />
                <div v-if="errors[index]" class="err-text">{{ errors[index] }}</div>
                <div v-else-if="!element.value.trim() && element.type !== 'set'" class="muted">空值规则保存时会被忽略</div>
              </td>
              <td>
                <el-select v-model="element.outbound" size="small" filterable style="width:200px">
                  <el-option-group v-for="g in grouped" :key="g.label" :label="g.label">
                    <el-option v-for="o in g.options" :key="o.tag" :label="o.label" :value="o.tag" />
                  </el-option-group>
                </el-select>
                <div v-if="outState(element.outbound).bad" class="err-text">⚠ {{ outState(element.outbound).text }}，应用时跳过</div>
              </td>
              <td><el-button size="small" type="danger" @click="rules.splice(index, 1)">删</el-button></td>
            </tr>
          </template>
        </draggable>
      </table>
    </div>
  </el-card>

  <div class="savebar" :class="{ show: unsaved }">
    <span class="dirty">● 路由有未保存的修改</span>
    <el-button @click="load">放弃修改</el-button>
    <el-button :loading="saving" @click="save(false)">保存</el-button>
    <el-button type="primary" :loading="saving" @click="save(true)">保存并应用</el-button>
  </div>

  <el-dialog v-model="testDlg" title="规则测试：这个网站会走哪个出口？" width="min(600px, 94vw)">
    <div class="tools">
      <el-input v-model="testForm.target" placeholder="域名、IP 或网址，如 www.google.com / https://x.com:8443" style="flex:1;min-width:220px"
                @keyup.enter="runTest()" />
      <el-button type="primary" :loading="testing" @click="runTest()">测试</el-button>
    </div>
    <div class="tools" style="margin-top:8px">
      <el-radio-group v-model="testForm.source" size="small">
        <el-radio-button value="draft">已保存的规则</el-radio-button>
        <el-radio-button value="applied">当前生效的配置</el-radio-button>
      </el-radio-group>
      <el-button v-for="e in EXAMPLES" :key="e" size="small" text @click="runTest(e)">{{ e }}</el-button>
    </div>
    <div v-if="unsaved" class="muted" style="margin-top:6px">⚠ 页面上有未保存的修改，测试使用的是已保存的规则</div>
    <div v-if="testRes" class="test-out">
      <div class="muted">{{ KIND_TEXT[testRes.result.kind] }}{{ testRes.result.user_rule ? ` · 第 ${testRes.result.user_rule} 条` : '' }}：{{ testRes.result.match }}</div>
      <div class="big">→ {{ testRes.label }}</div>
      <el-button v-if="testRes.result.user_rule" size="small" @click="gotoRule(testRes.result.user_rule)">定位到这条规则</el-button>
      <ol><li v-for="(s, i) in testRes.result.steps" :key="i">{{ s }}</li></ol>
      <el-alert v-for="(n, i) in testRes.result.notes" :key="i" type="warning" :closable="false" :title="n" style="margin-top:6px" />
      <div class="muted" style="margin-top:6px">域名解析使用服务器系统 DNS，可能与 Xray 内置 DNS 的结果略有不同。</div>
    </div>
  </el-dialog>

  <el-dialog v-model="tplDlg" title="套用路由模板" width="min(520px, 94vw)">
    <el-radio-group v-model="tplForm.id" class="tpl-list">
      <el-radio v-for="t in templates" :key="t.id" :value="t.id" border>
        <b>{{ t.name }}</b><div class="muted">{{ t.desc }}</div>
      </el-radio>
    </el-radio-group>
    <el-form v-if="tplNeedsProxy" label-width="90px" style="margin-top:12px">
      <el-form-item label="代理出口">
        <el-select v-model="tplForm.proxy" filterable style="width:100%">
          <el-option-group v-for="g in grouped" :key="g.label" :label="g.label">
            <el-option v-for="o in g.options" :key="o.tag" :label="o.label" :value="o.tag" />
          </el-option-group>
        </el-select>
      </el-form-item>
    </el-form>
    <template #footer><el-button @click="tplDlg = false">取消</el-button>
      <el-button type="primary" @click="doTemplate">追加到规则末尾</el-button></template>
  </el-dialog>
</template>

<style scoped>
a { color:var(--el-color-primary); text-decoration:none; }
.hd { display:flex; justify-content:space-between; align-items:center; gap:8px; flex-wrap:wrap; }
.tools { display:flex; gap:6px; flex-wrap:wrap; }
.table-wrap { overflow-x:auto; }
.rules { width:100%; border-collapse:collapse; min-width:760px; }
.rules th, .rules td { padding:6px 8px; border-bottom:1px solid var(--el-border-color); text-align:left; vertical-align:top; }
.rules tr.off td { opacity:.55; }
.rules tr.hl td { background:var(--el-color-warning-light-8); transition:background .3s; }
.test-out { margin-top:14px; padding:12px; border-radius:6px; background:var(--el-fill-color-light); }
.test-out .big { font-size:18px; font-weight:700; margin:4px 0 8px; }
.test-out ol { margin:6px 0 0; padding-left:20px; font-size:13px; line-height:1.7; color:var(--el-text-color-regular); }
.val { min-width:220px; }
.grip { cursor:grab; color:var(--el-text-color-secondary); white-space:nowrap; padding-top:10px !important; }
.err-text { color:var(--el-color-danger); font-size:12px; margin-top:2px; }
.opt-bad { float:right; color:var(--el-color-danger); font-size:12px; margin-left:12px; }
.savebar { position:sticky; bottom:0; margin-top:16px; padding:10px 16px; background:var(--el-bg-color); border:1px solid var(--el-border-color);
           border-radius:6px; display:none; justify-content:flex-end; align-items:center; gap:8px; box-shadow:0 -2px 8px rgba(0,0,0,.06); }
.savebar.show { display:flex; }
.dirty { color:var(--el-color-warning); font-weight:600; margin-right:auto; }
.tpl-list { display:flex; flex-direction:column; align-items:stretch; gap:8px; }
.tpl-list :deep(.el-radio) { height:auto; padding:8px 12px; margin-right:0; }
</style>
