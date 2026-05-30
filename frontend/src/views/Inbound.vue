<script setup>
import { ref, onMounted, reactive } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { inboundApi } from '../api/index.js'
import { apiError } from '../api/http.js'

const emit = defineEmits(['changed'])
const list = ref([])
const dialog = ref(false)
const editing = ref(null)            // tag | null(新建)
const form = reactive({ protocol: 'socks', listen: '127.0.0.1', port: null, udp: true, user: '', pass: '' })

async function load() { list.value = (await inboundApi.list()).data }
onMounted(load)

function openCreate() {
  editing.value = null
  Object.assign(form, { protocol: 'socks', listen: '127.0.0.1', port: null, udp: true, user: '', pass: '' })
  dialog.value = true
}
function openEdit(row) {
  editing.value = row.tag
  Object.assign(form, { protocol: row.protocol, listen: row.listen, port: row.port,
    udp: row.udp ?? true, user: row.auth?.user || '', pass: row.auth?.pass || '' })
  dialog.value = true
}
function payload() {
  const p = { protocol: form.protocol, listen: form.listen.trim() || '127.0.0.1', port: form.port }
  if (form.protocol === 'socks') p.udp = form.udp
  if (form.user.trim() || form.pass) p.auth = { user: form.user.trim(), pass: form.pass }
  return p
}
async function save() {
  try {
    if (editing.value) await inboundApi.update(editing.value, payload())
    else await inboundApi.create(payload())
    dialog.value = false; await load(); emit('changed'); ElMessage.success('已保存')
  } catch (e) { ElMessage.error(apiError(e)) }
}
async function remove(row) {
  try {
    await ElMessageBox.confirm(`删除入站 ${row.tag}?`, '确认', { type: 'warning' })
    await inboundApi.remove(row.tag); await load(); emit('changed')
  } catch (e) {
    if (e !== 'cancel' && e !== 'close') ElMessage.error(apiError(e))
  }
}
function risky(row) { return row.listen === '0.0.0.0' && !row.auth }
</script>

<template>
  <el-card>
    <template #header>
      <div class="hd"><span>入站 Inbound(本地代理端口)</span>
        <el-button type="primary" @click="openCreate">+ 新建入站</el-button></div>
    </template>
    <el-table :data="list">
      <el-table-column prop="tag" label="tag" width="90" />
      <el-table-column prop="protocol" label="协议" width="90" />
      <el-table-column prop="listen" label="监听" />
      <el-table-column prop="port" label="端口" width="90" />
      <el-table-column label="鉴权"><template #default="{ row }">
        <span v-if="row.auth">{{ row.auth.user }}</span>
        <el-tag v-else-if="risky(row)" type="warning" size="small">⚠ 0.0.0.0 无密码</el-tag>
        <span v-else>—</span></template></el-table-column>
      <el-table-column label="操作" width="140"><template #default="{ row }">
        <el-button size="small" @click="openEdit(row)">编辑</el-button>
        <el-button size="small" type="danger" @click="remove(row)">删</el-button></template></el-table-column>
    </el-table>
  </el-card>

  <el-dialog v-model="dialog" :title="editing ? '编辑入站' : '新建入站'" width="460px">
    <el-form label-width="80px">
      <el-form-item label="协议"><el-select v-model="form.protocol">
        <el-option label="socks" value="socks" /><el-option label="http" value="http" /></el-select></el-form-item>
      <el-form-item label="监听地址"><el-input v-model="form.listen" placeholder="127.0.0.1" /></el-form-item>
      <el-form-item label="端口"><el-input-number v-model="form.port" :min="1" :max="65535" controls-position="right" /></el-form-item>
      <el-form-item v-if="form.protocol === 'socks'" label="UDP"><el-switch v-model="form.udp" /></el-form-item>
      <el-form-item label="账号"><el-input v-model="form.user" placeholder="可选" /></el-form-item>
      <el-form-item label="密码"><el-input v-model="form.pass" placeholder="可选" /></el-form-item>
    </el-form>
    <template #footer><el-button @click="dialog = false">取消</el-button>
      <el-button type="primary" @click="save">保存</el-button></template>
  </el-dialog>
</template>

<style scoped>.hd { display:flex; justify-content:space-between; align-items:center; }</style>
