package manifest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"text/tabwriter"

	// crypto libraries included for go-digest
	_ "crypto/sha256"
	_ "crypto/sha512"

	digest "github.com/opencontainers/go-digest"
	"github.com/regclient/regclient/internal/units"
	"github.com/regclient/regclient/internal/wraperr"
	"github.com/regclient/regclient/types"
	"github.com/regclient/regclient/types/docker/schema2"
	"github.com/regclient/regclient/types/platform"
)

const (
	// MediaTypeDocker2Manifest is the media type when pulling manifests from a v2 registry
	MediaTypeDocker2Manifest = types.MediaTypeDocker2Manifest
	// MediaTypeDocker2ManifestList is the media type when pulling a manifest list from a v2 registry
	MediaTypeDocker2ManifestList = types.MediaTypeDocker2ManifestList
)

type docker2Manifest struct {
	common
	schema2.Manifest
}
type docker2ManifestList struct {
	common
	schema2.ManifestList
}

func (m *docker2Manifest) GetConfig() (types.Descriptor, error) {
	return m.Config, nil
}
func (m *docker2Manifest) GetConfigDigest() (digest.Digest, error) {
	return m.Config.Digest, nil
}
func (m *docker2ManifestList) GetConfig() (types.Descriptor, error) {
	return types.Descriptor{}, wraperr.New(fmt.Errorf("config digest not available for media type %s", m.desc.MediaType), types.ErrUnsupportedMediaType)
}
func (m *docker2ManifestList) GetConfigDigest() (digest.Digest, error) {
	return "", wraperr.New(fmt.Errorf("config digest not available for media type %s", m.desc.MediaType), types.ErrUnsupportedMediaType)
}

func (m *docker2Manifest) GetManifestList() ([]types.Descriptor, error) {
	return []types.Descriptor{}, wraperr.New(fmt.Errorf("platform descriptor list not available for media type %s", m.desc.MediaType), types.ErrUnsupportedMediaType)
}
func (m *docker2ManifestList) GetManifestList() ([]types.Descriptor, error) {
	return m.Manifests, nil
}

func (m *docker2Manifest) GetLayers() ([]types.Descriptor, error) {
	return m.Layers, nil
}
func (m *docker2ManifestList) GetLayers() ([]types.Descriptor, error) {
	return []types.Descriptor{}, wraperr.New(fmt.Errorf("layers are not available for media type %s", m.desc.MediaType), types.ErrUnsupportedMediaType)
}

func (m *docker2Manifest) GetOrig() interface{} {
	return m.Manifest
}
func (m *docker2ManifestList) GetOrig() interface{} {
	return m.ManifestList
}

func (m *docker2Manifest) GetPlatformDesc(p *platform.Platform) (*types.Descriptor, error) {
	return nil, wraperr.New(fmt.Errorf("platform lookup not available for media type %s", m.desc.MediaType), types.ErrUnsupportedMediaType)
}
func (m *docker2ManifestList) GetPlatformDesc(p *platform.Platform) (*types.Descriptor, error) {
	return getPlatformDesc(p, m.Manifests)
}

func (m *docker2Manifest) GetPlatformList() ([]*platform.Platform, error) {
	return nil, wraperr.New(fmt.Errorf("platform list not available for media type %s", m.desc.MediaType), types.ErrUnsupportedMediaType)
}
func (m *docker2ManifestList) GetPlatformList() ([]*platform.Platform, error) {
	dl, err := m.GetManifestList()
	if err != nil {
		return nil, err
	}
	return getPlatformList(dl)
}

func (m *docker2Manifest) MarshalJSON() ([]byte, error) {
	if !m.manifSet {
		return []byte{}, wraperr.New(fmt.Errorf("manifest unavailable, perform a ManifestGet first"), types.ErrUnavailable)
	}

	if len(m.rawBody) > 0 {
		return m.rawBody, nil
	}

	return json.Marshal((m.Manifest))
}
func (m *docker2ManifestList) MarshalJSON() ([]byte, error) {
	if !m.manifSet {
		return []byte{}, wraperr.New(fmt.Errorf("manifest unavailable, perform a ManifestGet first"), types.ErrUnavailable)
	}

	if len(m.rawBody) > 0 {
		return m.rawBody, nil
	}

	return json.Marshal((m.ManifestList))
}

