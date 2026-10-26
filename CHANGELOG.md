# Changelog

All notable changes to `github.com/JaimeStill/spike-ai-pipeline-client` are documented here. The
format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the module adheres to
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- The CI pair: a check workflow on every pull request into and push to main, and a release
  workflow that publishes a pushed `v*` tag's section of this changelog.
- Text chat: `client.New` builds a `Client` from a validated `Config` (base URL, query, token
  source, the caller's `http.Client`, and a required `Format`); `Client.Respond` sends a
  `Request` of Open Responses message items (spec 2026-04-24, stateless subset) and returns a
  `Response` with output items, status, incomplete details, and usage; a non-2xx reply is an
  `*APIError`. A request's `Native` raw-JSON object, the native tier, is merged into the wire
  body's top level, and a member that clashes with a typed field is refused.
- `chatcompletions`, the Chat Completions format: instructions and message items folded into
  messages, `max_output_tokens` sent as `max_completion_tokens`, and the reply's message,
  `finish_reason`, and usage unfolded into the response.

[Unreleased]: https://github.com/JaimeStill/spike-ai-pipeline-client/commits/main
