import axios from 'axios'

const _storage = typeof localStorage !== 'undefined' && typeof localStorage.getItem === 'function' ? localStorage : null
let _token = _storage ? _storage.getItem('xray_token') : null

export function setToken(t) {
  _token = t
  if (!_storage) return
  if (t) _storage.setItem('xray_token', t)
  else _storage.removeItem('xray_token')
}
export function getToken() { return _token }

export const http = axios.create({ baseURL: '/api' })

http.interceptors.request.use((cfg) => {
  if (_token) cfg.headers.Authorization = `Bearer ${_token}`
  return cfg
})

let onUnauth = () => {}
export function setUnauthHandler(fn) { onUnauth = fn }

http.interceptors.response.use(
  (r) => r,
  (err) => {
    if (err.response && err.response.status === 401 && _token) { setToken(null); onUnauth() }
    return Promise.reject(err)
  },
)

export function apiError(err) {
  return err?.response?.data?.detail || err?.message || '请求失败'
}
