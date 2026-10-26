//go:build integration

// Package integration is the client's integration tier: tests behind the integration build tag
// that drive the exported API black-box against the llama.cpp router SPIKE_ROUTER_URL names,
// gb10-2's serving set A by default. `mise run integration` runs them; the hermetic check builds
// and lints them but never runs them. Every request carries autoload=false, so a test fails
// rather than load a model set A hasn't loaded.
package integration
