# Design — Agnes video backend cho create_video

## Current behavior (verified 2026-10-19)

- `internal/tools/create_video.go:150-151` — `ResolveMediaProviderChain(...)` với `videoGenProviderPriority` (create_video.go:18) và `videoGenModelDefaults` (create_video.go:21-26). `buildDefaultChain` (media_provider_chain.go:130-138) chỉ include provider `registry.Get()` thành công.
- `media_provider_chain.go:202` — `callParams["_provider_type"] = resolvedType`. Với provider name `agnes` (DB type `openai_compat`) → resolved = `openai_compat`.
- `create_video.go:223-232` — switch trên `_provider_type`: `gemini`/`minimax`/`byteplus` native, **default → `callChatVideoGen`** (create_video_chat.go) → `POST {base}/chat/completions` + `modalities:["video"]` → Agnes 400/422 "Use /v1/videos". **Đây là root cause.**
- Timeout per provider: `media_provider_chain.go:209` — `WithTimeout(ctx, entry.Timeout)` default 600s (lines 24,49-50).
- Chain form UI: `media-provider-chain-form.tsx:42` dùng `useProviders()` → list động → provider `agnes` tự hiển thị, KHÔNG cần đổi UI để chọn.

## Changes

### 1. Routing mới — `internal/tools/create_video.go`

Trước switch trong `callProvider` (create_video.go:212):

```go
if isAgnesVideoProvider(providerName, cp.APIBase()) {
    return t.callAgnesVideoGen(ctx, cp.APIKey(), cp.APIBase(), model, prompt, duration, aspectRatio, params)
}
```

`isAgnesVideoProvider(name, apiBase string) bool`:
- `strings.HasPrefix(name, "agnes")` — provider đặt tên `agnes`/`agnes-...` (test với httptest base URL được).
- `strings.Contains(strings.ToLower(apiBase), "agnes-ai.com")` — tự nhận ra dù đổi tên provider.

**KHÔNG** sửa `providerTypeFromName`/`ResolveProviderType` — shared với create_image/create_audio (rule: change scoped, không phá surface khác).

### 2. Chain defaults — `create_video.go:18,21-26`

```go
var videoGenProviderPriority = []string{"gemini", "minimax", "openrouter", "byteplus", "agnes"}
// map thêm:
"agnes": "agnes-video-2.5-flash",
```

Append cuối → deployment khác không có provider `agnes` không đổi behavior (buildDefaultChain skip).

### 3. Backend mới — `internal/tools/create_video_agnes.go`

Signature mirror anh em (create_video_gemini.go:20, create_video_chat.go:18):

```go
func (t *CreateVideoTool) callAgnesVideoGen(ctx context.Context, apiKey, apiBase, model, prompt string, duration int, aspectRatio string, params map[string]any) ([]byte, *providers.Usage, error)
```

Flow:
1. `image_base64 != ""` → `slog.Warn("create_video: image-to-video not supported by Agnes text mode...")` + bỏ qua (parity create_video_chat.go:19-21).
2. URL: `base := strings.TrimRight(apiBase, "/")`; create = `base` nếu đã endswith `/v1` + `/videos`, else `base + "/v1/videos"`; origin = `strings.TrimSuffix(base, "/v1")` → poll `origin + "/agnesapi?video_id=" + url.QueryEscape(id) + "&model_name=" + url.QueryEscape(model)`.
3. Body: `{"model", "prompt", "mode":"text", "seconds": strconv.Itoa(duration), "size": GetParamString(params,"size","720P"), "aspect_ratio", "n":1}`. Marshal lỗi → `%w`.
4. `POST` Bearer + `Content-Type: application/json`, `http.Client{}` (timeout do chain ctx — pattern create_video_chat.go:45). Non-200: parse `{"detail": "..."}` nếu có → error `agnes: HTTP 400: <detail>`; fallback body truncate (`truncateBytes(respBody, 500)`).
5. Parse create response → `video_id` (fallback `id`). Không có → error.
6. Poll loop (1.5s, `select` ctx.Done / `time.NewTicker`):
   - GET poll URL **không cần giữ gì thêm** nhưng gửi Bearer (cùng host gốc Agnes — key không đi sang host lạ).
   - `status == "completed"` → lấy `url` (top-level, bắt buộc non-empty) → break.
   - `status == "failed"` → error kèm `error.message` (parse nested; fallback "video generation failed").
   - HTTP 429 → sleep 3s, tiếp (ctx 600s tự chặn lặp vô hạn); 404/400 poll → error.
   - Cap safety: ~400 iterations.
