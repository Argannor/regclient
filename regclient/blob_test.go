package regclient

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/opencontainers/go-digest"
	"github.com/regclient/regclient/regclient/types"
	"github.com/sirupsen/logrus"
)

func TestBlobGet(t *testing.T) {
	blobRepo := "/proj/repo"
	privateRepo := "/proj/private"
	ctx := context.Background()
	// include a random blob
	seed := time.Now().UTC().Unix()
	t.Logf("Using seed %d", seed)
	blobLen := 1024 // must be greater than 512 for retry test
	d1, blob1 := newRandomBlob(blobLen, seed)
	d2, blob2 := newRandomBlob(blobLen, seed+1)
	dMissing := digest.FromBytes([]byte("missing"))
	// define req/resp entries
	rrs := []ReqResp{
		// head
		{
			ReqEntry: ReqEntry{
				Name:   "HEAD for d1",
				Method: "HEAD",
				Path:   "/v2" + blobRepo + "/blobs/" + d1.String(),
			},
			RespEntry: RespEntry{
				Status: http.StatusOK,
				Headers: http.Header{
					"Content-Length":        {fmt.Sprintf("%d", blobLen)},
					"Content-Type":          {"application/octet-stream"},
					"Docker-Content-Digest": {d1.String()},
				},
			},
		},
		// get
		{
			ReqEntry: ReqEntry{
				Name:   "GET for d1",
				Method: "GET",
				Path:   "/v2" + blobRepo + "/blobs/" + d1.String(),
			},
			RespEntry: RespEntry{
				Status: http.StatusOK,
				Body:   blob1,
				Headers: http.Header{
					"Content-Length":        {fmt.Sprintf("%d", blobLen)},
					"Content-Type":          {"application/octet-stream"},
					"Docker-Content-Digest": {d1.String()},
				},
			},
		},
		// missing
		{
			ReqEntry: ReqEntry{
				Name:   "GET Missing",
				Method: "GET",
				Path:   "/v2" + blobRepo + "/blobs/" + dMissing.String(),
			},
			RespEntry: RespEntry{
				Status: http.StatusNotFound,
			},
		},
		// TODO: test unauthorized
		// TODO: test range read
		// head for d2
		{
			ReqEntry: ReqEntry{
				Name:   "HEAD for d2",
				Method: "HEAD",
				Path:   "/v2" + blobRepo + "/blobs/" + d2.String(),
			},
			RespEntry: RespEntry{
				Status: http.StatusOK,
				Headers: http.Header{
					"Accept-Ranges":         {"bytes"},
					"Content-Length":        {fmt.Sprintf("%d", blobLen)},
					"Content-Type":          {"application/octet-stream"},
					"Docker-Content-Digest": {d2.String()},
				},
			},
		},
		// get range
		{
			ReqEntry: ReqEntry{
				Name:   "GET for d2, range for second part",
				Method: "GET",
				Path:   "/v2" + blobRepo + "/blobs/" + d2.String(),
				Headers: http.Header{
					"Range": {fmt.Sprintf("bytes=512-%d", blobLen)},
				},
			},
			RespEntry: RespEntry{
				Status: http.StatusOK,
				Body:   blob2[512:],
				Headers: http.Header{
					"Content-Length":        {fmt.Sprintf("%d", blobLen-512)},
					"Content-Type":          {"application/octet-stream"},
					"Docker-Content-Digest": {d2.String()},
				},
			},
		},
		// get that stops early
		{
			ReqEntry: ReqEntry{
				Name:   "GET for d2, short read",
				Method: "GET",
				Path:   "/v2" + blobRepo + "/blobs/" + d2.String(),
			},
			RespEntry: RespEntry{
				Status: http.StatusOK,
				Body:   blob2[0:512],
				Headers: http.Header{
					"Content-Length":        {fmt.Sprintf("%d", blobLen)},
					"Content-Type":          {"application/octet-stream"},
					"Docker-Content-Digest": {d2.String()},
				},
			},
		},
		// forbidden
		{
			ReqEntry: ReqEntry{
				Name:   "GET Forbidden",
				Method: "GET",
				Path:   "/v2" + privateRepo + "/blobs/" + d1.String(),
			},
			RespEntry: RespEntry{
				Status: http.StatusForbidden,
			},
		},
	}
	rrs = append(rrs, rrBaseEntries...)
	// create a server
	ts := httptest.NewServer(NewHandler(t, rrs))
	defer ts.Close()
	// setup the regclient
	tsURL, _ := url.Parse(ts.URL)
	tsHost := tsURL.Host
	rcHosts := []ConfigHost{
		{
			Name:     tsHost,
			Hostname: tsHost,
			TLS:      TLSDisabled,
		},
	}
	log := &logrus.Logger{
		Out:       os.Stderr,
		Formatter: new(logrus.TextFormatter),
		Hooks:     make(logrus.LevelHooks),
		Level:     logrus.WarnLevel,
	}
	rc := NewRegClient(WithConfigHosts(rcHosts), WithLog(log))

	// Test successful blob
	t.Run("Get", func(t *testing.T) {
		ref, err := types.NewRef(tsURL.Host + blobRepo)
		if err != nil {
			t.Errorf("Failed creating ref: %v", err)
		}
		br, err := rc.BlobGet(ctx, ref, d1, nil)
		if err != nil {
			t.Errorf("Failed running BlobGet: %v", err)
			return
		}
		defer br.Close()
		brBlob, err := ioutil.ReadAll(br)
		if err != nil {
			t.Errorf("Failed reading blob: %v", err)
			return
		}
		if bytes.Compare(blob1, brBlob) != 0 {
			t.Errorf("Blob does not match")
		}
	})

	t.Run("Head", func(t *testing.T) {
		ref, err := types.NewRef(tsURL.Host + blobRepo)
		if err != nil {
			t.Errorf("Failed creating ref: %v", err)
		}
		br, err := rc.BlobHead(ctx, ref, d1)
		if err != nil {
			t.Errorf("Failed running BlobHead: %v", err)
			return
		}
		defer br.Close()
		if br.Length() != int64(blobLen) {
			t.Errorf("Failed comparing blob length")
		}
	})

	t.Run("Missing", func(t *testing.T) {
		ref, err := types.NewRef(tsURL.Host + blobRepo)
		if err != nil {
			t.Errorf("Failed creating ref: %v", err)
		}
		br, err := rc.BlobGet(ctx, ref, dMissing, nil)
		if err == nil {
			defer br.Close()
			t.Errorf("Unexpected success running BlobGet")
			return
		}
		if !errors.Is(err, ErrNotFound) {
			t.Errorf("Error does not match \"ErrNotFound\": %v", err)
		}
	})

	t.Run("Retry", func(t *testing.T) {
		ref, err := types.NewRef(tsURL.Host + blobRepo)
		if err != nil {
			t.Errorf("Failed creating ref: %v", err)
		}
		br, err := rc.BlobGet(ctx, ref, d2, nil)
		if err != nil {
			t.Errorf("Failed running BlobGet: %v", err)
			return
		}
		defer br.Close()
		brBlob, err := ioutil.ReadAll(br)
		if err != nil {
			t.Errorf("Failed reading blob: %v", err)
			return
		}
		if bytes.Compare(blob2, brBlob) != 0 {
			t.Errorf("Blob does not match")
		}
	})

	t.Run("Forbidden", func(t *testing.T) {
		ref, err := types.NewRef(tsURL.Host + privateRepo)
		if err != nil {
			t.Errorf("Failed creating ref: %v", err)
		}
		br, err := rc.BlobGet(ctx, ref, d1, nil)
		if err == nil {
			defer br.Close()
			t.Errorf("Unexpected success running BlobGet")
			return
		}
		if !errors.Is(err, ErrUnauthorized) {
			t.Errorf("Error does not match \"ErrUnauthorized\": %v", err)
		}
	})

}

