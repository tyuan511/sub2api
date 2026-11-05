import { describe, expect, it } from 'vitest'
import { groupCheckSvgUrl } from '../groupCheckSvg'

function decode(url: string | null) {
  return decodeURIComponent(url!.slice(url!.indexOf(',') + 1))
}

describe('groupCheckSvgUrl', () => {
  it('renders SVG as an isolated image and removes active and external content', () => {
    const url = groupCheckSvgUrl('<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script><image href="https://example.com/a"/><rect onload="alert(1)" style="background:url(https://example.com)" fill="url(https://example.com/a)" width="10" height="10"/></svg>')
    expect(url).toMatch(/^data:image\/svg\+xml/)
    const svg = decode(url)
    expect(svg).toContain('<rect')
    expect(svg).not.toMatch(/script|onload|https:|<image|style=/i)
  })

  it('rejects non-SVG, malformed and oversized content', () => {
    expect(groupCheckSvgUrl('<html>hello</html>')).toBeNull()
    expect(groupCheckSvgUrl('x'.repeat(1_000_001))).toBeNull()
    expect(groupCheckSvgUrl('')).toBeNull()
  })
})
