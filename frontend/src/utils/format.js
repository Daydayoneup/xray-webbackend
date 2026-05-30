export function latClass(ms) {
  if (ms == null) return 'bad'
  if (ms < 200) return 'good'
  if (ms < 500) return 'mid'
  return 'bad'
}
export function latText(ms) { return ms == null ? '超时' : `${ms}ms` }

export function aliveStats(nodes) {
  const alive = nodes.filter((n) => n.latency != null)
  const fastest = alive.slice().sort((a, b) => a.latency - b.latency)[0] || null
  return { total: nodes.length, alive: alive.length, fastest }
}
