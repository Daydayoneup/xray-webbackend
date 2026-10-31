import { http } from './http.js'

export const authApi = {
  login: (password) => http.post('/auth/login', { password }),
  logout: () => http.post('/auth/logout'),
  changePassword: (oldp, newp) => http.put('/auth/password', { old_password: oldp, new_password: newp }),
}
export const subscriptionApi = {
  list: () => http.get('/subscriptions'),
  create: (body) => http.post('/subscriptions', body),
  update: (id, body) => http.put(`/subscriptions/${id}`, body),
  remove: (id) => http.delete(`/subscriptions/${id}`),
  fetch: (id) => http.post(`/subscriptions/${id}/fetch`),
  fetchAll: () => http.post('/subscriptions/fetch-all'),
  nodes: () => http.get('/nodes'),
  test: (body = {}) => http.post('/nodes/test', body),
  setNode: (tag, body) => http.put(`/nodes/${tag}`, body),
  batchNodes: (tags, disabled) => http.post('/nodes/batch', { tags, disabled }),
}
export const inboundApi = {
  share: (tag, host) => http.get(`/inbounds/${tag}/share`, { params: { host } }),
  gen: (type, method) => http.get('/tools/gen', { params: { type, method } }),
  list: () => http.get('/inbounds'),
  create: (b) => http.post('/inbounds', b),
  update: (tag, b) => http.put(`/inbounds/${tag}`, b),
  remove: (tag) => http.delete(`/inbounds/${tag}`),
}
export const userApi = {
  list: () => http.get('/users'),
  create: (b) => http.post('/users', b),
  update: (name, b) => http.put(`/users/${encodeURIComponent(name)}`, b),
  remove: (name) => http.delete(`/users/${encodeURIComponent(name)}`),
  share: (name, host) => http.get(`/users/${encodeURIComponent(name)}/share`, { params: { host } }),
}
export const ruleSetApi = {
  list: () => http.get('/rulesets'),
  create: (b) => http.post('/rulesets', b),
  update: (tag, b) => http.put(`/rulesets/${tag}`, b),
  remove: (tag) => http.delete(`/rulesets/${tag}`),
  refresh: (tag) => http.post(`/rulesets/${tag}/refresh`, null, { timeout: 180000 }),
  setEntries: (tag, q = '', limit = 500) => http.get(`/rulesets/${tag}/entries`, { params: { q, limit } }),
  catalog: (kind) => http.get('/geo/catalog', { params: { kind } }),
  entries: (kind, code, q = '', limit = 500) => http.get('/geo/entries', { params: { kind, code, q, limit } }),
}
export const proxyApi = {
  list: () => http.get('/proxies'),
  create: (b) => http.post('/proxies', b),
  update: (tag, b) => http.put(`/proxies/${tag}`, b),
  remove: (tag) => http.delete(`/proxies/${tag}`),
}
export const balancerApi = {
  status: () => http.get('/balancers/status'),
  preview: (b) => http.post('/balancers/preview', b),
  select: (tag, selected) => http.put(`/balancers/${tag}/select`, { selected }),
  list: () => http.get('/balancers'),
  create: (b) => http.post('/balancers', b),
  update: (tag, b) => http.put(`/balancers/${tag}`, b),
  remove: (tag) => http.delete(`/balancers/${tag}`),
}
export const routingApi = {
  get: () => http.get('/routing'),
  put: (b) => http.put('/routing', b),
  templates: () => http.get('/routing/templates'),
  test: (target, source = 'draft') => http.post('/routing/test', { target, source }),
  outbounds: () => http.get('/outbounds'),
}
export const xrayApi = {
  status: () => http.get('/xray/status'),
  apply: () => http.post('/apply'),
  restart: () => http.post('/xray/restart'),
  logs: (n = 200) => http.get('/xray/logs', { params: { n } }),
  config: () => http.get('/config'),
  draft: () => http.get('/config/draft'),
  diff: () => http.get('/config/diff'),
  topology: () => http.get('/topology'),
}
export const opsApi = {
  traffic: (days) => http.get('/traffic', { params: { days } }),
  online: (params = {}) => http.get('/online', { params }),
  history: () => http.get('/history'),
  historyDiff: (id) => http.get(`/history/${id}/diff`),
  restore: (id) => http.post(`/history/${id}/restore`),
  exportUrl: '/api/backup',
  exportBackup: () => http.get('/backup', { responseType: 'blob' }),
  importBackup: (text) => http.post('/backup/restore', text, { headers: { 'Content-Type': 'application/json' } }),
  settings: () => http.get('/settings'),
  saveSettings: (b) => http.put('/settings', b),
  testNotify: () => http.post('/settings/notify/test'),
  updateGeo: () => http.post('/geo/update', null, { timeout: 0 }),
}
