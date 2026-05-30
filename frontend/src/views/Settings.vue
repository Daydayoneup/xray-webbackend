<script setup>
import { ref, reactive, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { authApi, subscriptionApi, xrayApi } from '../api/index.js'
import { apiError, setToken } from '../api/http.js'

const pw = reactive({ old_password: '', new_password: '' })
const subs = ref([])
const rawConfig = ref('')

async function load() {
  try { subs.value = (await subscriptionApi.list()).data } catch (_) { subs.value = [] }
  try { rawConfig.value = JSON.stringify((await xrayApi.config()).data, null, 2) }
  catch (_) { rawConfig.value = '(尚未应用过配置)' }
}
onMounted(load)

async function changePw() {
  try {
    const { data } = await authApi.changePassword(pw.old_password, pw.new_password)
    if (data.token) setToken(data.token)
    pw.old_password = ''; pw.new_password = ''; ElMessage.success('密码已修改')
  } catch (e) { ElMessage.error(apiError(e)) }
}
function fmtTime(ts) {
  if (!ts) return '—'
  return new Date(ts * 1000).toLocaleString()
}
</script>

<template>
  <el-card>
    <template #header>修改密码</template>
    <el-form label-width="90px" style="max-width:420px">
      <el-form-item label="原密码"><el-input v-model="pw.old_password" type="password" show-password /></el-form-item>
      <el-form-item label="新密码"><el-input v-model="pw.new_password" type="password" show-password /></el-form-item>
      <el-form-item><el-button type="primary" @click="changePw">保存</el-button></el-form-item>
    </el-form>
  </el-card>

  <el-card style="margin-top:16px;">
    <template #header>订阅信息 ({{ subs.length }})</template>
    <el-table :data="subs" size="small" v-if="subs.length">
      <el-table-column prop="remarks" label="备注" width="140"><template #default="{ row }">{{ row.remarks || '—' }}</template></el-table-column>
      <el-table-column prop="url" label="链接" show-overflow-tooltip />
      <el-table-column label="拉取时间" width="170"><template #default="{ row }">{{ fmtTime(row.fetched_at) }}</template></el-table-column>
    </el-table>
    <p v-else>暂无订阅</p>
  </el-card>

  <el-card style="margin-top:16px;">
    <template #header>生成的 config.json(只读)</template>
    <el-input v-model="rawConfig" type="textarea" :rows="16" readonly />
  </el-card>
</template>
