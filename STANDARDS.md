# Standards

The judgement calls the check can't enforce, one line each, for the standards-reviewer. What
`mise run check` enforces is never restated here.

The architecture pages that apply, in the architecture repository:

- `principles/minimal-footprint.md`
- `principles/composition-root.md`
- `principles/validation-first.md`
- `principles/service-tiers.md`
- `principles/rolling-currency.md`
- `standards/go-elemental/principles/topology-and-naming.md`
- `standards/go-elemental/principles/dependencies.md`
- `standards/go-elemental/principles/baseline-standards.md`
- `standards/go-elemental/principles/tests-and-docs.md`

## The client

- The library ships no policy: timeouts, retries, and limits are the caller's values.
- A wire format is chosen by typed constructor at the composition root, never from a registry.
- A native engine parameter goes through the request's raw-JSON extension field, documented as
  native tier, never as a typed field of the standard model.
