package schema2

import (
	"github.com/regclient/regclient/types"
	"github.com/regclient/regclient/types/docker"
)

// ManifestSchemaVersion is a pre-configured versioned field for manifests
var ManifestSchemaVersion = docker.Versioned{
	SchemaVersion: 2,
	MediaType:     types.MediaTypeDocker2Manifest,
}

// Manifest defines a schema2 manifest.
type Manifest struct {
	docker.Versioned

	// Config references the image configuration as a blob.
	Config types.Descriptor `json:"config"`

	// Layers lists descriptors for the layers referenced by the
	// configuration.
	Layers []types.Descriptor `json:"layers"`
}
