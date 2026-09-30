// Package kind embeds the kind cluster configuration used by `lucid cluster up`.
package kind

import _ "embed"

// Config is the kind cluster config: one control-plane node.
//
//go:embed cluster.yaml
var Config []byte
