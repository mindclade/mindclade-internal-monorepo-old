module github.com/Mindclade/mindclade-go

// PROVISIONAL. Set this when the SDK has an API, not before.
//
// 1.23.2 is not a support policy — it is the value the root module carried at this repository's
// first commit, inherited when sdk/go was reserved and never revisited. The files here are
// scaffold (`const scaffold_client`, `const scaffold_errors`); there is no code, no dependency,
// and no consumer, so there is nothing yet that requires any particular version.
//
// Two wrong ways to resolve it, both of which look like tidying:
//
//   Aligning it to the root module (currently 1.26.0). This module is PUBLIC — the directive is
//   a floor imposed on every external consumer. Raising it buys nothing while the package is
//   empty and costs anyone on an older toolchain.
//
//   Leaving it and assuming it is deliberate. It is not, and a number nobody chose will be read
//   as one that somebody did.
//
// The right moment is when API generation lands: pick the minimum the generated surface actually
// needs, and write the support window into README.md alongside it.
go 1.23.2
