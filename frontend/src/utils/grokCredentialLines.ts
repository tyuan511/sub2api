/**
 * Grok 批量上号凭证解析。
 *
 * 每行一个账号，支持三种写法：
 *   邮箱----密码----SSO Cookie   （标准三字段）
 *   邮箱----SSO Cookie           （无密码）
 *   SSO Cookie                   （只有 Cookie，邮箱从兑换结果里取）
 *
 * 密码只用于展示与人工核对：上号走 SSO Cookie 兑换，不会写入账号凭据。
 */

export interface GrokCredentialLine {
  /** 原始行号（从 1 开始），用于结果回显 */
  line: number
  /** 邮箱（可能为空，最终以兑换出的 token email 为准） */
  email: string
  /** Grok 密码（仅用于核对，不会持久化） */
  password: string
  /** Grok Web SSO Cookie */
  ssoToken: string
}

export interface ParsedGrokCredentialLines {
  items: GrokCredentialLine[]
  /** 因 SSO Cookie 重复而被跳过的行数 */
  duplicates: number
  /** 解析后仍然缺少 SSO Cookie 的行号 */
  invalidLines: number[]
}

const FIELD_SEPARATOR = '----'

function splitFields(line: string): string[] {
  const first = line.indexOf(FIELD_SEPARATOR)
  if (first < 0) return [line.trim()]
  const second = line.indexOf(FIELD_SEPARATOR, first + FIELD_SEPARATOR.length)
  if (second < 0) return [line.slice(0, first).trim(), line.slice(first + FIELD_SEPARATOR.length).trim()]
  return [
    line.slice(0, first).trim(),
    line.slice(first + FIELD_SEPARATOR.length, second).trim(),
    line.slice(second + FIELD_SEPARATOR.length).trim()
  ]
}

export function parseGrokCredentialLines(input: string): ParsedGrokCredentialLines {
  const items: GrokCredentialLine[] = []
  const invalidLines: number[] = []
  const seen = new Set<string>()
  let duplicates = 0

  input.split(/\r?\n/).forEach((rawLine, index) => {
    const line = rawLine.trim()
    // 空行与注释行（#）直接忽略，便于粘贴时带说明。
    if (!line || line.startsWith('#')) return

    const fields = splitFields(line)
    let email = ''
    let password = ''
    let ssoToken = ''

    if (fields.length >= 3) {
      email = fields[0]
      password = fields[1]
      ssoToken = fields[2]
    } else if (fields.length === 2) {
      // 两字段按「邮箱----SSO」处理：密码是可选字段。
      email = fields[0]
      ssoToken = fields[1]
    } else {
      ssoToken = fields[0]
    }

    // 允许粘贴 `sso=xxx` / `sso-rw=xxx` 形式的 Cookie 片段。
    const cookieMatch = ssoToken.match(/(?:^|;\s*)(?:sso-rw|sso)=([^;]+)/i)
    if (cookieMatch) ssoToken = cookieMatch[1].trim()

    if (!ssoToken) {
      invalidLines.push(index + 1)
      return
    }
    if (seen.has(ssoToken)) {
      duplicates++
      return
    }
    seen.add(ssoToken)
    items.push({ line: index + 1, email, password, ssoToken })
  })

  return { items, duplicates, invalidLines }
}
