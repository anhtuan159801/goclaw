# Agnes Video API — Grounded Contract

Sources (fetched 2026-10-19, TinyFish fetch):
- https://wiki.agnes-ai.com/en/docs/agnes-video-25 (model `agnes-video-2.5`)
- https://wiki.agnes-ai.com/en/docs/agnes-video-25-flash (model `agnes-video-2.5-flash`)

## Endpoints

- Base URL: `https://apihub.agnes-ai.com/v1`
- Create: `POST {base}/videos` — `Authorization: Bearer <key>`, `Content-Type: application/json`
- Retrieve/poll: `GET https://apihub.agnes-ai.com/agnesapi?video_id=<VIDEO_ID>&model_name=<MODEL_ID>` (host root, NOT under `/v1`) — Bearer auth required. Poll every 1–2 s until `status` ∈ {`completed`, `failed`}. Always include `model_name` (a bare `video_id` query is only valid for `mode: "text"`).
- Result download: top-level `url` field on completed response (public/presigned URL).

## Create request (text mode)

```json
{
  "model": "agnes-video-2.5-flash",
  "prompt": "...",
  "mode": "text",
  "seconds": "5",
  "size": "720P",
  "aspect_ratio": "16:9",
  "n": 1
}
```

| Param | Type | Required | Notes |
|---|---|---|---|
| `model` | string | yes | `agnes-video-2.5` or `agnes-video-2.5-flash` |
| `prompt` | string | yes | |
| `mode` | string | yes | `text` / `keyframe` / `reference` — GoClaw uses `text` only |
| `seconds` | string | no | `"4"`–`"12"`, default `"5"` — **string, not int** |
| `size` | string | no | `"720P"`, `"1080P"`, `"1K"`, `"2K"` — **Flash: only `"720P"` (else HTTP 400 `{"detail":"size must be 720P"}`)** |
| `aspect_ratio` | string | no | `16:9` (default), `9:16`, `21:9`, `4:3`, `1:1`, `3:4` |
| `n` | int | no | only `1` supported |

Mode `keyframe` needs `first_frame`/`last_frame` as **publicly reachable URLs** — GoClaw's local base64 image is NOT usable → ignore `image_base64` with a warn (same behavior as chat backend).

## Create response

```json
{
  "id": "task_...",
  "task_id": "task_...",
  "video_id": "video_...",
  "object": "video",
  "model": "agnes-video-2.5-flash",
  "status": "queued",
  "progress": 0,
  "created_at": 1786900000,
  "seconds": "5",
  "size": "720P"
}
```

Use `video_id` for polling (SDK fallback: `video_id` or `id`).

## Poll response (completed)

```json
{
  "id": "task_...",
  "object": "video",
  "status": "completed",
  "progress": 100,
  "url": "https://.../video.mp4",
  "error": null,
  "seconds": "4",
  "size": "720P"
}
```

- `status`: `queued` → `in_progress` → `completed` | `failed` (terminal).
- `url` (top level) = video file; only valid when `status == "completed"`.
- Failed: `error` object with `message`, e.g. `{"error": {"message": "Invalid reference media"}}`.
- Use `status`/`progress`, never `internal_status`/`internal_progress`.
- 429 → backoff; 400 → `{"detail": "..."}`; 404 → bad video_id.

## Models & pricing

| Model | ID | Price | Constraints |
|---|---|---|---|
| Agnes Video 2.5 | `agnes-video-2.5` | 720P $0.025/s, 1080P/1K $0.040/s, 2K $0.055/s | all sizes/aspects |
| Agnes Video 2.5 Flash | `agnes-video-2.5-flash` | **$0/s (limited-time free promo, as of 2026-10-19)** | size fixed `720P`; ≤5 ref images; ≤3 ref audios; no ref videos |

GoClaw default chain model for provider `agnes`: `agnes-video-2.5-flash`.

## GoClaw param mapping

- GoClaw `duration` ∈ {4, 6, 8} (create_video.go clamps) → `seconds` string — all within `"4"–"12"`.
- GoClaw `aspect_ratio` ∈ {16:9, 9:16} → both supported.
- GoClaw `size` chain param (optional) → default `720P` (safe for Flash).
- image_to_video (`image_base64`/`image_mime` injected by Execute chain) → not supported in `text` mode → warn + ignore.
