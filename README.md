# Aegis v3

A clean rebuild of the Aegis Protocol: an accelerated, encrypted tunnel that gives every app on a phone (and later on laptops) a more stable, unblocked internet connection over weak or lossy mobile links.

**Status:** Phase 0 – review and analysis. There is no code yet, on purpose.

Read first: [`docs/00_REVIEW_AND_LANGUAGE_DECISION.md`](docs/00_REVIEW_AND_LANGUAGE_DECISION.md)

## Languages (decided)

| Part | Language |
|---|---|
| Networking core (shared by phone and relay) | **Go** |
| Cloud relay server | **Go** |
| Control plane (accounts, payments, tokens) | **Go** |
| Android app | **Kotlin** |
| Customer web portal (later) | TypeScript |
| iOS app (later) | Swift |

## Ground rules
- No secrets, keys or passwords are ever committed to this repository.
- Nothing is copied from v2 without being reviewed first (see the security notes in the review doc).
