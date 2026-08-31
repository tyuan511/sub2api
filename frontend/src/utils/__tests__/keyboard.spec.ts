import { describe, expect, it } from 'vitest'
import { isIMECompositionKeyEvent } from '../keyboard'

function keyboardEvent(overrides: Partial<KeyboardEvent> = {}): KeyboardEvent {
  return {
    isComposing: false,
    keyCode: 13,
    ...overrides
  } as KeyboardEvent
}

describe('isIMECompositionKeyEvent', () => {
  it('detects an active IME composition', () => {
    expect(isIMECompositionKeyEvent(keyboardEvent({ isComposing: true }))).toBe(true)
  })

  it('handles browsers that report IME keydown as keyCode 229', () => {
    expect(isIMECompositionKeyEvent(keyboardEvent({ keyCode: 229 }))).toBe(true)
  })

  it('allows a regular Enter key to submit', () => {
    expect(isIMECompositionKeyEvent(keyboardEvent())).toBe(false)
  })
})
