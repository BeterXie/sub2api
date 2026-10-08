import { describe, expect, it } from 'vitest'
import { renderBrandMarkdown, renderBrandMarkdownPreview } from './brandMarkdown'
describe('brand documents', () => {
  it('uses the current canonical API origin in SDK examples', () => {
    const html = renderBrandMarkdown('`{{canonical_api_origin}}`\n\n`{{api_base_url}}/models`', 'https://mues.cc')
    expect(html).toContain('https://mues.cc/v1/models')
    expect(html).not.toContain('{{')
  })
  it('removes active HTML from published Markdown', () => {
    const html = renderBrandMarkdown('<script>alert(1)</script><img src="x" onerror="alert(1)"><a href="javascript:alert(1)">unsafe</a>', 'https://mues.cc')
    expect(html).not.toMatch(/script|onerror|javascript:/)
  })
  it('adds trusted media blob URLs only after sanitizing the rendered HTML', () => {
    const asset = '0123456789abcdef0123456789abcdef.png'
    const html = renderBrandMarkdownPreview(
      `![local](/api/v1/public/brand-media/${asset})\n![external](https://evil.example/api/v1/public/brand-media/${asset})`,
      'https://brand.example',
      { [asset]: 'blob:https://brand.example/trusted-preview' },
    )
    expect(html).toContain('src="blob:https://brand.example/trusted-preview"')
    expect(html).toContain(`src="https://evil.example/api/v1/public/brand-media/${asset}"`)
  })
  it('does not inject non-blob replacement values into sanitized HTML', () => {
    const asset = '0123456789abcdef0123456789abcdef.png'
    const html = renderBrandMarkdownPreview(
      `![local](/api/v1/public/brand-media/${asset})`,
      'https://brand.example',
      { [asset]: 'javascript:alert(1)' },
    )
    expect(html).toContain(`/api/v1/public/brand-media/${asset}`)
    expect(html).not.toContain('javascript:')
  })
})
