// Package dockerproxy implements a minimal OCI Distribution v2 registry
// that listens on a loopback port and proxies blob/manifest reads and writes
// to grog's content-addressable store.
//
// The point of this package is to let the local Docker daemon push and pull
// images via its normal HTTP code path while we transparently store the
// underlying blobs in the CAS (which itself is backed by S3/GCS/Azure/disk).
// Because the daemon does the streaming, gzip work, and parallelism, grog
// never has to materialise an image in its own memory — matching the
// performance of pushing to a real remote registry.
//
// Only the subset of endpoints needed by `docker push` / `docker pull` is
// implemented:
//
//	GET    /v2/                                       version probe
//	HEAD   /v2/<name>/blobs/<digest>                  blob existence check
//	GET    /v2/<name>/blobs/<digest>                  blob download (streaming)
//	POST   /v2/<name>/blobs/uploads/[?digest=...]     start upload (monolithic if digest set)
//	PATCH  /v2/<name>/blobs/uploads/<uuid>            chunked upload append
//	PUT    /v2/<name>/blobs/uploads/<uuid>?digest=... finalize chunked upload
//	HEAD   /v2/<name>/manifests/<reference>           manifest existence check
//	GET    /v2/<name>/manifests/<reference>           manifest download
//	PUT    /v2/<name>/manifests/<reference>           manifest upload
//
// The "name" portion of the URL is treated as a routing prefix only — CAS
// keys are content digests, never names. Manifest GETs only support digest
// references (sha256:...) since grog always pulls by digest.
package dockerproxy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash"
	"io"
	"net"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"grog/internal/caching"
	"grog/internal/console"
)

// Registry is an in-process OCI Distribution v2 server backed by a CAS.
type Registry struct {
	cas      *caching.Cas
	server   *http.Server
	listener net.Listener
	tempDir  string
	logger   *console.Logger

	uploadsMu sync.Mutex
	uploads   map[string]*pendingUpload

	// manifestsByName remembers the digest of the most recent manifest PUT
	// per repository name. The DockerOutputHandler reads this back after
	// `docker push` to learn the manifest digest the daemon produced.
	manifestsMu     sync.Mutex
	manifestsByName map[string]string
}

// pendingUpload tracks an in-flight chunked blob upload. Bytes are buffered
// to a temp file rather than memory so layers larger than RAM are tolerated.
// A running SHA256 lets us validate the client-supplied digest at PUT time
// without re-reading the file.
type pendingUpload struct {
	mu       sync.Mutex
	file     *os.File
	digester hash.Hash
	written  int64
}

// New starts an OCI Distribution v2 server on a random loopback port.
// The caller is responsible for invoking Close() to shut it down.
func New(ctx context.Context, cas *caching.Cas) (*Registry, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("dockerproxy: listen: %w", err)
	}

	tempDir, err := os.MkdirTemp("", "grog-dockerproxy-*")
	if err != nil {
		_ = listener.Close()
		return nil, fmt.Errorf("dockerproxy: temp dir: %w", err)
	}

	r := &Registry{
		cas:             cas,
		listener:        listener,
		tempDir:         tempDir,
		logger:          console.GetLogger(ctx),
		uploads:         make(map[string]*pendingUpload),
		manifestsByName: make(map[string]string),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/v2/", r.handle)
	mux.HandleFunc("/v2", r.handle)

	r.server = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 30 * time.Second,
	}

	go func() {
		if err := r.server.Serve(listener); err != nil && err != http.ErrServerClosed {
			r.logger.Warnf("dockerproxy server stopped: %v", err)
		}
	}()

	r.logger.Debugf("dockerproxy listening on %s", listener.Addr())

	return r, nil
}

// Addr returns the host:port of the loopback registry, suitable for use as
// the registry portion of a Docker image reference.
func (r *Registry) Addr() string {
	return r.listener.Addr().String()
}

// Close stops the HTTP server, removes any pending uploads, and deletes
// the temp directory. Safe to call multiple times.
func (r *Registry) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = r.server.Shutdown(ctx)

	r.uploadsMu.Lock()
	for id, u := range r.uploads {
		u.close()
		delete(r.uploads, id)
	}
	r.uploadsMu.Unlock()

	return os.RemoveAll(r.tempDir)
}

// digestPattern accepts the canonical OCI digest form sha256:<64-hex>.
var digestPattern = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)

