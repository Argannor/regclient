// Package ocidir implements the OCI Image Layout scheme with a directory (not packed in a tar)
package ocidir

import (
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"path"
	"sync"

	"github.com/regclient/regclient/internal/rwfs"
	"github.com/regclient/regclient/scheme"
	"github.com/regclient/regclient/types"
	v1 "github.com/regclient/regclient/types/oci/v1"
	"github.com/regclient/regclient/types/ref"
	"github.com/sirupsen/logrus"
)

const (
	imageLayoutFile = "oci-layout"
	aRefName        = "org.opencontainers.image.ref.name"
)

// OCIDir is used for accessing OCI Image Layouts defined as a directory
type OCIDir struct {
	fs      rwfs.RWFS
	log     *logrus.Logger
	gc      bool
	modRefs map[string]ref.Ref
	mu      sync.Mutex
}

type config struct {
	fs  rwfs.RWFS
	gc  bool
	log *logrus.Logger
}

// Opts are used for passing options to ocidir
type Opts func(*config)

// New creates a new OCIDir with options
func New(opts ...Opts) *OCIDir {
	conf := config{
		log: &logrus.Logger{Out: ioutil.Discard},
		gc:  true,
	}
	for _, opt := range opts {
		opt(&conf)
	}
	return &OCIDir{
		fs:      conf.fs,
		log:     conf.log,
		gc:      conf.gc,
		modRefs: map[string]ref.Ref{},
	}
}

// WithFS allows the rwfs to be replaced
// The default is to use the OS, this can be used to sandbox within a folder
// This can also be used to pass an in-memory filesystem for testing or special use cases
func WithFS(fs rwfs.RWFS) Opts {
	return func(c *config) {
		c.fs = fs
	}
}

// WithGC configures the garbage collection setting
// This defaults to enabled
func WithGC(gc bool) Opts {
	return func(c *config) {
		c.gc = gc
	}
}

// WithLog provides a logrus logger
// By default logging is disabled
func WithLog(log *logrus.Logger) Opts {
	return func(c *config) {
		c.log = log
	}
}

// Info is experimental, do not use
func (o *OCIDir) Info() scheme.Info {
	return scheme.Info{ManifestPushFirst: true}
}

func (o *OCIDir) readIndex(r ref.Ref) (v1.Index, error) {
	// validate dir
	index := v1.Index{}
	err := o.valid(r.Path)
	if err != nil {
		return index, err
	}
	indexFile := path.Join(r.Path, "index.json")
	fh, err := o.fs.Open(indexFile)
	if err != nil {
		return index, fmt.Errorf("%s cannot be open: %w", indexFile, err)
	}
	defer fh.Close()
	ib, err := io.ReadAll(fh)
	if err != nil {
		return index, fmt.Errorf("%s cannot be read: %w", indexFile, err)
	}
	err = json.Unmarshal(ib, &index)
	if err != nil {
		return index, fmt.Errorf("%s cannot be parsed: %w", indexFile, err)
	}
	return index, nil
}

func (o *OCIDir) writeIndex(r ref.Ref, i v1.Index) error {
	// create/replace oci-layout file
	layout := v1.ImageLayout{
		Version: "1.0.0",
	}
	lb, err := json.Marshal(layout)
	if err != nil {
		return fmt.Errorf("cannot marshal layout: %w", err)
	}
	lfh, err := o.fs.Create(path.Join(r.Path, imageLayoutFile))
	if err != nil {
		return fmt.Errorf("cannot create %s: %w", imageLayoutFile, err)
	}
	defer lfh.Close()
	_, err = lfh.Write(lb)
	if err != nil {
		return fmt.Errorf("cannot write %s: %w", imageLayoutFile, err)
	}
	// create/replace index.json file
	indexFile := path.Join(r.Path, "index.json")
	fh, err := o.fs.Create(indexFile)
	if err != nil {
		return fmt.Errorf("%s cannot be created: %w", indexFile, err)
	}
	defer fh.Close()
	b, err := json.Marshal(i)
	if err != nil {
		return fmt.Errorf("cannot marshal index: %w", err)
	}
	_, err = fh.Write(b)
	if err != nil {
		return fmt.Errorf("cannot write index: %w", err)
	}
	return nil
}

// func valid (dir) (error) // check for `oci-layout` file and `index.json` for read
func (o *OCIDir) valid(dir string) error {
	layout := v1.ImageLayout{}
	reqVer := "1.0.0"
	fh, err := o.fs.Open(path.Join(dir, imageLayoutFile))
	if err != nil {
		return fmt.Errorf("%s cannot be open: %w", imageLayoutFile, err)
	}
	defer fh.Close()
	lb, err := io.ReadAll(fh)
	if err != nil {
		return fmt.Errorf("%s cannot be read: %w", imageLayoutFile, err)
	}
	err = json.Unmarshal(lb, &layout)
	if err != nil {
		return fmt.Errorf("%s cannot be parsed: %w", imageLayoutFile, err)
	}
	if layout.Version != reqVer {
		return fmt.Errorf("unsupported oci layout version, expected %s, received %s", reqVer, layout.Version)
	}
	return nil
}

func (o *OCIDir) refMod(r ref.Ref) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.modRefs[r.Path] = r
}

func indexCreate() v1.Index {
	i := v1.Index{
		Versioned:   v1.IndexSchemaVersion,
		MediaType:   types.MediaTypeOCI1ManifestList,
		Manifests:   []types.Descriptor{},
		Annotations: map[string]string{},
	}
	return i
}

func indexGet(index v1.Index, r ref.Ref) (types.Descriptor, error) {
	if r.Digest == "" && r.Tag == "" {
		r.Tag = "latest"
	}
	if r.Digest != "" {
		for _, im := range index.Manifests {
			if im.Digest.String() == r.Digest {
				return im, nil
			}
		}
	} else if r.Tag != "" {
		for _, im := range index.Manifests {
			if name, ok := im.Annotations[aRefName]; ok && name == r.Tag {
				return im, nil
			}
		}
	}
	return types.Descriptor{}, types.ErrNotFound
}

func indexSet(index *v1.Index, r ref.Ref, d types.Descriptor) error {
	if index == nil {
		return fmt.Errorf("index is nil")
	}
	if r.Tag != "" {
		if d.Annotations == nil {
			d.Annotations = map[string]string{}
		}
		d.Annotations[aRefName] = r.Tag
	}
	if index.Manifests == nil {
		index.Manifests = []types.Descriptor{}
	}
	pos := -1
	// search for existing
	for i := range index.Manifests {
		var name string
		var ok bool
		if r.Tag != "" && index.Manifests[i].Annotations != nil {
			name, ok = index.Manifests[i].Annotations[aRefName]
		}
		if (index.Manifests[i].Digest == d.Digest && !ok) || (ok && name == r.Tag) {
			index.Manifests[i] = d
			pos = i
			break
		}
	}
	if pos >= 0 {
		// existing entry was replaced, remove any dup entries
		for i := len(index.Manifests) - 1; i > pos; i-- {
			var name string
			var ok bool
			if r.Tag != "" && index.Manifests[i].Annotations != nil {
				name, ok = index.Manifests[i].Annotations[aRefName]
			}
			if (index.Manifests[i].Digest == d.Digest && !ok) || (ok && name == r.Tag) {
				index.Manifests = append(index.Manifests[:i], index.Manifests[i+1:]...)
			}
		}
	} else {
		// existing entry to replace was not found, add the descriptor
		index.Manifests = append(index.Manifests, d)
	}
	return nil
}
