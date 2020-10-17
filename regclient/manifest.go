package regclient

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/url"

	"github.com/containerd/containerd/platforms"
	dockerDistribution "github.com/docker/distribution"
	dockerManifestList "github.com/docker/distribution/manifest/manifestlist"
	dockerSchema2 "github.com/docker/distribution/manifest/schema2"
	digest "github.com/opencontainers/go-digest"
	ociv1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/regclient/regclient/pkg/retryable"
	"github.com/sirupsen/logrus"
)

type manifest struct {
	digest   digest.Digest
	dockerM  dockerSchema2.Manifest
	dockerML dockerManifestList.ManifestList
	manifSet bool
	mt       string
	ociM     ociv1.Manifest
	ociML    ociv1.Index
	origByte []byte
}

// Manifest abstracts the various types of manifests that are supported
type Manifest interface {
	GetConfigDigest() (digest.Digest, error)
	GetDigest() digest.Digest
	GetDockerManifest() dockerSchema2.Manifest
	GetDockerManifestList() dockerManifestList.ManifestList
	GetLayers() ([]ociv1.Descriptor, error)
	GetMediaType() string
	GetPlatformDesc(p *ociv1.Platform) (*ociv1.Descriptor, error)
	GetPlatformList() ([]*ociv1.Platform, error)
	GetOCIManifest() ociv1.Manifest
	GetOCIManifestList() ociv1.Index
	GetOrigManifest() interface{}
	IsList() bool
	MarshalJSON() ([]byte, error)
}

func (m *manifest) GetConfigDigest() (digest.Digest, error) {
	switch m.mt {
	case MediaTypeDocker2Manifest:
		return m.dockerM.Config.Digest, nil
	case ociv1.MediaTypeImageManifest:
		return m.ociM.Config.Digest, nil
	}
	return "", ErrUnsupportedMediaType
}

func (m *manifest) GetDigest() digest.Digest {
	return m.digest
}

func (m *manifest) GetDockerManifest() dockerSchema2.Manifest {
	return m.dockerM
}

func (m *manifest) GetDockerManifestList() dockerManifestList.ManifestList {
	return m.dockerML
}

func (m *manifest) GetLayers() ([]ociv1.Descriptor, error) {
	switch m.mt {
	case MediaTypeDocker2Manifest:
		return d2oDescriptorList(m.dockerM.Layers), nil
	case ociv1.MediaTypeImageManifest:
		return m.ociM.Layers, nil
	}
	return []ociv1.Descriptor{}, ErrUnsupportedMediaType
}

func (m *manifest) GetMediaType() string {
	return m.mt
}

// GetPlatformDesc returns the descriptor for the platform from the manifest list or OCI index
func (m *manifest) GetPlatformDesc(p *ociv1.Platform) (*ociv1.Descriptor, error) {
	platformCmp := platforms.NewMatcher(*p)
	switch m.mt {
	case MediaTypeDocker2ManifestList:
		for _, d := range m.dockerML.Manifests {
			if platformCmp.Match(*dlp2Platform(d.Platform)) {
				return dl2oDescriptor(d), nil
			}
		}
	case MediaTypeOCI1ManifestList:
		for _, d := range m.ociML.Manifests {
			if platformCmp.Match(*d.Platform) {
				return &d, nil
			}
		}
	default:
		return nil, ErrUnsupportedMediaType
	}
	return nil, ErrNotFound
}

// GetPlatformList returns the list of platforms in a manifest list
func (m *manifest) GetPlatformList() ([]*ociv1.Platform, error) {
	var l []*ociv1.Platform
	if !m.manifSet {
		return l, ErrUnavailable
	}
	switch m.mt {
	case MediaTypeDocker2ManifestList:
		for _, d := range m.dockerML.Manifests {
			l = append(l, dlp2Platform(d.Platform))
		}
	case MediaTypeOCI1ManifestList:
		for _, d := range m.ociML.Manifests {
			l = append(l, d.Platform)
		}
	default:
		return nil, ErrUnsupportedMediaType
	}
	return l, nil
}

func (m *manifest) GetOCIManifest() ociv1.Manifest {
	return m.ociM
}

func (m *manifest) GetOCIManifestList() ociv1.Index {
	return m.ociML
}