// handle is the single ServeMux entry — we route by URL shape ourselves
// because the OCI <name> can itself contain slashes (e.g. "library/ubuntu").
func (r *Registry) handle(w http.ResponseWriter, req *http.Request) {
	r.logger.Tracef("dockerproxy: %s %s", req.Method, req.URL.RequestURI())
	// Version probe — both GET and HEAD must return 200 with the API version header.
	if req.URL.Path == "/v2/" || req.URL.Path == "/v2" {
		w.Header().Set("Docker-Distribution-API-Version", "registry/2.0")
		w.WriteHeader(http.StatusOK)
		return
	}

	// Strip /v2/ prefix and find the "blobs" or "manifests" segment;
	// everything before it is the repository <name>.
	rest := strings.TrimPrefix(req.URL.Path, "/v2/")
	parts := strings.Split(rest, "/")

	splitIdx := -1
	for i, p := range parts {
		if p == "blobs" || p == "manifests" {
			splitIdx = i
			break
		}
	}
	if splitIdx <= 0 || splitIdx == len(parts)-1 {
		http.NotFound(w, req)
		return
	}

	name := strings.Join(parts[:splitIdx], "/")
	kind := parts[splitIdx]
	tail := parts[splitIdx+1:]

	switch kind {
	case "blobs":
		r.handleBlobs(w, req, name, tail)
	case "manifests":
		r.handleManifests(w, req, name, tail)
	default:
		http.NotFound(w, req)
	}
}

