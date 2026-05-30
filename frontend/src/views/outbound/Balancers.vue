<script setup>
import { ref, onMounted, reactive } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { balancerApi, subscriptionApi } from '../../api/index.js'
import { apiError } from '../../api/http.js'
import { usePanel } from '../../stores/panel.js'

const emit = defineEmits(['changed'])
const panel = usePanel()
const list = ref([])
const nodes = ref([])
const dialog = ref(false)
const editing = ref(null)
const form = reactive({ name: '', nodes: [] })

async function load() {
  list.value = (await balancerApi.list()).data
  nodes.value = (await subscriptionApi.nodes()).data
}
onMounted(load)

const transferData = () => nodes.value.map((n) => ({ key: n.tag, label: `${n.name} (${n.tag})` }))

function openCreate() { editing.value = null; Object.assign(form, { name: '', nodes: [] }); dialog.value = true }
function openEdit(row) { editing.value = row.tag; Object.assign(form, { name: row.name, nodes: [...row.nodes] }); dialog.value = true }
async function save() {
  try {
    const body = { name: form.name, nodes: form.nodes }
    if (editing.value) await balancerApi.update(editing.value, body)
    else await balancerApi.create(body)
    dialog.value = false; await load(); await panel.refreshOutbounds(); emit('changed'); ElMessage.success('已保存')
  } catch (e) { ElMessage.error(apiError(e)) }
}
async function remove(row) {
  try {
    await ElMessageBox.confirm(`删除自动组「${row.name}」?`, '确认', { type: 'warning' })
    await balancerApi.remove(row.tag); await load(); await panel.refreshAll(); emit('changed')
  } catch (e) {
    if (e !== 'cancel' && e !== 'close') ElMessage.error(apiError(e))
  }
}
</script>

<template>
  <el-card>
    <template #header>
      <div class="hd"><span>自动组(负载均衡 · 自动选最快)</span>
        <el-button type="primary" @click="openCreate">+ 新建自动组</el-button></div>
    </template>
    <el-empty v-if="!list.length" description="无自动组。新建并勾选一组节点后,Xray 自动走延迟最低的活节点。" />
    <el-table v-else :data="list">
      <el-table-column prop="tag" label="tag" width="90" />
      <el-table-column prop="name" label="名称" />
      <el-table-column label="成员节点"><template #default="{ row }">{{ row.nodes.length }} 个</template></el-table-column>
      <el-table-column label="操作" width="140"><template #default="{ row }">
        <el-button size="small" @click="openEdit(row)">编辑</el-button>
        <el-button size="small" type="danger" @click="remove(row)">删</el-button></template></el-table-column>
    </el-table>
  </el-card>

  <el-dialog v-model="dialog" :title="editing ? '编辑自动组' : '新建自动组'" width="600px">
    <el-form label-width="70px">
      <el-form-item label="名称"><el-input v-model="form.name" /></el-form-item>
      <el-form-item label="节点">
        <el-transfer v-model="form.nodes" :data="transferData()" :titles="['可选节点', '已选节点']" filterable />
      </el-form-item>
    </el-form>
    <template #footer><el-button @click="dialog = false">取消</el-button>
      <el-button type="primary" @click="save">保存</el-button></template>
  </el-dialog>
</template>

<style scoped>.hd { display:flex; justify-content:space-between; align-items:center; }</style>
