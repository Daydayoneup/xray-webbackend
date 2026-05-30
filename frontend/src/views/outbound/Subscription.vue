<script setup>
import { ref, onMounted, computed } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { subscriptionApi, routingApi } from '../../api/index.js'
import { apiError } from '../../api/http.js'
import { latClass, latText } from '../../utils/format.js'
import { usePanel } from '../../stores/panel.js'

const emit = defineEmits(['changed'])
const panel = usePanel()
const url = ref('')
const subs = ref([])
const nodes = ref([])
const filter = ref('')
const fetching = ref(false)
const testing = ref(false)

const shown = computed(() => {
  const q = filter.value.toLowerCase()
  return nodes.value.filter((n) => !q || n.name.toLowerCase().includes(q) || n.type.includes(q))
})

async function load() {
  try {
    subs.value = (await subscriptionApi.list()).data
  } catch (_) { subs.value = [] }
  try {
    nodes.value = (await subscriptionApi.nodes()).data
  } catch (_) { nodes.value = [] }
}
onMounted(load)

async function addSub() {
  if (!url.value.trim()) return ElMessage.warning('请输入订阅链接')
  fetching.value = true
  try {
    const { data } = await subscriptionApi.create(url.value)
    await load(); await panel.refreshOutbounds(); emit('changed')
    ElMessage.success(`添加成功，合并 ${data.nodes_added} 个节点（总计 ${data.nodes_total}）`)
    url.value = ''
  } catch (e) { ElMessage.error(apiError(e)) }
  finally { fetching.value = false }
}

async function removeSub(id) {
  try {
    await ElMessageBox.confirm('删除该订阅？节点不会立即清除，下次拉取时更新。', '确认')
    await subscriptionApi.remove(id)
    await load(); emit('changed')
    ElMessage.success('已删除')
  } catch (_) { /* cancelled */ }
}

async function fetchSub(id) {
  fetching.value = true
  try {
    const { data } = await subscriptionApi.fetch(id)
    await load(); await panel.refreshOutbounds(); emit('changed')
    ElMessage.success(`拉取完成，节点总计 ${data.nodes_total}`)
  } catch (e) { ElMessage.error(apiError(e)) }
  finally { fetching.value = false }
}

async function fetchAll() {
  fetching.value = true
  try {
    const { data } = await subscriptionApi.fetchAll()
    await load(); await panel.refreshOutbounds(); emit('changed')
    ElMessage.success(`全部拉取完成，节点总计 ${data.nodes_total}`)
  } catch (e) { ElMessage.error(apiError(e)) }
  finally { fetching.value = false }
}

async function testSpeed() {
  testing.value = true
  try { nodes.value = (await subscriptionApi.test()).data; ElMessage.success('测速完成') }
  catch (e) { ElMessage.error(apiError(e)) }
  finally { testing.value = false }
}

async function pickFastest() {
  const alive = nodes.value.filter((n) => n.latency != null).sort((a, b) => a.latency - b.latency)
  if (!alive.length) return ElMessage.warning('先测速,或当前无可连节点')
  const rt = (await routingApi.get()).data
  await routingApi.put({ default_outbound: alive[0].tag, rules: rt.rules })
  await panel.refreshAll(); emit('changed')
  ElMessage.success(`默认出口已设为 ${alive[0].name}(${alive[0].latency}ms)`)
}

function fmtTime(ts) {
  if (!ts) return '—'
  return new Date(ts * 1000).toLocaleString()
}
</script>

<template>
  <el-card>
    <template #header>
      <span>订阅列表 ({{ subs.length }})</span>
      <el-button style="float:right" size="small" :loading="fetching" @click="fetchAll">全部拉取</el-button>
    </template>
    <div class="row" style="margin-bottom:10px">
      <el-input v-model="url" placeholder="粘贴订阅链接 (http...)" @keyup.enter="addSub" />
      <el-button type="primary" @click="addSub">添加</el-button>
    </div>
    <el-table :data="subs" size="small" v-if="subs.length">
      <el-table-column prop="remarks" label="备注" width="140" />
      <el-table-column prop="url" label="链接" show-overflow-tooltip />
      <el-table-column label="拉取时间" width="160">
        <template #default="{ row }">{{ fmtTime(row.fetched_at) }}</template>
      </el-table-column>
      <el-table-column label="操作" width="170">
        <template #default="{ row }">
          <el-button size="small" :loading="fetching" @click="fetchSub(row.id)">拉取</el-button>
          <el-button size="small" type="danger" @click="removeSub(row.id)">删</el-button>
        </template>
      </el-table-column>
    </el-table>
    <div v-else style="color:var(--el-text-color-secondary);font-size:13px;margin-top:6px">暂无订阅</div>
  </el-card>

  <el-card style="margin-top:16px;">
    <template #header>
      <div class="hd"><span>节点({{ nodes.length }})</span>
        <div class="row">
          <el-input v-model="filter" placeholder="过滤 名称/协议" style="width:180px" />
          <el-button @click="pickFastest">选最快为默认</el-button>
          <el-button :loading="testing" @click="testSpeed">测速</el-button>
        </div></div>
    </template>
    <el-table :data="shown" max-height="460">
      <el-table-column prop="tag" label="tag" width="90" />
      <el-table-column label="延迟" width="90"><template #default="{ row }">
        <span :class="'lat-' + latClass(row.latency)">{{ latText(row.latency) }}</span></template></el-table-column>
      <el-table-column prop="type" label="协议" width="110" />
      <el-table-column prop="name" label="名称" />
    </el-table>
  </el-card>
</template>

<style scoped>
.row { display:flex; gap:8px; }
.hd { display:flex; justify-content:space-between; align-items:center; }
.lat-good { color:var(--el-color-success); } .lat-mid { color:var(--el-color-warning); }
.lat-bad { color:var(--el-color-danger); }
</style>
