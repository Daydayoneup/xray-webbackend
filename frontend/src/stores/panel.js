import { defineStore } from 'pinia'
import { ref } from 'vue'
import { routingApi, xrayApi } from '../api/index.js'

export const usePanel = defineStore('panel', () => {
  const outbounds = ref([])              // [{tag,label,kind}]
  const status = ref({ running: false, applied: false, dirty: false })

  async function refreshOutbounds() {
    const { data } = await routingApi.outbounds(); outbounds.value = data
  }
  async function refreshStatus() {
    const { data } = await xrayApi.status(); status.value = data
  }
  async function refreshAll() { await Promise.all([refreshOutbounds(), refreshStatus()]) }
  return { outbounds, status, refreshOutbounds, refreshStatus, refreshAll }
})
