export function latClass(ms) {
  if (ms == null) return 'bad'
  if (ms < 200) return 'good'
  if (ms < 500) return 'mid'
  return 'bad'
}
export function latText(ms) { return ms == null ? '超时' : `${ms}ms` }

export function aliveStats(nodes) {
  const usable = nodes.filter((n) => !n.disabled && !n.missing)
  const alive = usable.filter((n) => n.latency != null)
  const fastest = alive.slice().sort((a, b) => a.latency - b.latency)[0] || null
  return { total: nodes.length, alive: alive.length, fastest }
}

export function fmtBytes(n) {
  if (n == null || n < 0) return '—'
  const u = ['B', 'KB', 'MB', 'GB', 'TB']
  let i = 0
  while (n >= 1024 && i < u.length - 1) { n /= 1024; i++ }
  return `${n.toFixed(i >= 3 ? 2 : 0)} ${u[i]}`
}

export function fmtTime(ts) {
  if (!ts) return '—'
  return new Date(ts * 1000).toLocaleString()
}

// "3 分钟前" 形式的相对时间
export function fmtAgo(ts, now = Date.now() / 1000) {
  if (!ts) return '从未'
  const d = Math.max(0, now - ts)
  if (d < 60) return '刚刚'
  if (d < 3600) return `${Math.floor(d / 60)} 分钟前`
  if (d < 86400) return `${Math.floor(d / 3600)} 小时前`
  return `${Math.floor(d / 86400)} 天前`
}

export function subName(s) {
  if (!s) return ''
  if (s.name || s.remarks) return s.name || s.remarks
  try { return new URL(s.url).hostname } catch (_) { return `订阅 ${s.id}` }
}

// 订阅到期：返回 { text, level }，level: ok | warn | danger
export function expireInfo(expire, now = Date.now() / 1000) {
  if (!expire) return null
  const days = Math.floor((expire - now) / 86400)
  if (days < 0) return { text: '已过期', level: 'danger' }
  const date = new Date(expire * 1000).toLocaleDateString()
  return { text: `${date}（剩 ${days} 天）`, level: days <= 7 ? 'warn' : 'ok' }
}

// 时长（秒）→ "3 分钟" / "2 小时 5 分钟"
export function fmtDur(sec) {
  sec = Math.max(0, Math.floor(sec || 0))
  if (sec < 60) return '不到 1 分钟'
  if (sec < 3600) return `${Math.floor(sec / 60)} 分钟`
  if (sec < 86400) return `${Math.floor(sec / 3600)} 小时 ${Math.floor((sec % 3600) / 60)} 分钟`
  return `${Math.floor(sec / 86400)} 天 ${Math.floor((sec % 86400) / 3600)} 小时`
}

// 内网 / 本机 / 链路本地地址
export function isPrivateIP(ip) {
  if (!ip) return false
  const v = ip.toLowerCase()
  if (v.includes(':')) return v === '::1' || v.startsWith('fc') || v.startsWith('fd') || v.startsWith('fe80')
  const p = v.split('.').map(Number)
  if (p.length !== 4) return false
  return p[0] === 10 || p[0] === 127 || (p[0] === 172 && p[1] >= 16 && p[1] <= 31) ||
    (p[0] === 192 && p[1] === 168) || (p[0] === 169 && p[1] === 254) || (p[0] === 100 && p[1] >= 64 && p[1] <= 127)
}
