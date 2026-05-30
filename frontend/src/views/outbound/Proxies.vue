<script setup>
import { ref, onMounted, reactive } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { proxyApi } from '../../api/index.js'
import { apiError } from '../../api/http.js'
import { usePanel } from '../../stores/panel.js'

const emit = defineEmits(['changed'])
const panel = usePanel()
const list = ref([])
const dialog = ref(false)
const editing = ref(null)
const form = reactive({ name: '', protocol: 'socks', host: '', port: null, user: '', pass: '' })

async function load() { list.value = (await proxyApi.list()).data }
onMounted(load)

function openCreate() { editing.value = null; Object.assign(form, { name: '', protocol: 'socks', host: '', port: null, user: '', pass: '' }); dialog.value = true }
function openEdit(row) {
  editing.value = row.tag
  Object.assign(form, { name: row.name, protocol: row.protocol, host: row.host, port: row.port,
    user: row.auth?.user || '', pass: row.auth?.pass || '' })
  dialog.value = true
}
function payload() {
  const p = { name: form.name.trim(), protocol: form.protocol, host: form.host.trim(), port: form.port }
  if (form.user.trim() || form.pass) p.auth = { user: form.user.trim(), pass: form.pass }
  return p
}
async function save() {
  try {
    if (editing.value) await proxyApi.update(editing.value, payload())
    else await proxyApi.create(payload())
    dialog.value = false; await load(); await panel.refreshOutbounds(); emit('changed'); ElMessage.success('已保存')
  } catch (e) { ElMessage.error(apiError(e)) }
}
async function remove(row) {
  try {
    await ElMessageBox.confirm(`删除代理「${row.name}」?`, '确认', { type: 'warning' })
    await proxyApi.remove(row.tag); await load(); await panel.refreshAll(); emit('changed')
  } catch (e) {
    if (e !== 'cancel' && e !== 'close') ElMessage.error(apiError(e))
  }
}
</script>

<template>
  <el-card>
    <template #header>
      <div class="hd"><span>自定义出口代理(落地代理)</span>
        <el-button type="primary" @click="openCreate">+ 新建代理</el-button></div>
    </template>
    <el-table :data="list">
      <el-table-column prop="tag" label="tag" width="80" />
      <el-table-column prop="name" label="名称" />
      <el-table-column prop="protocol" label="协议" width="90" />
      <el-table-column prop="host" label="地址" />
      <el-table-column prop="port" label="端口" width="90" />
      <el-table-column label="操作" width="140"><template #default="{ row }">
        <el-button size="small" @click="openEdit(row)">编辑</el-button>
        <el-button size="small" type="danger" @click="remove(row)">删</el-button></template></el-table-column>
    </el-table>
  </el-card>

  <el-dialog v-model="dialog" :title="editing ? '编辑代理' : '新建代理'" width="460px">
    <el-form label-width="80px">
      <el-form-item label="名称"><el-input v-model="form.name" /></el-form-item>
      <el-form-item label="协议"><el-select v-model="form.protocol">
        <el-option label="socks" value="socks" /><el-option label="http" value="http" /></el-select></el-form-item>
      <el-form-item label="地址"><el-input v-model="form.host" placeholder="host" /></el-form-item>
      <el-form-item label="端口"><el-input-number v-model="form.port" :min="1" :max="65535" controls-position="right" /></el-form-item>
      <el-form-item label="账号"><el-input v-model="form.user" placeholder="可选" /></el-form-item>
      <el-form-item label="密码"><el-input v-model="form.pass" placeholder="可选" /></el-form-item>
    </el-form>
    <template #footer><el-button @click="dialog = false">取消</el-button>
      <el-button type="primary" @click="save">保存</el-button></template>
  </el-dialog>
</template>

<style scoped>.hd { display:flex; justify-content:space-between; align-items:center; }</style>
