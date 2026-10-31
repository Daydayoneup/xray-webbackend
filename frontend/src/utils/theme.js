import { ref, watch } from 'vue'

const KEY = 'xray_theme'
function read() { try { return localStorage.getItem(KEY) } catch (_) { return null } }

const prefersDark = typeof window !== 'undefined' && window.matchMedia?.('(prefers-color-scheme: dark)').matches
export const dark = ref(read() ? read() === 'dark' : !!prefersDark)

function apply(v) { if (typeof document !== 'undefined') document.documentElement.classList.toggle('dark', v) }
apply(dark.value)
watch(dark, (v) => { apply(v); try { localStorage.setItem(KEY, v ? 'dark' : 'light') } catch (_) {} })
