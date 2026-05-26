# Supasend to GitHub

Webhook that turns Supasend captures into Markdown files in a GitHub repo.

Flow:

```text
Supasend -> this service -> GitHub Git API -> one commit -> Obsidian Git sync
```

It writes:

```text
Inbox/Quick Capture/<created_at>.md
Attachments/Supasend/<created_at>-<filename>
```

If a target path already exists, the service writes the next free name:

```text
Inbox/Quick Capture/2026-05-26T10-00-00-1.md
```

## Config

Required:

```sh
GITHUB_TOKEN=
GITHUB_OWNER=
GITHUB_REPO=
WEBHOOK_TOKEN=
```

Optional:

```sh
GITHUB_BRANCH=main
GITHUB_API_URL=https://api.github.com
LISTEN_ADDR=:8080
NOTE_DIR="Inbox/Quick Capture"
ATTACHMENT_DIR="Attachments/Supasend"
MAX_ATTACHMENT_BYTES=26214400
```

## Run

```sh
task test
go run ./cmd/server
```

Webhook:

```sh
curl -X POST http://localhost:8080/webhooks/supasend \
  -H "Authorization: Bearer $WEBHOOK_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"text":"hello","created_at":"2026-05-26T10:00:00Z"}'
```

Supasend attachment payload:

```json
{
  "text": "receipt",
  "shared_url": "https://example.com/receipt.jpg",
  "created_at": "2026-05-26T10:00:00Z"
}
```

Custom file endpoint:

```sh
curl -X POST http://localhost:8080/webhooks/file \
  -H "Authorization: Bearer $WEBHOOK_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"text":"receipt","file_name":"receipt.txt","file":"aGVsbG8K"}'
```

Response body:

```json
{
  "ok": true
}
```

Failure response body:

```json
{
  "ok": false
}
```
