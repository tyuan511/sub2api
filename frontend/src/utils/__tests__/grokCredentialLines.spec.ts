import { describe, expect, it } from 'vitest'
import { parseGrokCredentialLines } from '../grokCredentialLines'

describe('parseGrokCredentialLines', () => {
  it('parses email----password----sso lines', () => {
    const result = parseGrokCredentialLines(
      'a@example.com----p@ss----sso-token-a\nb@example.com----p@ss2----sso-token-b'
    )

    expect(result.items).toEqual([
      { line: 1, email: 'a@example.com', password: 'p@ss', ssoToken: 'sso-token-a' },
      { line: 2, email: 'b@example.com', password: 'p@ss2', ssoToken: 'sso-token-b' }
    ])
    expect(result.duplicates).toBe(0)
    expect(result.invalidLines).toEqual([])
  })

  it('keeps dashes inside the password and cookie intact', () => {
    const result = parseGrokCredentialLines('a@example.com----p--ss----sso--token')

    expect(result.items[0].password).toBe('p--ss')
    expect(result.items[0].ssoToken).toBe('sso--token')
  })

  it('treats a two-field line as email + sso', () => {
    const result = parseGrokCredentialLines('a@example.com----sso-token')

    expect(result.items[0]).toMatchObject({ email: 'a@example.com', password: '', ssoToken: 'sso-token' })
  })

  it('treats a bare cookie line as sso only', () => {
    const result = parseGrokCredentialLines('sso-token-only')

    expect(result.items[0]).toMatchObject({ email: '', password: '', ssoToken: 'sso-token-only' })
  })

  it('extracts the sso value from a full cookie string', () => {
    const result = parseGrokCredentialLines('a@example.com----pw----foo=1; sso=abc123; sso-rw=def456')

    expect(result.items[0].ssoToken).toBe('abc123')
  })

  it('skips blank and comment lines and dedupes by sso cookie', () => {
    const result = parseGrokCredentialLines(
      ['# 说明', '', 'a@example.com----pw----token-1', 'b@example.com----pw----token-1'].join('\n')
    )

    expect(result.items).toHaveLength(1)
    expect(result.items[0].line).toBe(3)
    expect(result.duplicates).toBe(1)
  })

  it('reports lines without an sso cookie', () => {
    const result = parseGrokCredentialLines('a@example.com----pw----')

    expect(result.items).toHaveLength(0)
    expect(result.invalidLines).toEqual([1])
  })
})
