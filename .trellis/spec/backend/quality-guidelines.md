# Quality Guidelines

> Code quality standards for backend development.

---

## Overview

Quality standards for the GoClaw backend (`internal/`): routing conventions, auth boundaries for media providers, and gate commands. Sections without content are not yet codified.

---

## Forbidden Patterns

### Don't: extend shared `providerTypeFromName` / `ResolveProviderType` for a single tool's provider family

**Problem**: `providerTypeFromName` and `ResolveProviderType` are shared by `create_video`, `create_image`, `create_audio`. Adding a name/host mapping there silently changes routing for ALL media tools.

**Instead**: scope the dispatch inside the specific tool's `callProvider` with an early return (see "Media provider chain backend routing" below).

---

## Required Patterns

### Scenario: Media provider chain backend routing (create_video / create_image / create_audio)

#### 1. Scope / Trigger
- Trigger: adding a new backend for a media tool when the provider's DB type is **generic** (e.g. `openai_compat`) and the existing `default:` branch would mis-route it — e.g. Agnes exposes video via async `POST /v1/videos`, but the default chat backend sends `POST /chat/completions` with `modalities:["video"]` → provider rejects with "Use /v1/videos".
- Also applies when the provider needs an auth boundary the default path doesn't have (vendor-host-only API key).

#### 2. Signatures
```go
// Tool-owned dispatch (internal/tools/create_video.go)
func (t *CreateVideoTool) callProvider(ctx context.Context, cp credentialProvider,
    providerName, model string, params map[string]any) ([]byte, *providers.Usage, error)
// → early return BEFORE the _provider_type switch:
//   if isAgnesVideoProvider(providerName, cp.APIBase()) { ... }

// Detection as a pure function (testable, no network):
func isAgnesVideoProvider(name, apiBase string) bool  // HasPrefix(name,"agnes") || Contains(lower(apiBase),"agnes-ai.com")

// Backend funcs mirror siblings:
func (t *CreateVideoTool) callAgnesVideoGen(ctx, apiKey, apiBase, model, prompt string,
    duration int, aspectRatio string, params map[string]any) ([]byte, *providers.Usage, error)

// Chain defaults (append-only):
var videoGenProviderPriority = []string{"gemini", "minimax", "openrouter", "byteplus", "agnes"}
var videoGenModelDefaults = map[string]string{ ..., "agnes": "agnes-video-2.5-flash" }
```

#### 3. Contracts
- `ExecuteWithChain` injects `callParams["_provider_type"] = resolvedType` (`media_provider_chain.go:202`) — for `openai_compat` providers this lands in the chat default branch; hence dispatch must happen BEFORE the switch.
- Default chain (`buildDefaultChain`) only includes providers that `registry.Get()` resolves — appending an unregistered name to `videoGenProviderPriority` is a no-op for other deployments.
- Universal params injected per chain entry: `prompt`, `duration`, `aspect_ratio`, `image_base64`/`image_mime`.
- Per-provider timeout = `entry.Timeout` (default **600s**, `media_provider_chain.go:24,49-50,209`) — poll loops must honor `ctx.Done()`, no separate timeout needed.
- **Auth boundary (critical)**: API key `Authorization: Bearer` goes ONLY to the vendor's own endpoints (create + poll). The result/download URL (third-party host) is fetched with a **plain GET, never with the key**. No key in `slog` calls.
- Download size cap: reuse `limitedReadAll(resp.Body, maxMediaDownloadBytes)` (`media_provider_chain.go:257-270`, 200 MB, errors when exceeded).
- **UI params schema keying**: `ui/web/src/pages/builtin-tools/media-provider-params-schema.ts` is keyed by the provider's **DB `provider_type`** (lookups: `media-sortable-provider-card.tsx:76`, `media-provider-chain-helpers.ts:26`). A generic type (e.g. `openai_compat`) entry shows for all providers of that type — acceptable because backends ignore unknown params. Chain provider dropdown is dynamic via `useProviders()` — never hardcode provider lists.

#### 4. Validation & Error Matrix
| Condition | Behavior |
|---|---|
| Create HTTP 400/4xx | parse `{"detail":"..."}` if present → error string includes detail; fallback truncated body (500 B) |
| Async status `failed` | error includes nested `error.message` |
| HTTP 429 (poll) | ctx-aware backoff (~3s), continue; bounded by ctx |
| Other non-200 (poll) | error (`<scope>: poll HTTP <code>`) |
| `ctx` deadline (600s) | poll loop returns ctx error → chain moves to next provider / fails fast |
| Any backend error | `ExecuteWithChain` tries next chain entry (MaxRetries per entry) |

#### 5. Good/Base/Bad Cases
- **Good**: provider named `agnes` with test base URL `httptest` → name-prefix detection routes correctly (host check alone would fail in tests).
- **Base**: provider renamed (`my-agnes`, base `https://apihub.agnes-ai.com/v1`) → host detection still routes.
- **Bad**: provider named `agnes` pointing at OpenAI base → routed to Agnes backend by name (accepted tradeoff: prefix ownership beats base sniffing; document provider naming).

#### 6. Tests Required
- Backend test calls the backend func **directly** with `httptest` (no registry, no real network): assert (a) request body fields (strings like `seconds` stay strings, `mode`, `size` default), (b) poll query params present, (c) returned bytes equal served bytes, (d) `Authorization` header **present** on vendor calls and **absent** on the result download, (e) failed-status and 4xx-detail error substrings.
- Pure detection func: table test with ≥2 negative cases.
- Gate commands: `go build ./...`, `go build -tags sqliteonly ./...` (tools package is shared with desktop), `go vet ./internal/tools/`, targeted `go test ./internal/tools/ -run '<Family>' -count=1`.

#### 7. Wrong vs Correct
##### Wrong
```go
// in shared providerTypeFromName (breaks create_image/create_audio routing):
case strings.HasPrefix(name, "agnes"): return "agnes"
```
##### Correct
```go
// in CreateVideoTool.callProvider only — before the _provider_type switch:
if isAgnesVideoProvider(providerName, cp.APIBase()) {
    return t.callAgnesVideoGen(ctx, cp.APIKey(), cp.APIBase(), model, prompt, duration, aspectRatio, params)
}
```

---

## Testing Requirements

- Package-level gates before merge: `go build ./...`, `go build -tags sqliteonly ./...`, `go vet`, targeted `go test -run`.
- **Windows gotcha (pre-existing)**: `internal/tools` test package does NOT compile on Windows — `document_parser_test.go:194` calls `waitForRecordedPIDs`/`findLivePIDs` defined in `shell_abort_test.go` behind `//go:build !windows`. `-run` filters cannot bypass a compile failure. Workaround to run vet/tests: temporarily move `document_parser_test.go` aside → run gates → **restore via `git checkout -- internal/tools/document_parser_test.go`** and verify `git status` clean of it. Do NOT "fix" the build tags in unrelated tasks.
- Full-package `go test ./internal/tools/` also fails on this + related pre-existing Windows issues — use `-run` filters and record known failures instead of claiming full green.
- Repo-wide `gofmt -l` noise (285+ files) is pre-existing `core.autocrlf=true` — only check `gofmt -l` on files you touched.

---

## Code Review Checklist

<!-- What reviewers should check -->

- Shared resolvers (`providerTypeFromName`, `ResolveProviderType`) unchanged unless the change is intentionally cross-tool.
- API key never sent to a host other than the provider's own; never logged.
- Media download path enforces a size cap.
- Chain default lists appended, not reordered (append-only preserves other deployments' behavior).
