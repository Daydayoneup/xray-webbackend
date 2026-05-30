<script setup>
import { ref, onMounted, computed } from 'vue'
import { ElMessage } from 'element-plus'
import { subscriptionApi, routingApi } from '../../api/index.js'
import { apiError } from '../../api/http.js'
import { latClass, latText } from '../../utils/format.js'
import { usePanel } from '../../stores/panel.js'

const emit = defineEmits(['changed'])
const panel = usePanel()
const url = ref('')
const sub = ref({})
const nodes = ref([])
const filter = ref('')
const fetching = ref(false)
const testing = ref(false)

const shown = computed(() => {
  const q = filter.value.toLowerCase()
  return nodes.value.filter((n) => !q || n.name.toLowerCase().includes(q) || n.type.includes(q))
})

async function load() {
  sub.value = (await subscriptionApi.get()).data; url.value = sub.value.url || ''
  nodes.value = (await subscriptionApi.nodes()).data
}
onMounted(load)

async function fetchSub() {
  fetching.value = true
  try {
    const { data } = await subscriptionApi.set(url.value)
    nodes.value = data.nodes; sub.value = data.subscription
    await panel.refreshOutbounds(); emit('changed')
    ElMessage.success(`解析到 ${data.nodes.length} 个节点${data.skipped ? `,跳过 ${data.skipped} 个` : ''}`)
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
</script>

<template>
  <el-card>
    <template #header>订阅</template>
    <div class="row">
      <el-input v-model="url" placeholder="粘贴订阅链接 (http...)" />
      <el-button type="primary" :loading="fetching" @click="fetchSub">拉取并解析</el-button>
    </div>
    <div class="meta" v-if="sub.remarks || sub.status">{{ sub.remarks }} {{ sub.status }}</div>
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
.meta { margin-top:8px; color:var(--el-text-color-secondary); font-size:13px; }
.hd { display:flex; justify-content:space-between; align-items:center; }
.lat-good { color:var(--el-color-success); } .lat-mid { color:var(--el-color-warning); }
.lat-bad { color:var(--el-color-danger); }
</style>
