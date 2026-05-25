# Overview
This is a small Go webhook service.
It receives Supasend captures and writes them to a GitHub repository in one Git commit.

# Base Policy Links (Load First)
- Router: https://github.com/RevoTale/agent-docs/blob/main/doc.md
- Common: https://github.com/RevoTale/agent-docs/blob/main/modules/common/doc.md
- Taskfile: https://github.com/RevoTale/agent-docs/blob/main/modules/taskfile/doc.md
- Go: https://github.com/RevoTale/agent-docs/blob/main/modules/go/doc.md
- Awesome index: https://github.com/RevoTale/agent-docs/blob/main/awesome/index.md
- Go awesome list: https://github.com/RevoTale/agent-docs/blob/main/awesome/go.md

# Local Details
- Use Taskfile for local workflows.
- Keep webhook auth in `Authorization: Bearer <WEBHOOK_TOKEN>`.
- Use GitHub Git Data API when a capture must write more than one file in one commit.
- Runtime configuration comes from environment variables.
- Do not add generated shell wrappers for environment setup unless explicitly requested.