func (m *manifest) GetOrigManifest() interface{} {
	if !m.manifSet {
		return nil
	}
	switch m.mt {
	case MediaTypeDocker2Manifest:
		return m.dockerM
	case MediaTypeDocker2ManifestList:
		return m.dockerML
	case MediaTypeOCI1Manifest:
		return m.ociM
	case MediaTypeOCI1ManifestList:
		return m.ociML
	default:
		return nil
	}
}

func (m *manifest) IsList() bool {
	switch m.mt {
	case MediaTypeDocker2ManifestList:
		return true
	case MediaTypeOCI1ManifestList:
		return true
	}
	return false
}

func (m *manifest) MarshalJSON() ([]byte, error) {
	if !m.manifSet {
		return []byte{}, ErrUnavailable
	}

	if len(m.origByte) > 0 {
		return m.origByte, nil
	}

	switch m.mt {
	case MediaTypeDocker2Manifest:
		return json.Marshal(m.dockerM)
	case MediaTypeDocker2ManifestList:
		return json.Marshal(m.dockerML)
	case MediaTypeOCI1Manifest:
		return json.Marshal(m.ociM)
	case MediaTypeOCI1ManifestList:
		return json.Marshal(m.ociML)
	}
	return []byte{}, ErrUnsupportedMediaType
}

func (rc *regClient) ManifestDelete(ctx context.Context, ref Ref) error {
	if ref.Digest == "" {
		return ErrMissingDigest
	}

	// build request
	host := rc.getHost(ref.Registry)
	manfURL := url.URL{
		Scheme: host.Scheme,
		Host:   host.DNS[0],
		Path:   "/v2/" + ref.Repository + "/manifests/" + ref.Digest,
	}
	opts := []retryable.OptsReq{}
	opts = append(opts, retryable.WithHeader("Accept", []string{
		MediaTypeDocker2Manifest,
		MediaTypeDocker2ManifestList,
		MediaTypeOCI1Manifest,
		MediaTypeOCI1ManifestList,
	}))

	// send the request
	rty := rc.getRetryable(host)
	resp, err := rty.DoRequest(ctx, "DELETE", manfURL, opts...)
	if err != nil {
		return err
	}

	// validate response
	if resp.HTTPResponse().StatusCode != 202 {
		body, _ := ioutil.ReadAll(resp)
		rc.log.WithFields(logrus.Fields{
			"ref":    ref.Reference,
			"status": resp.HTTPResponse().StatusCode,
			"body":   body,
		}).Warn("Unexpected status code for manifest delete")
		return fmt.Errorf("Unexpected status code on manifest delete %d\nResponse object: %v\nBody: %s", resp.HTTPResponse().StatusCode, resp, body)
	}

	return nil
}

/* func (rc *regClient) ManifestDigest(ctx context.Context, ref Ref) (digest.Digest, error) {
	var tagOrDigest string
	if ref.Digest != "" {
		tagOrDigest = ref.Digest
	} else if ref.Tag != "" {
		tagOrDigest = ref.Tag
	} else {
		rc.log.WithFields(logrus.Fields{
			"ref": ref.Reference,
		}).Warn("Manifest requires a tag or digest")
		return "", ErrMissingTagOrDigest
	}
	host := rc.getHost(ref.Registry)
	manfURL := url.URL{
		Scheme: host.Scheme,
		Host:   host.DNS[0],
		Path:   "/v2/" + ref.Repository + "/manifests/" + tagOrDigest,
	}

	opts := []retryable.OptsReq{}
	opts = append(opts, retryable.WithHeader("Accept", []string{
		MediaTypeDocker2Manifest,
		MediaTypeDocker2ManifestList,
		MediaTypeOCI1Manifest,
		MediaTypeOCI1ManifestList,
	}))

	rty := rc.getRetryable(host)
	resp, err := rty.DoRequest(ctx, "GET", manfURL, opts...)
	if err != nil {
		return "", err
	}
	respBody, err := ioutil.ReadAll(resp)
	if err != nil {
		rc.log.WithFields(logrus.Fields{
			"err": err,
			"ref": ref.Reference,
		}).Warn("Failed to read manifest")
		return "", err
	}
	return digest.FromBytes(respBody), nil
} */

