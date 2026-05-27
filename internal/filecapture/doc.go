// Package filecapture validates payloads for the custom /webhooks/file endpoint.
//
// Custom file captures are used by Apple iOS Shortcuts and may arrive as separate
// note and attachment requests. Every request must include a folder name so files
// created by one automation run can be grouped without relying on ambiguous file
// name suffixes.
package filecapture