7. Download: GET `url` **không gửi Authorization** (host bên thứ ba — N+key leak). Non-200 → error. Giới hạn dung lượng theo pattern cap download của `create_video_gemini.go` (dùng chung const/helper nếu có sẵn; nếu không, đặt const riêng `maxVideoDownloadBytes` hợp lý ≥ 20MB — video 12s 720P ~ vài MB).
8. Return `(bytes, nil, nil)` — Agnes không trả token usage.

### 4. Tests — `internal/tools/create_video_agnes_test.go`

Không cần registry: gọi thẳng `t.callAgnesVideoGen(ctx, "key", srv.URL+"/v1", "agnes-video-2.5-flash", "p", 8, "16:9", params)`.

- **Happy path:** httptest server route `POST /v1/videos` (trả `video_id`), `GET /agnesapi` (trả `completed` + `url`), `GET /files/v.mp4` (bytes mp4 giả). Assert: body create có `"seconds":"8"`, `"mode":"text"`, `"size":"720P"`, `"model"`, `aspect_ratio`; poll query có `video_id` + `model_name`; return == bytes.
- **Failed:** poll trả `failed` + `error.message` → error chứa message.
- **400 detail:** create trả 400 `{"detail":"size must be 720P"}` → error chứa detail.
- **`isAgnesVideoProvider` table:** `("agnes", "https://x/v1")` true; `("agnes-backup", "...")` true; `("custom", "https://apihub.agnes-ai.com/v1")` true; `("openai", "https://api.openai.com/v1")` false; `("agnes", "")` true…
- Follow pattern test hiện có (httptest, không network thật) — xem `create_image_minimax_test.go` / test文件 tương đương trong package nếu cần helper.

### 5. Web UI (parity — verify then minimal)

- Chain form: đã list động → **không đổi**.
- `ui/web/src/pages/builtin-tools/media-provider-params-schema.ts:77-122` (`create_video`): key hiện là `minimax_native`/`gemini_native` (DB provider_type). Verify keying bằng `media-provider-chain-helpers.ts`/`media-sortable-provider-card.tsx`: nếu key theo provider DB type → thêm `openai_compat: [{key:"size", label:"Size", type:"select", default:"720P", options: 720P/1080P/1K/2K}]` vào `create_video`. Nếu keying không hỗ trợ clean → BỎ qua, ghi lý do surface-parity N/A trong implement.md (không force).
- Ràng buộc Flash: option 720P default; user tự biết non-720P 400 với flash (không cần validation thêm — server trả lỗi rõ).

### 6. Docs — `docs/deployment-guide.md`

Section Agnes: thêm bullet/video note — video generation qua tool `create_video` (không phải model chat), model mặc định `agnes-video-2.5-flash` (đang free promo, 720P cố định), chain cấu hình ở Builtin Tools → create_video.

## Security notes

- Key chỉ gửi: (a) `{origin}/videos`, (b) `{origin}/agnesapi` — đều host Agnes; (c) KHÔNG gửi cho `url` download.
- Không log key; slog chỉ log provider/model/status.

## Out of scope

- Image-to-video (keyframe) + reference mode — cần public URL hosting, để sau.
- Đổi `ResolveProviderType` hay routing của create_image/create_audio.
- Backend `/v1/videos` qua provider khác (Gemini/Minimax đã có native).
