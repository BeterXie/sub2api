import { marked } from 'marked'
import DOMPurify from 'dompurify'

export function renderBrandMarkdown(markdown: string, apiOrigin: string): string {
  const content = markdown.split('{{canonical_api_origin}}').join(apiOrigin).split('{{api_base_url}}').join(apiOrigin + '/v1')
  return DOMPurify.sanitize(marked.parse(content, { async: false }))
}

const brandMediaPath = /^\/api\/v1\/public\/brand-media\/([a-f0-9]{32}\.(?:png|jpg|gif|webp))$/

export function renderBrandMarkdownPreview(markdown: string, apiOrigin: string, mediaURLs: Record<string, string>): string {
  const sanitized = renderBrandMarkdown(markdown, apiOrigin)
  if (!Object.keys(mediaURLs).length) return sanitized

  let allowedOrigin: string
  try {
    allowedOrigin = new URL(apiOrigin).origin
  } catch {
    return sanitized
  }

  const template = document.createElement('template')
  template.innerHTML = sanitized
  for (const image of template.content.querySelectorAll('img[src]')) {
    const source = image.getAttribute('src')
    if (!source) continue
    try {
      const resolved = new URL(source, allowedOrigin)
      const match = resolved.origin === allowedOrigin ? resolved.pathname.match(brandMediaPath) : null
      const objectURL = match ? mediaURLs[match[1]] : undefined
      if (objectURL?.startsWith('blob:')) image.setAttribute('src', objectURL)
    } catch {
      // Leave malformed sources in the already-sanitized HTML unchanged.
    }
  }
  return template.innerHTML
}
