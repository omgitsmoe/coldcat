# UI inspection artifact policy

Handoff screenshots are temporary inspection evidence, not repository assets. Keep them in
ignored Playwright outputs or preserve them externally before another suite clears outputs.
Commit text specifications, measurements and reproduction commands, not handoff-only images.

Images still needed as durable baseline references must use working Git LFS, with committed
attributes and verified LFS pointers/objects. Do not add regular Git binaries or claim an LFS
migration based only on attributes. Confirm LFS availability and remote object storage before
restoring a retained baseline. Never silently replace the original before-reference with a
capture of the redesigned application. This is artifact storage policy, not a UI architecture
decision; no ADR is required for this cleanup.

## 2026-10-11 inventory and preservation

All 60 tracked images were PNGs under `doc/ui-ux/`; no other tracked images were found.
Each listed scenario has captures at 1440, 1024 and 768 CSS pixels.

| Package | Classification | Scenarios | Images |
| --- | --- | --- | --- |
| U01 | Needed pre-redesign before-reference, not pixel goldens | content-history, directory-error, directory-history, disks, search, search-empty | 18 |
| U02 | Handoff-only inspection | content-history, controls, directory-error, directory-history, disks, search, search-empty | 21 |
| U03 | Handoff-only inspection | checking, ready, unavailable | 9 |
| U04 | Handoff-only inspection | capacity | 3 |
| U05 | Handoff-only inspection | compact, precision, subtotal | 9 |
| U06 | Already external handoff-only inspection | See U06 handoff | 0 tracked |

Before removal, all 60 original images and the U01–U06 text/manifests were copied to:

`/tmp/opencode/ui-ux-artifact-cleanup-20261011/`

The archive preserves original repository-relative paths. Its `inventory.json` enumerates every
image, classification, SHA-256, byte size and PNG dimensions; `SHA256SUMS` allows verification
with `sha256sum --check SHA256SUMS` from the archive root. Every copied image was byte-verified
before removal. U06's separate `/tmp/opencode/u06-screenshots/` remains unchanged.

**LFS unavailable:** `git lfs version` reports that `lfs` is not a Git command. No existing
`.gitattributes` was present. No system dependency was installed, no filters/hooks were
configured, and no LFS upload or migration is claimed. U01 remains needed, but its images are
external references rather than regular Git binaries until a working LFS setup is available.
The committed U01 and U03 manifests explicitly identify their external image roots.

These `/tmp/opencode` paths are environment-local, not durable public URLs or checkout assets.
Copy the archive to owner-managed storage before this environment is discarded if continued
access is required. Future U01 restoration requires Git LFS and owner-managed object storage;
regeneration from current UI is not a substitute for the original reference.

This cleanup removes images from the current tree only. Existing historical Git image blobs
remain; no history rewrite was requested or performed.

## Cleanup validation

- All 60 external images passed byte/SHA-256 and PNG-dimension verification; the archive's
  `sha256sum --check SHA256SUMS` passed for every image.
- All 18 U01 and nine U03 manifest records resolve to external files with matching widths;
  U01 full-page heights and U03 240px crop heights match their recorded capture contracts.
- All 16 local Markdown links in U01–U06, this policy and the parent UI plan resolve;
  none links to a removed PNG. JSON manifests parse successfully.
- The index contains no tracked images; all 60 removed paths match the new ignore rules.
- Working and staged diffs pass `git diff --check`. No application/build/browser/backend
  tests were rerun for this artifact-only cleanup; no catalogs or backend files were touched.