func (rc *regClient) ManifestGet(ctx context.Context, ref Ref) (Manifest, error) {
	m := manifest{}

	// build the request
	host := rc.getHost(ref.Registry)
	var tagOrDigest string
	if ref.Digest != "" {
		tagOrDigest = ref.Digest
	} else if ref.Tag != "" {
		tagOrDigest = ref.Tag
	} else {
		rc.log.WithFields(logrus.Fields{
			"ref": ref.Reference,
		}).Warn("Manifest requires a tag or digest")
		return nil, ErrMissingTagOrDigest
	}

	manfURL := url.URL{
		Scheme: host.Scheme,
		Host:   host.DNS[0],
		Path:   "/v2/" + ref.Repository + "/manifests/" + tagOrDigest,
	}

	opts := []retryable.OptsReq{}
	opts = append(opts, retryable.WithHeader("Accept", []string{
		MediaTypeDocker2Manifest,
		MediaTypeDocker2ManifestList,
		MediaTypeOCI1Manifest,
		MediaTypeOCI1ManifestList,
	}))

	// send the request
	rty := rc.getRetryable(host)
	resp, err := rty.DoRequest(ctx, "GET", manfURL, opts...)
	if err != nil {
		return nil, err
	}
	if resp.HTTPResponse().StatusCode != 200 {
		return nil, fmt.Errorf("Unexpected http response code %d", resp.HTTPResponse().StatusCode)
	}

	// read manifest and compute digest
	digester := digest.Canonical.Digester()
	reader := io.TeeReader(resp, digester.Hash())
	m.origByte, err = ioutil.ReadAll(reader)
	if err != nil {
		rc.log.WithFields(logrus.Fields{
			"err": err,
			"ref": ref.Reference,
		}).Warn("Failed to read manifest")
		return nil, err
	}
	m.digest = digester.Digest()

	if m.digest.String() != resp.HTTPResponse().Header.Get("Docker-Content-Digest") {
		rc.log.WithFields(logrus.Fields{
			"computed": m.digest.String(),
			"returned": resp.HTTPResponse().Header.Get("Docker-Content-Digest"),
		}).Warn("Computed digest does not match header from registry")
	}

	// parse body into variable according to media type
	m.mt = resp.HTTPResponse().Header.Get("Content-Type")
	switch m.mt {
	case MediaTypeDocker2Manifest:
		err = json.Unmarshal(m.origByte, &m.dockerM)
	case MediaTypeDocker2ManifestList:
		err = json.Unmarshal(m.origByte, &m.dockerML)
	case MediaTypeOCI1Manifest:
		err = json.Unmarshal(m.origByte, &m.ociM)
	case MediaTypeOCI1ManifestList:
		err = json.Unmarshal(m.origByte, &m.ociML)
	default:
		rc.log.WithFields(logrus.Fields{
			"mediatype": m.mt,
			"ref":       ref.Reference,
		}).Warn("Unsupported media type for manifest")
		return nil, fmt.Errorf("Unknown manifest media type %s", m.mt)
	}
	// TODO: consider making a manifest Unmarshal method that detects which mediatype from the json
	// err = json.Unmarshal(m.origByte, &m)
	if err != nil {
		rc.log.WithFields(logrus.Fields{
			"err":       err,
			"mediatype": m.mt,
			"ref":       ref.Reference,
		}).Warn("Failed to unmarshal manifest")
		return nil, err
	}
	m.manifSet = true

	return &m, nil
}

func (rc *regClient) ManifestHead(ctx context.Context, ref Ref) (Manifest, error) {
	m := manifest{}

	// build the request
	host := rc.getHost(ref.Registry)
	var tagOrDigest string
	if ref.Digest != "" {
		tagOrDigest = ref.Digest
	} else if ref.Tag != "" {
		tagOrDigest = ref.Tag
	} else {
		rc.log.WithFields(logrus.Fields{
			"ref": ref.Reference,
		}).Warn("Manifest requires a tag or digest")
		return nil, ErrMissingTagOrDigest
	}

	manfURL := url.URL{
		Scheme: host.Scheme,
		Host:   host.DNS[0],
		Path:   "/v2/" + ref.Repository + "/manifests/" + tagOrDigest,
	}

	opts := []retryable.OptsReq{}
	opts = append(opts, retryable.WithHeader("Accept", []string{
		MediaTypeDocker2Manifest,
		MediaTypeDocker2ManifestList,
		MediaTypeOCI1Manifest,
		MediaTypeOCI1ManifestList,
	}))

	// send the request
	rty := rc.getRetryable(host)
	resp, err := rty.DoRequest(ctx, "HEAD", manfURL, opts...)
	if err != nil {
		return nil, err
	}

	if resp.HTTPResponse().StatusCode != 200 {
		return nil, fmt.Errorf("Unexpected http response code %d", resp.HTTPResponse().StatusCode)
	}

	// extract media type and digest from header
	m.mt = resp.HTTPResponse().Header.Get("Content-Type")
	m.digest, err = digest.Parse(resp.HTTPResponse().Header.Get("Docker-Content-Digest"))
	if err != nil {
		return nil, err
	}

	return &m, nil
}

