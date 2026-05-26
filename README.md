# Supasend to GitHub

Small Go webhook that writes Supasend captures to a GitHub repo in one commit.

```text
Supasend -> this service -> GitHub Git API -> Obsidian Git sync
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
MAX_ATTACHMENT_BYTES=26214400
```

Auth is always:

```text
Authorization: Bearer <WEBHOOK_TOKEN>
```

## Run

```sh
task test
go run ./cmd/server
```

## File Rules

Without attachment:

```text
<NOTE_DIR>/<file_name or created_at>.md
```

If the note exists, the service tries `-1`, `-2`, up to `-5` before `.md`.
If all names exist, it returns `409` with `{"ok": false, "error": "reason"}`.

With attachment:

```text
<NOTE_DIR>/<name>/<name>.md
<NOTE_DIR>/<name>/<name><attachment extension>
```

If the folder exists, the service tries folder suffixes `-1` through `-5`.
The note and attachment use the same final folder name, so links do not break.

Example duplicate:

```text
Inbox/Quick Capture/receipt-1/receipt-1.md
Inbox/Quick Capture/receipt-1/receipt-1.jpg
```

## Supasend Endpoint

`POST /webhooks/supasend`

```json
{
  "text": "receipt",
  "shared_url": "https://example.com/receipt.jpg",
  "created_at": "2026-05-26T10:00:00Z"
}
```

`text` is required. `shared_url`, `created_at`, and `due_date_utc` are optional.
If `created_at` is missing, the server time is used.

## Custom File Endpoint

`POST /webhooks/file`

```json
{
  "text": "receipt",
  "file_name": "receipt-note"
}
```

`text` is required. `file_name` is optional. `.md` may be included or omitted.

Optional attachment:

```json
{
  "text": "receipt",
  "file_name": "receipt-note",
  "attachment_name": "receipt.txt",
  "attachment": "aGVsbG8K",
  "attachment_content_type": "text/plain"
}
```

`attachment` is base64 encoded file content. If you send an attachment, send both
`attachment_name` and `attachment`.

## Response

Success:

```json
{"ok": true}
```

Failure:

```json
{"ok": false, "error": "reason"}
```
