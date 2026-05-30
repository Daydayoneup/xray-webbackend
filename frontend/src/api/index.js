import { http } from './http.js'

export const authApi = {
  login: (password) => http.post('/auth/login', { password }),
  logout: () => http.post('/auth/logout'),
  changePassword: (oldp, newp) => http.put('/auth/password', { old_password: oldp, new_password: newp }),
}
export const subscriptionApi = {
  list: () => http.get('/subscriptions'),
  create: (url) => http.post('/subscriptions', { url }),
  remove: (id) => http.delete(`/subscriptions/${id}`),
  fetch: (id) => http.post(`/subscriptions/${id}/fetch`),
  fetchAll: () => http.post('/subscriptions/fetch-all'),
  nodes: () => http.get('/nodes'),
  test: () => http.post('/nodes/test'),
}
export const inboundApi = {
  list: () => http.get('/inbounds'),
  create: (b) => http.post('/inbounds', b),
  update: (tag, b) => http.put(`/inbounds/${tag}`, b),
  remove: (tag) => http.delete(`/inbounds/${tag}`),
}
export const proxyApi = {
  list: () => http.get('/proxies'),
  create: (b) => http.post('/proxies', b),
  update: (tag, b) => http.put(`/proxies/${tag}`, b),
  remove: (tag) => http.delete(`/proxies/${tag}`),
}
export const balancerApi = {
  list: () => http.get('/balancers'),
  create: (b) => http.post('/balancers', b),
  update: (tag, b) => http.put(`/balancers/${tag}`, b),
  remove: (tag) => http.delete(`/balancers/${tag}`),
}
export const routingApi = {
  get: () => http.get('/routing'),
  put: (b) => http.put('/routing', b),
  templates: () => http.get('/routing/templates'),
  outbounds: () => http.get('/outbounds'),
}
export const xrayApi = {
  status: () => http.get('/xray/status'),
  apply: () => http.post('/apply'),
  restart: () => http.post('/xray/restart'),
  config: () => http.get('/config'),
  topology: () => http.get('/topology'),
}
