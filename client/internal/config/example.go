package config

import _ "embed"

//go:embed example.yaml
var example []byte

// Example returns a copy of the versioned, validated configuration template.
func Example() []byte { return append([]byte(nil), example...) }
