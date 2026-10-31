<script setup>
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { useAuth } from '../stores/auth.js'
import { apiError } from '../api/http.js'

const password = ref('')
const loading = ref(false)
const auth = useAuth()
const router = useRouter()

async function submit() {
  if (loading.value) return
  loading.value = true
  try {
    await auth.login(password.value)
    router.replace({ name: 'dashboard' })
  } catch (e) { ElMessage.error(apiError(e)) }
  finally { loading.value = false }
}
</script>

<template>
  <div class="login-wrap">
    <el-card class="login-card">
      <h2>Xray 面板登录</h2>
      <el-input v-model="password" type="password" placeholder="密码" show-password
                @keyup.enter="submit" />
      <el-button type="primary" :loading="loading" class="login-btn" @click="submit">登录</el-button>
    </el-card>
  </div>
</template>

<style scoped>
.login-wrap { display:flex; justify-content:center; padding-top:16vh; }
.login-card { width:360px; }
.login-card h2 { margin:0 0 16px; }
.login-btn { width:100%; margin-top:12px; }
</style>
