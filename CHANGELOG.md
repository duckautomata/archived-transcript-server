# latest
Using version [1.6](#16-2026-02-26)

# Major version 1
Using version [1.6](#16-2026-02-26)

## 1.6 (2026-02-26)
Upgraded go version to 1.26.

## 1.5 (2026-01-25)
Accept gzip and zstd content encoding on POST /transcripts.
Switched back to alpine for reduced image size.
Simplified DB setup.
Added mem cache and db configs.

## 1.4 (2026-01-13)
Changed docker image to debian instead of alpine to fix random build issues.

## 1.3 (2026-01-05)
Improved database performance for cold starts.

## 1.2 (2025-12-20)
Added membership key and hides "Members" stream type behind it.
Added tests for the full app.
Added metrics for the new endpoint

## 1.1 (2025-11-22)
Fixed duplication when inserting existing transcript.

## 1.0 (2025-10-26)
Initial version.
