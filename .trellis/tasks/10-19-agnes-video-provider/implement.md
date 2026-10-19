# Implement plan — 10-19-agnes-video-provider

## Steps

- [x] **0. Context:** đọc `research/agnes-video-api.md` (API contract grounded) → `prd.md` → `design.md`.
- [x] **1. Backend:** tạo `internal/tools/create_video_agnes.go` — `callAgnesVideoGen` + `isAgnesVideoProvider` theo design (create→poll→download, size default 720P, seconds string, warn image_to_video, no-auth download, 429 backoff).
- [x] **2. Wire:** `internal/tools/create_video.go` — early dispatch trước switch (callProvider) + append `"agnes"` vào `videoGenProviderPriority` + `videoGenModelDefaults["agnes"]="agnes-video-2.5-flash"`. KHÔNG sửa shared `providerTypeFromName`/`ResolveProviderType`.
- [x] **3. Tests:** `internal/tools/create_video_agnes_test.go` — happy path / failed / 400 detail / `isAgnesVideoProvider` table. Không network thật.
- [x] **4. UI parity (verify first):** check keying của `media-provider-params-schema.ts` (`minimax_native` = DB provider_type?); nếu match convention → thêm entry `openai_compat` (size select) vào `create_video`; nếu không → ghi "Surface parity: Web UI params N/A vì ..." trong report. KHÔNG sửa locale i18n (không thêm key mới — label "Size" đã có pattern).
- [x] **5. Docs:** `docs/deployment-guide.md` section Agnes → thêm video note (model default, free promo, 720P, chain config path).
- [x] **6. Gates:**
  ```bash
  go build ./...
  go build -tags sqliteonly ./...
  go vet ./internal/tools/
  go test ./internal/tools/ -run 'Agnes|Video' -count=1
  ```
  Nếu đụng `ui/web` → `cd ui/web && pnpm build`.
  Known pre-existing: `internal/tools/document_parser_test.go:194` fail full-package — không phải của task này, không sửa trong task này.
- [x] **7. Report:** ghi kết quả vào cuối file này (items done, gates output tóm tắt, surface parity statement, files changed).

## Notes

- Package shared server/desktop → build cả 2 tag là bắt buộc.
- Coding conventions: `errors.Is`, switch/case, handle mọi error, append(dst, src...), không comment thừa.
- NO git commit (trellis-implement rule) — main session sẽ commit sau check.

## Report

### Files changed

| File | Change |
|---|---|
| `internal/tools/create_video_agnes.go` | NEW — `isAgnesVideoProvider`, `callAgnesVideoGen` (create `POST {base}/videos` mode `text`, `seconds` string, `size` default `720P`, poll `GET {origin}/agnesapi?video_id=..&model_name=..` every 1.5s, 429 → 3s backoff, 400 → `{"detail"}` in error, failed → `error.message`, download without Authorization, `limitedReadAll(..., maxMediaDownloadBytes)`, image_base64 → `slog.Warn` + ignore) |
| `internal/tools/create_video.go` | Wire: early Agnes dispatch in `callProvider` before the `_provider_type` switch; `"agnes"` appended to `videoGenProviderPriority`; `videoGenModelDefaults["agnes"] = "agnes-video-2.5-flash"`. Shared `providerTypeFromName` / `ResolveProviderType` untouched |
| `internal/tools/create_video_agnes_test.go` | NEW — happy path (create body `seconds:"8"`/`mode:"text"`/`size:"720P"`/`model`/`aspect_ratio` + poll query `video_id`+`model_name` + bytes equal + Bearer on create/poll + **no** Authorization on download), failed status → error contains message, create 400 → error contains detail, `isAgnesVideoProvider` table (8 cases incl. negatives) |
| `ui/web/src/pages/builtin-tools/media-provider-params-schema.ts` | Added `openai_compat` entry to `create_video`: `size` select (`720P` default, options `720P/1080P/1K/2K`), matching the existing `minimax_native` entry shape |
| `docs/deployment-guide.md` | Agnes section: video via `create_video` tool (not chat model), default `agnes-video-2.5-flash` (free promo, fixed 720P), `agnes-video-2.5` $0.025/s at 720P, chain path Builtin Tools → create_video |

### Gates

| Command | Result |
|---|---|
| `go build ./...` | PASS |
| `go build -tags sqliteonly ./...` | PASS |
| `go vet ./internal/tools/` | PASS with caveat — see deviation ① |
| `go test ./internal/tools/ -run 'Agnes\|Video' -count=1` | PASS — all 4 new Agnes tests + all other Video-matched tests green (see caveat ① for how it was run) |
| `cd ui/web && pnpm build` | PASS (`tsc -b && vite build`; `pnpm install` first — node_modules was absent) |
| `gofmt` on new files | PASS (LF, clean) |

### Surface parity

- **Backend:** changed — new Agnes video backend + chain routing/defaults (PG server + `-tags sqliteonly` desktop both compile; `internal/tools` is shared).
- **Web UI:** changed — keying of `media-provider-params-schema.ts` verified against live code: `media-sortable-provider-card.tsx:76` and `media-provider-chain-helpers.ts:26` both look up by `selectedProvider.provider_type` (DB provider_type). Agnes's DB type is `openai_compat`, so the `openai_compat` entry was added (convention match confirmed). The chain provider dropdown needed NO change (dynamic via `useProviders()`). No new i18n keys (label `"Size"` reuses the existing no-`labelKey` pattern).
- **CLI:** N/A — no CLI surface consumes media-provider chain config or video generation routing.

### Deviations from design.md

1. **`go vet` / `go test` on Windows require a temporary workaround for a pre-existing issue.** `document_parser_test.go:194` references `waitForRecordedPIDs`/`findLivePIDs`, which live in `shell_abort_test.go` (`//go:build !windows`) → on Windows the whole `internal/tools` **test package fails to compile**, so even `-run`-filtered `go test` cannot build. This is the same Windows test-file build-tag issue already documented in archived task `10-15-neon-koyeb` (and the task's known pre-existing failure list). To verify gates without touching the pre-existing file, `document_parser_test.go` was temporarily moved aside → `go vet ./internal/tools/` PASS, `go test -run 'Agnes|Video' -count=1` PASS (175s, all green incl. `TestCallAgnesVideoGen_*` ×3 and `TestIsAgnesVideoProvider`) → file restored via `git checkout` (working tree verified clean of it). The pre-existing file was **not** modified, per task constraints.
2. **Poll timing**: first poll also waits 1.5s (matches the byteplus/gemini "wait-then-poll" pattern) rather than polling immediately — costs ≤1.5s extra latency, keeps code consistent with siblings.
3. **Error prefixes**: implemented as `agnes: create HTTP 400: <detail>` / `agnes: poll HTTP 404: ...` / `agnes: video generation failed: <message>` via a shared `agnesAPIError` helper — design specified `agnes: HTTP 400: <detail>`; scope word added for debuggability (test asserts only on the detail substring).
4. **Download cap**: reused the shared `maxMediaDownloadBytes` (200 MB, `media_provider_chain.go:257`) + `limitedReadAll` — the pattern used by gemini/byteplus/minimax backends, satisfying the design's "reuse existing cap" preference (no new const needed).
5. **429 backoff on create**: implemented only on poll (design step 4 says non-200 create → error; step 6 puts 429 backoff in the poll loop). Chain retry (`MaxRetries`) covers transient create failures.

No blockers. No git commit performed (per trellis-implement rule).
