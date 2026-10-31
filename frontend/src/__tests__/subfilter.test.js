import { describe, it, expect } from 'vitest'
import { keywords, previewExcluded, DEFAULT_EXCLUDE } from '../utils/subfilter.js'

describe('subscription filter', () => {
  it('keywords splits on | , and full-width comma', () => {
    expect(keywords(' 剩余 | 到期,官网，Expire ')).toEqual(['剩余', '到期', '官网', 'expire'])
  })
  it('previewExcluded matches case-insensitively', () => {
    const names = ['剩余流量：50 GB', '香港 01', 'US EXPIRE 2027', '官网 x.com']
    expect(previewExcluded(names, DEFAULT_EXCLUDE)).toEqual(['剩余流量：50 GB', 'US EXPIRE 2027', '官网 x.com'])
    expect(previewExcluded(names, '')).toEqual([])
  })
})