func (rc *regClient) ManifestPut(ctx context.Context, ref Ref, m Manifest) error {
	host := rc.getHost(ref.Registry)
	manfURL := url.URL{
		Scheme: host.Scheme,
		Host:   host.DNS[0],
		Path:   "/v2/" + ref.Repository + "/manifests/",
	}
	if ref.Tag != "" {
		manfURL.Path += ref.Tag
	} else if ref.Digest != "" {
		manfURL.Path += ref.Digest
	} else {
		rc.log.WithFields(logrus.Fields{
			"ref": ref.Reference,
		}).Warn("Manifest put requires a tag")
		return ErrMissingTag
	}

	// add body to request
	opts := []retryable.OptsReq{}
	opts = append(opts, retryable.WithHeader("Content-Type", []string{m.GetMediaType()}))

	// mj, err := json.MarshalIndent(m, "", "  ")
	mj, err := m.MarshalJSON()
	if err != nil {
		rc.log.WithFields(logrus.Fields{
			"ref": ref.Reference,
			"err": err,
		}).Warn("Error marshaling manifest")
		return err
	}

	// TODO: if pushing by digest, recompute digest on mj?
	opts = append(opts, retryable.WithBodyBytes(mj))
	opts = append(opts, retryable.WithContentLen(int64(len(mj))))

	rty := rc.getRetryable(host)
	resp, err := rty.DoRequest(ctx, "PUT", manfURL, opts...)
	if err != nil {
		return fmt.Errorf("Error calling manifest put request: %w\nResponse object: %v", err, resp)
	}

	if resp.HTTPResponse().StatusCode < 200 || resp.HTTPResponse().StatusCode > 299 {
		body, _ := ioutil.ReadAll(resp)
		rc.log.WithFields(logrus.Fields{
			"ref":    ref.Reference,
			"status": resp.HTTPResponse().StatusCode,
			"body":   body,
		}).Warn("Unexpected status code for manifest")
		return fmt.Errorf("Unexpected status code on manifest put %d\nResponse object: %v\nBody: %s", resp.HTTPResponse().StatusCode, resp, body)
	}

	return nil
}

func d2oDescriptor(sd dockerDistribution.Descriptor) *ociv1.Descriptor {
	return &ociv1.Descriptor{
		MediaType:   sd.MediaType,
		Digest:      sd.Digest,
		Size:        sd.Size,
		URLs:        sd.URLs,
		Annotations: sd.Annotations,
		Platform:    sd.Platform,
	}
}

func dl2oDescriptor(sd dockerManifestList.ManifestDescriptor) *ociv1.Descriptor {
	return &ociv1.Descriptor{
		MediaType:   sd.MediaType,
		Digest:      sd.Digest,
		Size:        sd.Size,
		URLs:        sd.URLs,
		Annotations: sd.Annotations,
		Platform:    dlp2Platform(sd.Platform),
	}
}

func dlp2Platform(sp dockerManifestList.PlatformSpec) *ociv1.Platform {
	return &ociv1.Platform{
		Architecture: sp.Architecture,
		OS:           sp.OS,
		Variant:      sp.Variant,
		OSVersion:    sp.OSVersion,
		OSFeatures:   sp.OSFeatures,
	}
}

func d2oDescriptorList(src []dockerDistribution.Descriptor) []ociv1.Descriptor {
	var tgt []ociv1.Descriptor
	for _, sd := range src {
		tgt = append(tgt, *d2oDescriptor(sd))
	}
	return tgt
}
