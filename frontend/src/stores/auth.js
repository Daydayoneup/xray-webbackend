import { defineStore } from 'pinia'
import { ref } from 'vue'
import { authApi } from '../api/index.js'
import { setToken, getToken } from '../api/http.js'

export const useAuth = defineStore('auth', () => {
  const token = ref(getToken())
  async function login(password) {
    const { data } = await authApi.login(password)
    setToken(data.token); token.value = data.token
  }
  async function logout() {
    try { await authApi.logout() } catch (_) {}
    setToken(null); token.value = null
  }
  function isAuthed() { return !!token.value }
  return { token, login, logout, isAuthed }
})
