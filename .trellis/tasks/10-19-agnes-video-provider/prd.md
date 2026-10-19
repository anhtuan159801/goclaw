# Agnes video provider for create_video tool

## Goal

Cho phép provider `agnes` (openai_compat, base `https://apihub.agnes-ai.com/v1`) đã đăng ký hoạt động như một **video generation provider** của builtin tool `create_video`, để agent trên deployment chỉ có Agnes có thể sinh video mà không cần thêm tài khoản provider khác.

Lý do: hiện tại `create_video` dispatch theo `_provider_type=openai_compat` → backend `callChatVideoGen` gửi `POST /chat/completions` với `modalities:["video"]` → Agnes từ chối: *"Model agnes-video-2.5-flash is a video model. Use /v1/videos."*

## Requirements

- **R1 — Routing:** `create_video.callProvider` dispatch sang backend Agnes mới khi provider name có prefix `agnes` **HOẶC** API base chứa host `agnes-ai.com`. KHÔNG sửa `providerTypeFromName`/`ResolveProviderType` (shared với create_image/create_audio).
- **R2 — Async API:** Backend thực hiện `POST {base}/videos` (mode `text`) → lấy `video_id` → poll `GET {origin}/agnesapi?video_id=...&model_name=...` mỗi ~1.5s đến khi `completed`/`failed` → download `url` → trả bytes. Honor chain ctx (timeout 600s mặc định). Backoff khi 429.
- **R3 — Chain defaults:** Append `"agnes"` vào `videoGenProviderPriority` (cuối danh sách) + `videoGenModelDefaults["agnes"] = "agnes-video-2.5-flash"`. Provider chưa đăng ký vẫn bị `buildDefaultChain` bỏ qua (hành vi sẵn có).
- **R4 — Param mapping:** `prompt` ✓; `duration` {4,6,8} → `seconds` string ✓; `aspect_ratio` 16:9/9:16 ✓; `size` từ chain param, default `720P` (Flash chỉ chấp nhận 720P); `image_base64` → warn + bỏ qua (keyframe mode cần public URL).
- **R5 — Errors:** Lỗi Agnes (`error.message` khi failed, `detail` khi HTTP 400) hiển thị trong error message; lỗi → chain thử provider kế tiếp (hành vi `ExecuteWithChain` sẵn có).
- **R6 — Security:** Không lộ API key trong log; KHÔNG gửi header Authorization khi download `url` (host bên thứ ba).
- **R7 — Docs:** Cập nhật `docs/deployment-guide.md` (section Agnes): video qua `create_video`, model mặc định, pricing, cấu hình chain.

## Acceptance Criteria

- [ ] Unit tests: happy path (create body đúng `seconds`/`mode`/`size` + poll có `model_name` + download bytes), failed status → error chứa message, table test `isAgnesVideoProvider` (name prefix / base host / negative cases)
- [ ] `go build ./...` + `go build -tags sqliteonly ./...` + `go vet ./internal/tools/` PASS
- [ ] `go test ./internal/tools/ -run 'Agnes|Video' -count=1` PASS (failure `document_parser_test.go:194` là pre-existing, không phải của task này)
- [ ] Surface parity: Web UI chain form list provider động qua `useProviders()` → agnes selectable (không cần đổi); verify keying của `media-provider-params-schema.ts` rồi thêm entry `size` cho create_video nếu đúng convention; CLI N/A
- [ ] `docs/deployment-guide.md` cập nhật

## Constraints

- Package `internal/tools` shared giữa server (PG) và desktop (`-tags sqliteonly`) → compile cả hai tag.
- Flash model: `size` khác `720P` → HTTP 400 → default PHẢI là `720P`.
- Poll bị bound bởi chain timeout 600s (`media_provider_chain.go:49-50,209`) — không cần timeout riêng, nhưng cần cap/backoff hợp lý.
- Không dùng API key làm host khác khi download kết quả (SSRF/leak).
