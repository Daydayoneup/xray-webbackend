import { describe, it, expect, beforeEach } from 'vitest'
import { http, setToken } from '../api/http.js'

describe('http interceptor', () => {
  beforeEach(() => setToken(null))
  it('injects bearer when token set', () => {
    setToken('abc')
    const cfg = http.interceptors.request.handlers[0].fulfilled({ headers: {} })
    expect(cfg.headers.Authorization).toBe('Bearer abc')
  })
  it('no header when token null', () => {
    const cfg = http.interceptors.request.handlers[0].fulfilled({ headers: {} })
    expect(cfg.headers.Authorization).toBeUndefined()
  })
})
