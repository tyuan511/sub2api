# Images API upstream streaming

For a verified OpenAI API-key account, set `accounts.extra.openai_images_upstream_stream` to the boolean `true`. Missing, false, or incorrectly typed values keep the existing behavior. This is an account setting on our gateway; the upstream operator does not need to change their configuration.

The gateway sends `stream: true` and `Accept: text/event-stream` upstream for non-streaming image generation and edits, then aggregates final images into the regular JSON Images response. JSON and multipart requests retain their model mapping, prompt, count, dimensions, reference files, and other controls. Existing client SSE requests and OAuth accounts keep their original paths.

The adapter accepts standard `image_generation.completed` / `image_edit.completed` events and the verified provider's indexed `image.generation.result` events. It ignores progress and preview images, deduplicates repeated result indexes, adds per-result usage once, and reads dimensions from final image bytes for billing. The response size limit and an execution deadline of at most 30 minutes bound the request.

If generation stops after some final images arrive, those images are returned with `partial: true`, `requested_count`, and a `warning`; Image Studio stores them normally and shows its existing received/requested count notice. Billing uses the received images and their reported usage. If no final images arrive, the request returns an error. Ambiguous transport failures, upstream 408/5xx, and interrupted image streams do not trigger automatic failover or replay. Their upstream jobs may already have incurred charges.

This avoids Cloudflare's read timeout only while the upstream actually sends events often enough. It does not retrieve results from an older synchronous request, guarantee delivery through every network failure, or persist an in-progress HTTP stream across a process restart.

The initial deployment should enable this only for the verified `mdkj-image` account. Before enabling another provider, check its event format, generation/edit behavior, final-image and usage semantics, and error handling. Reverting the account flag restores the old request mode for new calls.
