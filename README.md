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

Supasend captures keep the Supasend flow.

Custom `/webhooks/file` captures require `folder_name`. Obsidian Quick Capture can
save a note and an attachment with the same filename in the same folder, so the
service cannot safely infer which files belong to one capture from filenames
alone. Use the Apple iOS Shortcut automation start time as `folder_name`.

Custom notes and attachments are written into the same folder:

```text
<NOTE_DIR>/<folder_name>/<file_name>
<NOTE_DIR>/<folder_name>/<attachment_name>
```

Note filenames must end with `.md`.

If a target file already exists, the service returns `409` with
`{"ok": false, "error": "reason"}`. It does not create suffixes.

Created notes include `created_at` in Obsidian properties as
`YYYY-MM-DD HH:MM:SS`, preserving the wall-clock time from the sent timestamp.
If the note text already has frontmatter with `created_at`, the service keeps
the existing value.

## Supasend Endpoint

`POST /webhooks/supasend`

```json
{
  "text": "receipt",
  "shared_url": "https://example.com/receipt.jpg",
  "created_at": "2026-05-26T10:00:00Z"
}
```

`text` and `created_at` are required. `shared_url` is optional. `created_at`
must be RFC3339 / ISO 8601, for example `2026-05-26T10:00:00Z`.

## Custom File Endpoint

`POST /webhooks/file`

Note:

```json
{
  "folder_name": "2026-05-27T10-00-00",
  "created_at": "2026-05-26T10:00:00Z",
  "text": "receipt",
  "file_name": "receipt.md"
}
```

Attachment:

```json
{
  "folder_name": "2026-05-27T10-00-00",
  "created_at": "2026-05-26T10:00:00Z",
  "attachment_name": "receipt.txt",
  "attachment": "aGVsbG8K"
}
```

Combined note and attachment:

```json
{
  "folder_name": "2026-05-27T10-00-00",
  "created_at": "2026-05-26T10:00:00Z",
  "text": "receipt",
  "file_name": "receipt.md",
  "attachment_name": "receipt.txt",
  "attachment": "aGVsbG8K"
}
```

`folder_name` and `created_at` are always required. `created_at` must be RFC3339
/ ISO 8601. Send either note fields, attachment fields, or both. Notes require
`text` and `file_name`; `file_name` must end with `.md`. Attachments require
`attachment_name` and `attachment`; `attachment_name` must include an extension.
`attachment` is base64 encoded file content.

## Response

Success:

```json
{"ok": true}
```

Failure:

```json
{"ok": false, "error": "reason"}
```
