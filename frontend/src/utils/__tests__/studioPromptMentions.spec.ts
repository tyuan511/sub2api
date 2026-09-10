import { describe, expect, it } from 'vitest'
import { dropStudioMentionsAbove, parseStudioPrompt, remapStudioMentionsOnRemove, studioMentionToken, studioPromptDisplayText, studioPromptMaxMention, studioPromptPasteNeedsReferences, studioPromptTokenizeText } from '../studioPromptMentions'

describe('studio prompt mentions', () => {
  it('parses mixed text and 1-based image tokens', () => {
    expect(parseStudioPrompt('用参考图1的风格改参考图2')).toEqual([
      { type: 'text', value: '用' },
      { type: 'mention', index: 1 },
      { type: 'text', value: '的风格改' },
      { type: 'mention', index: 2 },
    ])
    expect(parseStudioPrompt('用[第1张图]的风格')).toEqual([
      { type: 'text', value: '用' },
      { type: 'mention', index: 1 },
      { type: 'text', value: '的风格' },
    ])
    expect(studioMentionToken(3)).toBe('参考图3')
  })
  it('drops mentions beyond the current reference limit', () => {
    expect(dropStudioMentionsAbove('A参考图1B参考图3C', 2)).toBe('A参考图1BC')
  })
  it('remaps later mentions when a reference is removed', () => {
    expect(remapStudioMentionsOnRemove('A参考图1B参考图2C参考图3', 1)).toBe('A参考图1BC参考图2')
  })
  it('flags pasted prompts that reference unavailable images', () => {
    expect(studioPromptPasteNeedsReferences('用@cat.png的风格', ['cat.png'])).toBe(false)
    expect(studioPromptPasteNeedsReferences('用@cat.png的风格', [])).toBe(true)
    expect(studioPromptPasteNeedsReferences('用@cat.png和@dog.png', ['cat.png'])).toBe(true)
    expect(studioPromptPasteNeedsReferences('用参考图2的风格', ['cat.png'])).toBe(true)
    expect(studioPromptPasteNeedsReferences('普通文字', [])).toBe(false)
  })
  it('reports the highest referenced image index', () => {
    expect(studioPromptMaxMention('用参考图1和参考图3')).toBe(3)
    expect(studioPromptMaxMention('无引用')).toBe(0)
  })
  it('round-trips between stored and display forms', () => {
    expect(studioPromptDisplayText('用参考图1的风格改参考图2', ['cat.png', 'dog.png'])).toBe('用@cat.png的风格改@dog.png')
    expect(studioPromptTokenizeText('用@cat.png的风格改@dog.png', ['cat.png', 'dog.png'])).toBe('用参考图1的风格改参考图2')
  })
})
