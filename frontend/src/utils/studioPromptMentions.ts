export const studioMentionPattern = /\[第(\d+)张图\]|参考图(\d+)/g

export type StudioPromptPart = { type: 'text'; value: string } | { type: 'mention'; index: number }

export function studioMentionToken(index: number) {
  return `参考图${index}`
}

function mentionIndex(bracket?: string, plain?: string) {
  return Number(bracket || plain)
}

export function parseStudioPrompt(prompt: string): StudioPromptPart[] {
  const parts: StudioPromptPart[] = []
  let last = 0
  for (const match of prompt.matchAll(studioMentionPattern)) {
    const start = match.index ?? 0
    if (start > last) parts.push({ type: 'text', value: prompt.slice(last, start) })
    parts.push({ type: 'mention', index: mentionIndex(match[1], match[2]) })
    last = start + match[0].length
  }
  if (last < prompt.length) parts.push({ type: 'text', value: prompt.slice(last) })
  return parts
}

export function dropStudioMentionsAbove(prompt: string, maxIndex: number) {
  return prompt.replace(studioMentionPattern, (token, bracket, plain) => mentionIndex(bracket, plain) > maxIndex ? '' : token)
}

export function remapStudioMentionsOnRemove(prompt: string, removedIndex: number) {
  const removed = removedIndex + 1
  return prompt.replace(studioMentionPattern, (token, bracket, plain) => {
    const index = mentionIndex(bracket, plain)
    if (index === removed) return ''
    if (index > removed) return studioMentionToken(index - 1)
    return token
  })
}

export function studioPromptPasteNeedsReferences(text: string, names: (string | undefined)[]) {
  const available = new Set(names.filter((name): name is string => !!name))
  const displayMentions = text.match(/@[\w.-]+\.[\w.-]+/g) || []
  if (displayMentions.some(token => !available.has(token.slice(1)))) return true
  return studioPromptMaxMention(text) > available.size
}

export function studioPromptMaxMention(prompt: string) {
  let max = 0
  for (const match of prompt.matchAll(studioMentionPattern)) {
    max = Math.max(max, mentionIndex(match[1], match[2]))
  }
  return max
}

export function studioPromptDisplayText(prompt: string, names: (string | undefined)[]) {
  return parseStudioPrompt(prompt).map(part => part.type === 'text'
    ? part.value
    : `@${names[part.index - 1] || studioMentionToken(part.index)}`).join('')
}

export function studioPromptTokenizeText(text: string, names: (string | undefined)[]) {
  let out = text
  names.forEach((name, index) => {
    if (name) out = out.split(`@${name}`).join(studioMentionToken(index + 1))
  })
  return out
}
