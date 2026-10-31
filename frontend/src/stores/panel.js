import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import { routingApi, xrayApi } from '../api/index.js'

export const usePanel = defineStore('panel', () => {
  const outbounds = ref([])              // [{tag,label,kind,available,reason,refs}]
  const status = ref({ running: false, state: 'stopped', applied: false, dirty: false, warnings: [] })

  async function refreshOutbounds() {
    const { data } = await routingApi.outbounds(); outbounds.value = data
  }
  async function refreshStatus() {
    const { data } = await xrayApi.status(); status.value = data
  }
  async function refreshAll() { await Promise.all([refreshOutbounds(), refreshStatus()]) }

  const outboundMap = computed(() => Object.fromEntries(outbounds.value.map((o) => [o.tag, o])))
  function label(tag) { return outboundMap.value[tag]?.label || tag }

  return { outbounds, status, outboundMap, label, refreshOutbounds, refreshStatus, refreshAll }
})
