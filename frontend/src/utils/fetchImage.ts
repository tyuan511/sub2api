// Some S3-compatible stores omit Vary: Origin on ordinary <img> responses.
// That cached response cannot be read by fetch, even when CORS is configured.
// Recover once with a fresh CORS request, preserving the exact signed URL.
export async function fetchImage(url: string, signal: AbortSignal): Promise<Response> {
  const options: RequestInit = { credentials: 'omit', referrerPolicy: 'no-referrer', signal }
  try {
    return await fetch(url, options)
  } catch (error) {
    if (signal.aborted || !(error instanceof TypeError)) throw error
    return fetch(url, { ...options, cache: 'reload' })
  }
}
