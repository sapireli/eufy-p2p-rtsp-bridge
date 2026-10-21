package pipeline

// TileConfig is the private per-tile decoder configuration consumed by eufytile.
type TileConfig struct {
	Path     string
	Contents []byte
}
