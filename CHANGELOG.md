# Changelog

All notable changes to `github.com/JaimeStill/spike-ai-pipeline-client` are documented here. The
format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the module adheres to
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- The CI pair: a check workflow on every pull request into and push to main, and a release
  workflow that publishes a pushed `v*` tag's section of this changelog.
- Text chat, in four packages importing one way. `protocol` is the model: a `Request` of Open
  Responses message items (spec 2026-04-24, stateless subset) and a `Response` with output
  items, status, incomplete details, and usage, with `Request.Validate` for the spec's rules. A
  request's `Native` raw-JSON object, the native tier, is merged into the wire body's top level,
  and a member that clashes with a typed field is refused. `format` holds the `Format` contract.
  The module root, `client`, is the transport: `client.New` builds a `Client` from a validated
  `Config` (base URL, query, token source, the caller's `http.Client`, and a required
  `format.Format`), `Client.Generate` validates and sends a `protocol.Request`, and a non-2xx
  reply is an `*APIError`.
- `format/completions`, the Chat Completions format (`/chat/completions`): instructions and
  message items encoded as messages, `max_output_tokens` sent as `max_completion_tokens`, and
  the reply's message, `finish_reason`, and usage decoded into the response.

[Unreleased]: https://github.com/JaimeStill/spike-ai-pipeline-client/commits/main
