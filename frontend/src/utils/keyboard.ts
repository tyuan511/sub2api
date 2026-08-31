export function isIMECompositionKeyEvent(event: KeyboardEvent): boolean {
  return event.isComposing || event.keyCode === 229
}
