// Package release holds repository-level tests for the release pipeline:
// the GitHub Actions workflow (.github/workflows/release.yml) and the
// channel version guard (scripts/release_version_guard.py). It has no
// production code; the tests read those files relative to the repo root.
package release
