// Package flagcommons holds vendored copies of the Go flag-commons packages
// BONNIE depends on. The upstream repository (github.com/flag-ai/commons) was
// rewritten in Python after v0.2.1, so these packages are maintained here.
//
// Provenance:
//   - Upstream: github.com/flag-ai/commons at v0.2.1 (commit f4c6e6b,
//     also tagged go-final-v0.2.1). The four packages below are byte-identical
//     between the previously pinned pseudo-version (7f85750) and v0.2.1.
//   - Vendored: secrets (all files), logging (logging.go only), health
//     (checker.go and registry.go only), version.
//   - Omitted on purpose: logging/context.go (unused), health/http.go
//     (unused) and health/database.go (unused, and vendoring it would keep
//     pgx in the dependency graph).
//
// Deviations from upstream, both in secrets/openbao.go and both without
// behavior change: http.NoBody instead of a nil request body, and named
// results on parseKey. Everything else differs only in import paths.
package flagcommons