func TestBlobPut(t *testing.T) {
	blobRepo := "/proj/repo"
	// privateRepo := "/proj/private"
	ctx := context.Background()
	// include a random blob
	seed := time.Now().UTC().Unix()
	t.Logf("Using seed %d", seed)
	blobChunk := 512
	blobLen := 1024 // must be blobChunk < blobLen <= blobChunk * 2
	d1, blob1 := newRandomBlob(blobLen, seed)
	uuid1 := uuid.New()
	d2, blob2 := newRandomBlob(blobLen, seed+1)
	uuid2 := uuid.New()
	// dMissing := digest.FromBytes([]byte("missing"))
	// define req/resp entries
	rrs := []ReqResp{
		// get upload location
		{
			ReqEntry: ReqEntry{
				Name:   "POST for d1",
				Method: "POST",
				Path:   "/v2" + blobRepo + "/blobs/uploads/",
				Query: map[string][]string{
					"mount": {d1.String()},
				},
			},
			RespEntry: RespEntry{
				Status: http.StatusAccepted,
				Headers: http.Header{
					"Content-Length": {"0"},
					"Range":          {"bytes=0-0"},
					"Location":       {uuid1.String()},
				},
			},
		},
		// upload blob
		{
			ReqEntry: ReqEntry{
				Name:   "PUT for d1",
				Method: "PUT",
				Path:   "/v2" + blobRepo + "/blobs/uploads/" + uuid1.String(),
				Query: map[string][]string{
					"digest": {d1.String()},
				},
				Headers: http.Header{
					"Content-Length": {fmt.Sprintf("%d", len(blob1))},
					"Content-Type":   {"application/octet-stream"},
				},
				Body: blob1,
			},
			RespEntry: RespEntry{
				Status: http.StatusCreated,
				Headers: http.Header{
					"Content-Length":        {"0"},
					"Location":              {"/v2" + blobRepo + "/blobs/" + d1.String()},
					"Docker-Content-Digest": {d1.String()},
				},
			},
		},

		// get upload2 location
		{
			ReqEntry: ReqEntry{
				Name:   "POST for d2",
				Method: "POST",
				Path:   "/v2" + blobRepo + "/blobs/uploads/",
				Query: map[string][]string{
					"mount": {d2.String()},
				},
			},
			RespEntry: RespEntry{
				Status: http.StatusAccepted,
				Headers: http.Header{
					"Content-Length": {"0"},
					"Range":          {"bytes=0-0"},
					"Location":       {uuid2.String()},
				},
			},
		},
		// upload put for d2
		{
			ReqEntry: ReqEntry{
				DelOnUse: false,
				Name:     "PUT for patched d2",
				Method:   "PUT",
				Path:     "/v2" + blobRepo + "/blobs/uploads/" + uuid2.String(),
				Query: map[string][]string{
					"digest": {d2.String()},
					"chunk":  {"3"},
				},
				Headers: http.Header{
					"Content-Length": {"0"},
					"Content-Range":  {fmt.Sprintf("%d-%d", blobLen, blobLen)},
					"Content-Type":   {"application/octet-stream"},
				},
			},
			RespEntry: RespEntry{
				Status: http.StatusCreated,
				Headers: http.Header{
					"Content-Length":        {"0"},
					"Location":              {"/v2" + blobRepo + "/blobs/" + d2.String()},
					"Docker-Content-Digest": {d2.String()},
				},
			},
		},
		// upload patch 2 fail for d2
		{
			ReqEntry: ReqEntry{
				DelOnUse: true,
				Name:     "PATCH 2 fail for d2",
				Method:   "PATCH",
				Path:     "/v2" + blobRepo + "/blobs/uploads/" + uuid2.String(),
				Query: map[string][]string{
					"chunk": {"2"},
				},
				Headers: http.Header{
					"Content-Length": {fmt.Sprintf("%d", blobChunk)},
					"Content-Range":  {fmt.Sprintf("%d-%d", blobChunk, blobLen)},
					"Content-Type":   {"application/octet-stream"},
				},
				Body: blob2[blobChunk:],
			},
			RespEntry: RespEntry{
				Status: http.StatusGatewayTimeout,
				Headers: http.Header{
					"Content-Length": {fmt.Sprintf("%d", 0)},
				},
			},
		},
		// upload patch 2 for d2
		{
			ReqEntry: ReqEntry{
				DelOnUse: false,
				Name:     "PATCH 2 for d2",
				Method:   "PATCH",
				Path:     "/v2" + blobRepo + "/blobs/uploads/" + uuid2.String(),
				Query: map[string][]string{
					"chunk": {"2"},
				},
				Headers: http.Header{
					"Content-Length": {fmt.Sprintf("%d", blobChunk)},
					"Content-Range":  {fmt.Sprintf("%d-%d", blobChunk, blobLen)},
					"Content-Type":   {"application/octet-stream"},
				},
				Body: blob2[blobChunk:],
			},
			RespEntry: RespEntry{
				Status: http.StatusAccepted,
				Headers: http.Header{
					"Content-Length": {fmt.Sprintf("%d", 0)},
					"Range":          {fmt.Sprintf("bytes=0-%d", blobLen)},
					"Location":       {uuid2.String() + "?chunk=3"},
				},
			},
		},
		// upload patch 1 for d2
		{
			ReqEntry: ReqEntry{
				DelOnUse: false,
				Name:     "PATCH 1 for d2",
				Method:   "PATCH",
				Path:     "/v2" + blobRepo + "/blobs/uploads/" + uuid2.String(),
				Query:    map[string][]string{},
				Headers: http.Header{
					"Content-Length": {fmt.Sprintf("%d", blobChunk)},
					"Content-Range":  {fmt.Sprintf("0-%d", blobChunk)},
					"Content-Type":   {"application/octet-stream"},
				},
				Body: blob2[0:blobChunk],
			},
			RespEntry: RespEntry{
				Status: http.StatusAccepted,
				Headers: http.Header{
					"Content-Length": {fmt.Sprintf("%d", 0)},
					"Range":          {fmt.Sprintf("bytes=0-%d", blobChunk)},
					"Location":       {uuid2.String() + "?chunk=2"},
				},
			},
		},
		// upload blob
		{
			ReqEntry: ReqEntry{
				DelOnUse: false,
				Name:     "PUT for d2",
				Method:   "PUT",
				Path:     "/v2" + blobRepo + "/blobs/uploads/" + uuid2.String(),
				Query: map[string][]string{
					"digest": {d2.String()},
				},
				Headers: http.Header{
					"Content-Length": {fmt.Sprintf("%d", len(blob2))},
					"Content-Type":   {"application/octet-stream"},
				},
				Body: blob2,
			},
			RespEntry: RespEntry{
				Status: http.StatusGatewayTimeout,
				Headers: http.Header{
					"Content-Length": {fmt.Sprintf("%d", 0)},
				},
			},
		},
	}
	rrs = append(rrs, rrBaseEntries...)
	// create a server
	ts := httptest.NewServer(NewHandler(t, rrs))
	defer ts.Close()
	// setup the regclient
	tsURL, _ := url.Parse(ts.URL)
	tsHost := tsURL.Host
	rcHosts := []ConfigHost{
		{
			Name:     tsHost,
			Hostname: tsHost,
			TLS:      TLSDisabled,
		},
	}
	log := &logrus.Logger{
		Out:       os.Stderr,
		Formatter: new(logrus.TextFormatter),
		Hooks:     make(logrus.LevelHooks),
		Level:     logrus.WarnLevel,
	}
	delayInit, _ := time.ParseDuration("0.1s")
	delayMax, _ := time.ParseDuration("0.5s")
	rc := NewRegClient(
		WithConfigHosts(rcHosts),
		WithLog(log),
		WithBlobSize(int64(blobChunk), int64(blobLen*10)),
		WithRetryDelay(delayInit, delayMax),
	)

	t.Run("Put", func(t *testing.T) {
		ref, err := types.NewRef(tsURL.Host + blobRepo)
		if err != nil {
			t.Errorf("Failed creating ref: %v", err)
		}
		br := bytes.NewReader(blob1)
		dp, clp, err := rc.BlobPut(ctx, ref, d1, br, "", int64(len(blob1)))
		if err != nil {
			t.Errorf("Failed running BlobPut: %v", err)
			return
		}
		if dp.String() != d1.String() {
			t.Errorf("Digest mismatch, expected %s, received %s", d1.String(), dp.String())
		}
		if clp != int64(len(blob1)) {
			t.Errorf("Content length mismatch, expected %d, received %d", len(blob1), clp)
		}

	})

	t.Run("Retry", func(t *testing.T) {
		ref, err := types.NewRef(tsURL.Host + blobRepo)
		if err != nil {
			t.Errorf("Failed creating ref: %v", err)
		}
		br := bytes.NewReader(blob2)
		dp, clp, err := rc.BlobPut(ctx, ref, d2, br, "", int64(len(blob2)))
		if err != nil {
			t.Errorf("Failed running BlobPut: %v", err)
			return
		}
		if dp.String() != d2.String() {
			t.Errorf("Digest mismatch, expected %s, received %s", d2.String(), dp.String())
		}
		if clp != int64(len(blob2)) {
			t.Errorf("Content length mismatch, expected %d, received %d", len(blob2), clp)
		}

	})

}
