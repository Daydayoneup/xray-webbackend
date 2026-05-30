<script setup>
import { ref, onMounted } from 'vue'
import draggable from 'vuedraggable'
import { ElMessage } from 'element-plus'
import { routingApi } from '../api/index.js'
import { apiError } from '../api/http.js'
import { usePanel } from '../stores/panel.js'
import { RULE_TYPES, cleanRules, applyTemplate } from './rules-helpers.js'

const emit = defineEmits(['changed'])
const panel = usePanel()
const defaultOut = ref('')
const rules = ref([])
const templates = ref({})
const tplSel = ref('')

let _kSeq = 0
const withKey = (r) => ({ ...r, _k: r._k ?? `k${_kSeq++}` })

async function load() {
  await panel.refreshOutbounds()
  const { data } = await routingApi.get()
  defaultOut.value = data.default_outbound
  rules.value = data.rules.map((r) => withKey(r))
  templates.value = (await routingApi.templates()).data
}
onMounted(load)

function addRule() {
  rules.value.push(withKey({ type: 'domain-suffix', value: '', outbound: defaultOut.value || 'direct', enabled: true }))
}
function move(i, d) {
  const j = i + d
  if (j < 0 || j >= rules.value.length) return
  const a = rules.value
  ;[a[i], a[j]] = [a[j], a[i]]
}
function doTemplate() {
  const tpl = templates.value[tplSel.value]
  if (!tpl) return
  rules.value.push(...applyTemplate(tpl, defaultOut.value || 'direct').map(withKey))
  ElMessage.success('已追加模板规则,检查后保存')
}
async function save() {
  try {
    await routingApi.put({ default_outbound: defaultOut.value, rules: cleanRules(rules.value) })
    await panel.refreshAll(); emit('changed'); ElMessage.success('已保存路由')
    await load()
  } catch (e) { ElMessage.error(apiError(e)) }
}
function exportRules() {
  const blob = new Blob([JSON.stringify({ rules: cleanRules(rules.value) }, null, 2)], { type: 'application/json' })
  const a = document.createElement('a'); a.href = URL.createObjectURL(blob); a.download = 'xray-rules.json'; a.click()
}
function importRules(ev) {
  const f = ev.target.files[0]; if (!f) return
  const rd = new FileReader()
  rd.onload = () => { try { rules.value = (JSON.parse(rd.result).rules || []).map((r) => withKey({ ...r, enabled: r.enabled !== false })); ElMessage.success('已导入,检查后保存') }
    catch (e) { ElMessage.error('导入失败: ' + e.message) } }
  rd.readAsText(f); ev.target.value = ''
}
</script>

<template>
  <el-card>
    <template #header>默认出口(未命中规则的流量)</template>
    <el-select v-model="defaultOut" style="width:320px">
      <el-option v-for="o in panel.outbounds" :key="o.tag" :label="`${o.label} (${o.tag})`" :value="o.tag" />
    </el-select>
  </el-card>

  <el-card style="margin-top:16px;">
    <template #header>
      <div class="hd"><span>分流规则(自上而下,先命中先生效 · 可拖拽排序)</span>
        <div class="tools">
          <el-select v-model="tplSel" placeholder="套用模板…" style="width:160px">
            <el-option v-for="(_, k) in templates" :key="k" :label="k" :value="k" /></el-select>
          <el-button @click="doTemplate">追加</el-button>
          <el-button @click="exportRules">导出</el-button>
          <el-button @click="$refs.imp.click()">导入</el-button>
          <input ref="imp" type="file" accept="application/json" hidden @change="importRules" />
          <el-button type="primary" @click="addRule">+ 添加规则</el-button>
        </div></div>
    </template>

    <table class="rules">
      <thead><tr><th>#</th><th>排序</th><th>启用</th><th>匹配类型</th><th>匹配值</th><th>出口</th><th></th></tr></thead>
      <draggable v-model="rules" tag="tbody" item-key="_k" handle=".grip">
        <template #item="{ element, index }">
          <tr>
            <td class="grip">⠿ {{ index + 1 }}</td>
            <td><el-button-group>
              <el-button size="small" :disabled="index === 0" @click="move(index, -1)">↑</el-button>
              <el-button size="small" :disabled="index === rules.length - 1" @click="move(index, 1)">↓</el-button>
            </el-button-group></td>
            <td><el-switch v-model="element.enabled" /></td>
            <td><el-select v-model="element.type" size="small" style="width:130px">
              <el-option v-for="[v, l] in RULE_TYPES" :key="v" :label="l" :value="v" /></el-select></td>
            <td><el-input v-model="element.value" size="small" placeholder="如 google.com / cn / 443" /></td>
            <td><el-select v-model="element.outbound" size="small" style="width:200px">
              <el-option v-for="o in panel.outbounds" :key="o.tag" :label="o.label" :value="o.tag" /></el-select></td>
            <td><el-button size="small" type="danger" @click="rules.splice(index, 1)">删</el-button></td>
          </tr>
        </template>
      </draggable>
    </table>

    <div class="save"><el-button type="primary" @click="save">保存路由</el-button></div>
  </el-card>
</template>

<style scoped>
.hd { display:flex; justify-content:space-between; align-items:center; gap:8px; flex-wrap:wrap; }
.tools { display:flex; gap:6px; flex-wrap:wrap; }
.rules { width:100%; border-collapse:collapse; }
.rules th, .rules td { padding:6px 8px; border-bottom:1px solid var(--el-border-color); text-align:left; }
.grip { cursor:grab; color:var(--el-text-color-secondary); white-space:nowrap; }
.save { margin-top:12px; text-align:right; }
</style>
