// Package blobtest is an in-memory S3 server for tests: enough of the API for blob.S3 (path-style buckets,
// PUT with aws-chunked bodies, GET with ranges, HEAD, DELETE, ListObjectsV2). Signatures are not checked.
package blobtest

import (
	"bufio"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

type object struct {
	data    []byte
	ctype   string
	modTime time.Time
}

// S3 is the fake; Objects lists its keys for assertions.
type S3 struct {
	URL     string
	mu      sync.Mutex
	buckets map[string]map[string]object
}

// NewS3 starts the fake with the given buckets; it stops with the test.
func NewS3(t testing.TB, buckets ...string) *S3 {
	f := &S3{buckets: map[string]map[string]object{}}
	for _, b := range buckets {
		f.buckets[b] = map[string]object{}
	}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	f.URL = srv.URL
	return f
}

// Keys returns the object keys of a bucket, sorted.
func (f *S3) Keys(bucket string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for k := range f.buckets[bucket] {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Get returns an object's bytes, or nil.
func (f *S3) Get(bucket, key string) []byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.buckets[bucket][key].data
}

func s3Error(w http.ResponseWriter, r *http.Request, status int, code string) {
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(status)
	if r.Method != http.MethodHead {
		fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?><Error><Code>%s</Code><Message>%s</Message></Error>`, code, code)
	}
}

func (f *S3) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/")
	bucket, key, _ := strings.Cut(path, "/")
	f.mu.Lock()
	defer f.mu.Unlock()
	objs, ok := f.buckets[bucket]
	if key == "" {
		switch {
		case r.Method == http.MethodPut:
			if !ok {
				f.buckets[bucket] = map[string]object{}
			}
			w.WriteHeader(200)
		case !ok:
			s3Error(w, r, 404, "NoSuchBucket")
		case r.Method == http.MethodHead:
			w.WriteHeader(200)
		case r.Method == http.MethodGet && r.URL.Query().Has("location"):
			fmt.Fprint(w, `<?xml version="1.0" encoding="UTF-8"?><LocationConstraint>us-east-1</LocationConstraint>`)
		case r.Method == http.MethodGet:
			f.list(w, r, objs)
		default:
			s3Error(w, r, 405, "MethodNotAllowed")
		}
		return
	}
	if !ok {
		s3Error(w, r, 404, "NoSuchBucket")
		return
	}
	switch r.Method {
	case http.MethodPut:
		body, err := readBody(r)
		if err != nil {
			s3Error(w, r, 400, "IncompleteBody")
			return
		}
		objs[key] = object{data: body, ctype: r.Header.Get("Content-Type"), modTime: time.Now().UTC().Truncate(time.Second)}
		w.Header().Set("ETag", `"`+strconv.Itoa(len(body))+`"`)
		w.WriteHeader(200)
	case http.MethodGet, http.MethodHead:
		o, ok := objs[key]
		if !ok {
			s3Error(w, r, 404, "NoSuchKey")
			return
		}
		w.Header().Set("ETag", `"`+strconv.Itoa(len(o.data))+`"`)
		w.Header().Set("Content-Type", o.ctype)
		w.Header().Set("Last-Modified", o.modTime.Format(http.TimeFormat))
		http.ServeContent(w, r, "", o.modTime, bytes.NewReader(o.data))
	case http.MethodDelete:
		delete(objs, key)
		w.WriteHeader(204)
	default:
		s3Error(w, r, 405, "MethodNotAllowed")
	}
}

// readBody decodes the aws-chunked framing minio-go uses over plain HTTP ("<hex>;chunk-signature=...\r\n<data>\r\n",
// then a 0 chunk and optional trailers); other bodies are read as they are.
func readBody(r *http.Request) ([]byte, error) {
	if !strings.HasPrefix(r.Header.Get("X-Amz-Content-Sha256"), "STREAMING-") &&
		!strings.Contains(r.Header.Get("Content-Encoding"), "aws-chunked") {
		return io.ReadAll(r.Body)
	}
	br := bufio.NewReader(r.Body)
	var out bytes.Buffer
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			return nil, err
		}
		sizeHex, _, _ := strings.Cut(strings.TrimSpace(line), ";")
		n, err := strconv.ParseInt(sizeHex, 16, 64)
		if err != nil {
			return nil, err
		}
		if n == 0 {
			return out.Bytes(), nil // trailers, if any, are ignored
		}
		if _, err := io.CopyN(&out, br, n); err != nil {
			return nil, err
		}
		if _, err := br.ReadString('\n'); err != nil {
			return nil, err
		}
	}
}

type listResult struct {
	XMLName     xml.Name `xml:"ListBucketResult"`
	Name        string
	Prefix      string
	KeyCount    int
	MaxKeys     int
	IsTruncated bool
	Contents    []listObject
}

type listObject struct {
	Key          string
	LastModified string
	ETag         string
	Size         int64
	StorageClass string
}

func (f *S3) list(w http.ResponseWriter, r *http.Request, objs map[string]object) {
	prefix := r.URL.Query().Get("prefix")
	res := listResult{Name: strings.Trim(r.URL.Path, "/"), Prefix: prefix, MaxKeys: 1000}
	keys := make([]string, 0, len(objs))
	for k := range objs {
		if strings.HasPrefix(k, prefix) {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	for _, k := range keys {
		o := objs[k]
		res.Contents = append(res.Contents, listObject{Key: k, LastModified: o.modTime.Format(time.RFC3339),
			ETag: `"` + strconv.Itoa(len(o.data)) + `"`, Size: int64(len(o.data)), StorageClass: "STANDARD"})
	}
	res.KeyCount = len(res.Contents)
	w.Header().Set("Content-Type", "application/xml")
	_ = xml.NewEncoder(w).Encode(res)
}