func (r *Registry) handleBlobs(w http.ResponseWriter, req *http.Request, name string, tail []string) {
	if tail[0] == "uploads" {
		var uploadID string
		if len(tail) > 1 {
			uploadID = tail[1]
		}
		switch req.Method {
		case http.MethodPost:
			r.startBlobUpload(w, req, name)
		case http.MethodPatch:
			r.patchBlobUpload(w, req, name, uploadID)
		case http.MethodPut:
			r.finishBlobUpload(w, req, name, uploadID)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
		return
	}

	// /v2/<name>/blobs/<digest>
	digest := tail[0]
	if !digestPattern.MatchString(digest) {
		writeError(w, http.StatusBadRequest, "DIGEST_INVALID", "invalid digest format")
		return
	}

	switch req.Method {
	case http.MethodHead:
		r.headBlob(w, req, digest)
	case http.MethodGet:
		r.getBlob(w, req, digest)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (r *Registry) headBlob(w http.ResponseWriter, req *http.Request, digest string) {
	exists, err := r.cas.Exists(req.Context(), digest)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	if !exists {
		writeError(w, http.StatusNotFound, "BLOB_UNKNOWN", "blob not found")
		return
	}
	// Docker daemon refuses HEAD responses without Content-Length even though
	// the body is empty — it uses the header to learn the blob size up front.
	size, err := r.cas.Size(req.Context(), digest)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	w.Header().Set("Docker-Content-Digest", digest)
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
	w.WriteHeader(http.StatusOK)
}

func (r *Registry) getBlob(w http.ResponseWriter, req *http.Request, digest string) {
	// Look up the size first so we can set Content-Length on the response and
	// avoid Go's default chunked transfer-encoding (which the daemon rejects).
	size, err := r.cas.Size(req.Context(), digest)
	if err != nil {
		writeError(w, http.StatusNotFound, "BLOB_UNKNOWN", err.Error())
		return
	}
	reader, err := r.cas.Load(req.Context(), digest)
	if err != nil {
		writeError(w, http.StatusNotFound, "BLOB_UNKNOWN", err.Error())
		return
	}
	defer reader.Close()
	w.Header().Set("Docker-Content-Digest", digest)
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
	w.WriteHeader(http.StatusOK)
	if _, err := io.Copy(w, reader); err != nil {
		r.logger.Warnf("dockerproxy: blob %s stream error: %v", digest, err)
	}
}

// startBlobUpload handles both monolithic POST (with ?digest=) and the start
// of a chunked upload session (no digest, returns a session UUID + Location).
func (r *Registry) startBlobUpload(w http.ResponseWriter, req *http.Request, name string) {
	digest := req.URL.Query().Get("digest")

	if digest != "" {
		// Monolithic upload — body contains the entire blob.
		if !digestPattern.MatchString(digest) {
			writeError(w, http.StatusBadRequest, "DIGEST_INVALID", "invalid digest format")
			return
		}
		if err := r.cas.Write(req.Context(), digest, req.Body); err != nil {
			writeError(w, http.StatusInternalServerError, "BLOB_UPLOAD_INVALID", err.Error())
			return
		}
		w.Header().Set("Location", "/v2/"+name+"/blobs/"+digest)
		w.Header().Set("Docker-Content-Digest", digest)
		w.WriteHeader(http.StatusCreated)
		return
	}

	// Chunked upload — open a temp file and return a session ID.
	id, err := r.openUpload()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "BLOB_UPLOAD_INVALID", err.Error())
		return
	}
	w.Header().Set("Location", "/v2/"+name+"/blobs/uploads/"+id)
	w.Header().Set("Docker-Upload-UUID", id)
	w.Header().Set("Range", "0-0")
	w.WriteHeader(http.StatusAccepted)
}

func (r *Registry) patchBlobUpload(w http.ResponseWriter, req *http.Request, name, uploadID string) {
	if uploadID == "" {
		writeError(w, http.StatusBadRequest, "BLOB_UPLOAD_INVALID", "missing upload id")
		return
	}
	upload := r.getUpload(uploadID)
	if upload == nil {
		writeError(w, http.StatusNotFound, "BLOB_UPLOAD_UNKNOWN", "upload session not found")
		return
	}

	upload.mu.Lock()
	_, copyErr := io.Copy(upload, req.Body)
	written := upload.written
	upload.mu.Unlock()

	if copyErr != nil {
		// On a partial write the session is in an unknown state — drop it
		// so the daemon starts over instead of trusting a stale Range header.
		r.removeUpload(uploadID)
		writeError(w, http.StatusInternalServerError, "BLOB_UPLOAD_INVALID", copyErr.Error())
		return
	}

	w.Header().Set("Location", "/v2/"+name+"/blobs/uploads/"+uploadID)
	w.Header().Set("Docker-Upload-UUID", uploadID)
	if written > 0 {
		w.Header().Set("Range", "0-"+strconv.FormatInt(written-1, 10))
	} else {
		w.Header().Set("Range", "0-0")
	}
	w.WriteHeader(http.StatusAccepted)
}

func (r *Registry) finishBlobUpload(w http.ResponseWriter, req *http.Request, name, uploadID string) {
	if uploadID == "" {
		writeError(w, http.StatusBadRequest, "BLOB_UPLOAD_INVALID", "missing upload id")
		return
	}
	digest := req.URL.Query().Get("digest")
	if !digestPattern.MatchString(digest) {
		writeError(w, http.StatusBadRequest, "DIGEST_INVALID", "invalid or missing digest")
		return
	}

	upload := r.getUpload(uploadID)
	if upload == nil {
		writeError(w, http.StatusNotFound, "BLOB_UPLOAD_UNKNOWN", "upload session not found")
		return
	}

	// The PUT body may carry a final trailing chunk (the spec allows it).
	upload.mu.Lock()
	if req.ContentLength != 0 {
		if _, err := io.Copy(upload, req.Body); err != nil {
			upload.mu.Unlock()
			r.removeUpload(uploadID)
			writeError(w, http.StatusInternalServerError, "BLOB_UPLOAD_INVALID", err.Error())
			return
		}
	}
	actualDigest := "sha256:" + hex.EncodeToString(upload.digester.Sum(nil))
	upload.mu.Unlock()

	if actualDigest != digest {
		r.removeUpload(uploadID)
		writeError(w, http.StatusBadRequest, "DIGEST_INVALID",
			fmt.Sprintf("digest mismatch: client=%s actual=%s", digest, actualDigest))
		return
	}

	// Stream the temp file into the CAS, then clean up.
	if err := upload.streamToCAS(req.Context(), r.cas, digest); err != nil {
		r.removeUpload(uploadID)
		writeError(w, http.StatusInternalServerError, "BLOB_UPLOAD_INVALID", err.Error())
		return
	}
	r.removeUpload(uploadID)

	w.Header().Set("Location", "/v2/"+name+"/blobs/"+digest)
	w.Header().Set("Docker-Content-Digest", digest)
	w.WriteHeader(http.StatusCreated)
}

func (r *Registry) handleManifests(w http.ResponseWriter, req *http.Request, name string, tail []string) {
	reference := tail[0]
	switch req.Method {
	case http.MethodHead, http.MethodGet:
		r.getOrHeadManifest(w, req, reference, req.Method == http.MethodHead)
	case http.MethodPut:
		r.putManifest(w, req, name, reference)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (r *Registry) getOrHeadManifest(w http.ResponseWriter, req *http.Request, reference string, headOnly bool) {
	if !digestPattern.MatchString(reference) {
		// Tags are write-only in this implementation; grog always pulls by digest.
		writeError(w, http.StatusNotFound, "MANIFEST_UNKNOWN", "tag references are not supported, use a digest")
		return
	}

	exists, err := r.cas.Exists(req.Context(), reference)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	if !exists {
		writeError(w, http.StatusNotFound, "MANIFEST_UNKNOWN", "manifest not found")
		return
	}

	// Manifests are tiny (a few KB at most) so we can afford to load them
	// into memory in order to sniff the media type and report Content-Length.
	manifestBytes, err := r.cas.LoadBytes(req.Context(), reference)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	w.Header().Set("Docker-Content-Digest", reference)
	w.Header().Set("Content-Type", sniffManifestMediaType(manifestBytes))
	w.Header().Set("Content-Length", strconv.Itoa(len(manifestBytes)))
	w.WriteHeader(http.StatusOK)
	if !headOnly {
		_, _ = w.Write(manifestBytes)
	}
}

func (r *Registry) putManifest(w http.ResponseWriter, req *http.Request, name, reference string) {
	body, err := io.ReadAll(req.Body) // manifests are tiny, OK to buffer
	if err != nil {
		writeError(w, http.StatusBadRequest, "MANIFEST_INVALID", err.Error())
		return
	}
	sum := sha256.Sum256(body)
	digest := "sha256:" + hex.EncodeToString(sum[:])

	if err := r.cas.WriteBytes(req.Context(), digest, body); err != nil {
		writeError(w, http.StatusInternalServerError, "MANIFEST_INVALID", err.Error())
		return
	}

	r.manifestsMu.Lock()
	r.manifestsByName[name] = digest
	r.manifestsMu.Unlock()

	r.logger.Debugf("dockerproxy: stored manifest %s (reference %q under %q)", digest, reference, name)

	w.Header().Set("Docker-Content-Digest", digest)
	w.Header().Set("Location", "/v2/"+name+"/manifests/"+digest)
	w.WriteHeader(http.StatusCreated)
}

// LastManifestDigest returns the digest of the most recent manifest PUT
// against the given repository name, or "" if none has been received.
// Used by the docker output handler to discover the manifest digest the
// daemon produced for a push, since the daemon never tells us directly.
func (r *Registry) LastManifestDigest(name string) string {
	r.manifestsMu.Lock()
	defer r.manifestsMu.Unlock()
	return r.manifestsByName[name]
}

// ResetManifest forgets any recorded manifest digest for the given name.
// Call before initiating a push so a stale value cannot be observed if
// the new push fails before the manifest stage.
func (r *Registry) ResetManifest(name string) {
	r.manifestsMu.Lock()
	defer r.manifestsMu.Unlock()
	delete(r.manifestsByName, name)
}

// upload session management ---------------------------------------------------

func (r *Registry) openUpload() (string, error) {
	id := uuid.NewString()
	f, err := os.CreateTemp(r.tempDir, "upload-*")
	if err != nil {
		return "", err
	}
	upload := &pendingUpload{
		file:     f,
		digester: sha256.New(),
	}
	r.uploadsMu.Lock()
	r.uploads[id] = upload
	r.uploadsMu.Unlock()
	return id, nil
}

func (r *Registry) getUpload(id string) *pendingUpload {
	r.uploadsMu.Lock()
	defer r.uploadsMu.Unlock()
	return r.uploads[id]
}

func (r *Registry) removeUpload(id string) {
	r.uploadsMu.Lock()
	upload, ok := r.uploads[id]
	delete(r.uploads, id)
	r.uploadsMu.Unlock()
	if ok {
		upload.close()
	}
}

// Write implements io.Writer for streaming chunks into the upload session.
// Both the temp file and the running digester are advanced for the bytes
// that were actually persisted, never for bytes that failed to write.
func (u *pendingUpload) Write(p []byte) (int, error) {
	n, err := u.file.Write(p)
	if n > 0 {
		u.digester.Write(p[:n])
		u.written += int64(n)
	}
	return n, err
}

func (u *pendingUpload) streamToCAS(ctx context.Context, cas *caching.Cas, digest string) error {
	if _, err := u.file.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("seek upload temp file: %w", err)
	}
	return cas.Write(ctx, digest, u.file)
}

func (u *pendingUpload) close() {
	if u.file == nil {
		return
	}
	name := u.file.Name()
	_ = u.file.Close()
	_ = os.Remove(name)
	u.file = nil
}

// helpers --------------------------------------------------------------------

type errorBody struct {
	Errors []errorEntry `json:"errors"`
}

type errorEntry struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(errorBody{
		Errors: []errorEntry{{Code: code, Message: message}},
	})
}

// sniffManifestMediaType peeks at the manifest JSON to find the mediaType
// field. Defaults to the OCI manifest media type when unset.
func sniffManifestMediaType(body []byte) string {
	var probe struct {
		MediaType string `json:"mediaType"`
	}
	if err := json.Unmarshal(body, &probe); err == nil && probe.MediaType != "" {
		return probe.MediaType
	}
	return "application/vnd.oci.image.manifest.v1+json"
}