func (m *docker2Manifest) MarshalPretty() ([]byte, error) {
	if m == nil {
		return []byte{}, nil
	}
	buf := &bytes.Buffer{}
	tw := tabwriter.NewWriter(buf, 0, 0, 1, ' ', 0)
	if m.r.Reference != "" {
		fmt.Fprintf(tw, "Name:\t%s\n", m.r.Reference)
	}
	fmt.Fprintf(tw, "MediaType:\t%s\n", m.desc.MediaType)
	fmt.Fprintf(tw, "Digest:\t%s\n", m.desc.Digest.String())
	var total int64
	for _, d := range m.Layers {
		total += d.Size
	}
	fmt.Fprintf(tw, "Total Size:\t%s\n", units.HumanSize(float64(total)))
	fmt.Fprintf(tw, "\t\n")
	fmt.Fprintf(tw, "Config:\t\n")
	err := m.Config.MarshalPrettyTW(tw, "  ")
	if err != nil {
		return []byte{}, err
	}
	fmt.Fprintf(tw, "\t\n")
	fmt.Fprintf(tw, "Layers:\t\n")
	for _, d := range m.Layers {
		fmt.Fprintf(tw, "\t\n")
		err := d.MarshalPrettyTW(tw, "  ")
		if err != nil {
			return []byte{}, err
		}
	}
	tw.Flush()
	return buf.Bytes(), nil
}
func (m *docker2ManifestList) MarshalPretty() ([]byte, error) {
	if m == nil {
		return []byte{}, nil
	}
	buf := &bytes.Buffer{}
	tw := tabwriter.NewWriter(buf, 0, 0, 1, ' ', 0)
	if m.r.Reference != "" {
		fmt.Fprintf(tw, "Name:\t%s\n", m.r.Reference)
	}
	fmt.Fprintf(tw, "MediaType:\t%s\n", m.desc.MediaType)
	fmt.Fprintf(tw, "Digest:\t%s\n", m.desc.Digest.String())
	fmt.Fprintf(tw, "\t\n")
	fmt.Fprintf(tw, "Manifests:\t\n")
	for _, d := range m.Manifests {
		fmt.Fprintf(tw, "\t\n")
		dRef := m.r
		if dRef.Reference != "" {
			dRef.Digest = d.Digest.String()
			fmt.Fprintf(tw, "  Name:\t%s\n", dRef.CommonName())
		}
		err := d.MarshalPrettyTW(tw, "  ")
		if err != nil {
			return []byte{}, err
		}
	}
	tw.Flush()
	return buf.Bytes(), nil
}

func (m *docker2Manifest) SetOrig(origIn interface{}) error {
	orig, ok := origIn.(schema2.Manifest)
	if !ok {
		return types.ErrUnsupportedMediaType
	}
	if orig.MediaType != types.MediaTypeDocker2Manifest {
		// TODO: error?
		orig.MediaType = types.MediaTypeDocker2Manifest
	}
	mj, err := json.Marshal(orig)
	if err != nil {
		return err
	}
	m.manifSet = true
	m.rawBody = mj
	m.desc = types.Descriptor{
		MediaType: types.MediaTypeDocker2Manifest,
		Digest:    digest.FromBytes(mj),
		Size:      int64(len(mj)),
	}
	m.Manifest = orig

	return nil
}

func (m *docker2ManifestList) SetOrig(origIn interface{}) error {
	orig, ok := origIn.(schema2.ManifestList)
	if !ok {
		return types.ErrUnsupportedMediaType
	}
	if orig.MediaType != types.MediaTypeDocker2ManifestList {
		// TODO: error?
		orig.MediaType = types.MediaTypeDocker2ManifestList
	}
	mj, err := json.Marshal(orig)
	if err != nil {
		return err
	}
	m.manifSet = true
	m.rawBody = mj
	m.desc = types.Descriptor{
		MediaType: types.MediaTypeDocker2ManifestList,
		Digest:    digest.FromBytes(mj),
		Size:      int64(len(mj)),
	}
	m.ManifestList = orig

	return nil
}
