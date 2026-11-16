// Package cell is the root of the module that replicates the kernel of one
// cell: several replicas run the kernel program, and they agree on every
// input and on every state through CometBFT. docs/replication.md describes
// it.
//
// The module is apart from the root module because it has a dependency, and
// the root module has none. Its packages are app (the kernel as an
// application of CometBFT), node (one replica), client (inputs, verdicts,
// and the certificate of a verdict), and cmd/cell (the command).
package cell
