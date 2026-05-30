<script setup>
import { ref, reactive, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { authApi, subscriptionApi, xrayApi } from '../api/index.js'
import { apiError, setToken } from '../api/http.js'

const pw = reactive({ old_password: '', new_password: '' })
const sub = ref({})
const rawConfig = ref('')

async function load() {
  sub.value = (await subscriptionApi.get()).data
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
    <template #header>订阅信息</template>
    <p>链接:{{ sub.url || '—' }}</p>
    <p>备注:{{ sub.remarks || '—' }} {{ sub.status }}</p>
    <p v-if="sub.fetched_at">拉取于:{{ new Date(sub.fetched_at * 1000).toLocaleString() }}</p>
  </el-card>

  <el-card style="margin-top:16px;">
    <template #header>生成的 config.json(只读)</template>
    <el-input v-model="rawConfig" type="textarea" :rows="16" readonly />
  </el-card>
</template>
