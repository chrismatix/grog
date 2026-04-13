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
	"errors"
	"fmt"
	"hash"
	"io"
	"net"
	"net/http"
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
	logger   *console.Logger

	// sessionCtx is a long-lived context that backs every chunked upload
	// session. It is *not* derived from any individual HTTP request — that
	// would be wrong, because cas.BeginWrite hands the context to a
	// background goroutine that lives across the POST/PATCH/.../PUT
	// boundary, and net/http cancels each request's context as soon as the
	// handler returns. sessionCtx is cancelled exactly once, by Close, so
	// any backend implementation that observes ctx cancellation (S3, GCS,
	// Azure) tears its in-flight upload down on shutdown.
	sessionCtx    context.Context
	sessionCancel context.CancelFunc

	uploadsMu sync.Mutex
	uploads   map[string]*pendingUpload

	// manifestsByName remembers the digest of the most recent manifest PUT
	// per repository name. The DockerOutputHandler reads this back after
	// `docker push` to learn the manifest digest the daemon produced.
	manifestsMu     sync.Mutex
	manifestsByName map[string]string
}

// pendingUpload tracks an in-flight chunked blob upload. Incoming PATCH bytes
// are streamed straight into a CAS staged writer (no temp file double copy)
// and a running SHA256 lets us validate the client-supplied digest at PUT
// time without re-reading anything.
type pendingUpload struct {
	mu       sync.Mutex
	writer   caching.StagedWriter
	digester hash.Hash
	written  int64
	finished bool // true after Commit/Cancel; further Writes are rejected
}

// New starts an OCI Distribution v2 server on a random loopback port.
// The caller is responsible for invoking Close() to shut it down.
func New(ctx context.Context, cas *caching.Cas) (*Registry, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("dockerproxy: listen: %w", err)
	}

	// Derive the session context from the caller's ctx so that cancelling
	// the parent (e.g. SIGINT during a build) tears down any in-flight
	// staged uploads. The cancel func is called explicitly from Close.
	sessionCtx, sessionCancel := context.WithCancel(ctx)

	r := &Registry{
		cas:             cas,
		listener:        listener,
		logger:          console.GetLogger(ctx),
		sessionCtx:      sessionCtx,
		sessionCancel:   sessionCancel,
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

// Close stops the HTTP server and cancels any pending uploads.
// Safe to call multiple times.
func (r *Registry) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = r.server.Shutdown(ctx)

	// Cancel the session context first. This propagates to any backend
	// staged-writer goroutines that block on network I/O (S3 uploader,
	// GCS resumable upload, Azure UploadStream); their failures bubble
	// through pending PATCH io.Copy loops and unblock the per-upload
	// mutexes so the cleanup loop below isn't held up by a slow PATCH.
	if r.sessionCancel != nil {
		r.sessionCancel()
	}

	r.uploadsMu.Lock()
	for id, u := range r.uploads {
		u.cancel(ctx)
		delete(r.uploads, id)
	}
	r.uploadsMu.Unlock()

	return nil
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
//
// Both code paths stream the body straight into a CAS staged writer — there is
// no temp file in either case. The chunked path opens the staged writer here
// and stores it in the session for subsequent PATCH/PUT to write into; the
// monolithic path opens, writes, verifies the digest, and commits in one go.
func (r *Registry) startBlobUpload(w http.ResponseWriter, req *http.Request, name string) {
	digest := req.URL.Query().Get("digest")

	if digest != "" {
		// Monolithic upload — the entire blob is in this request body.
		if !digestPattern.MatchString(digest) {
			writeError(w, http.StatusBadRequest, "DIGEST_INVALID", "invalid digest format")
			return
		}
		if err := r.writeBlobMonolithic(req.Context(), digest, req.Body); err != nil {
			writeError(w, http.StatusInternalServerError, "BLOB_UPLOAD_INVALID", err.Error())
			return
		}
		w.Header().Set("Location", "/v2/"+name+"/blobs/"+digest)
		w.Header().Set("Docker-Content-Digest", digest)
		w.WriteHeader(http.StatusCreated)
		return
	}

	// Chunked upload — open a staged writer up front so PATCH and PUT can
	// stream straight into it without buffering through a temp file. The
	// session uses the registry's long-lived ctx, NOT req.Context(): the
	// POST handler returns 202 immediately and net/http cancels its
	// request context, but the staged writer needs to keep accepting
	// bytes through subsequent PATCH/PUT requests.
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

// writeBlobMonolithic streams a single-request blob upload into the CAS and
// verifies the supplied digest at the end. Used by the rare monolithic POST
// path; chunked PATCH/PUT goes through the upload-session machinery instead.
func (r *Registry) writeBlobMonolithic(ctx context.Context, digest string, body io.Reader) error {
	sw, err := r.cas.BeginWrite(ctx)
	if err != nil {
		return fmt.Errorf("begin staged write: %w", err)
	}
	digester := sha256.New()
	tee := io.TeeReader(body, digester)
	if _, err := io.Copy(sw, tee); err != nil {
		_ = sw.Cancel(ctx)
		return fmt.Errorf("stream body to staged writer: %w", err)
	}
	actual := "sha256:" + hex.EncodeToString(digester.Sum(nil))
	if actual != digest {
		_ = sw.Cancel(ctx)
		return fmt.Errorf("digest mismatch: client=%s actual=%s", digest, actual)
	}
	if err := sw.Commit(ctx, "cas", digest); err != nil {
		return fmt.Errorf("commit blob: %w", err)
	}
	return nil
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
	if upload.finished {
		upload.mu.Unlock()
		writeError(w, http.StatusConflict, "BLOB_UPLOAD_INVALID", "upload session is closed")
		return
	}
	_, copyErr := io.Copy(upload, req.Body)
	written := upload.written
	upload.mu.Unlock()

	if copyErr != nil {
		// On a partial write the session is in an unknown state — cancel
		// the staged writer so the daemon starts over instead of trusting
		// a stale Range header. Cancel uses the session ctx (not the
		// request ctx) so cleanup survives a client disconnect mid-PATCH.
		r.cancelUpload(uploadID)
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

	// Take ownership of the session for the rest of the request — we either
	// commit or cancel before returning, and we don't want a concurrent PATCH
	// (which would be a protocol violation anyway) to race with us.
	upload.mu.Lock()
	if upload.finished {
		upload.mu.Unlock()
		writeError(w, http.StatusConflict, "BLOB_UPLOAD_INVALID", "upload session is closed")
		return
	}
	upload.finished = true

	// The PUT body may carry a final trailing chunk (the spec allows it).
	if req.ContentLength != 0 {
		if _, err := io.Copy(upload, req.Body); err != nil {
			upload.mu.Unlock()
			r.cancelUpload(uploadID)
			writeError(w, http.StatusInternalServerError, "BLOB_UPLOAD_INVALID", err.Error())
			return
		}
	}
	actualDigest := "sha256:" + hex.EncodeToString(upload.digester.Sum(nil))
	writer := upload.writer
	upload.mu.Unlock()

	// Always remove the upload from the registry's session map, regardless
	// of which branch we take below.
	r.dropUpload(uploadID)

	// Use the session ctx for Commit/Cancel — these run on a backend
	// goroutine that may outlive the request, and we don't want a client
	// disconnect to interrupt a final commit.
	if actualDigest != digest {
		_ = writer.Cancel(r.sessionCtx)
		writeError(w, http.StatusBadRequest, "DIGEST_INVALID",
			fmt.Sprintf("digest mismatch: client=%s actual=%s", digest, actualDigest))
		return
	}

	if err := writer.Commit(r.sessionCtx, "cas", digest); err != nil {
		writeError(w, http.StatusInternalServerError, "BLOB_UPLOAD_INVALID", err.Error())
		return
	}

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

// openUpload starts a new chunked upload session by opening a CAS staged
// writer up front. Subsequent PATCH calls stream their bodies straight into
// this writer, and PUT either Commits or Cancels it depending on whether the
// daemon-supplied digest matches the running SHA256.
//
// The staged writer is opened with the registry's long-lived sessionCtx so
// it survives the POST handler returning. Backends like S3/GCS/Azure run a
// background goroutine for the upload that observes ctx cancellation; using
// req.Context() here would tear the upload down before the first PATCH.
func (r *Registry) openUpload() (string, error) {
	sw, err := r.cas.BeginWrite(r.sessionCtx)
	if err != nil {
		return "", fmt.Errorf("open staged writer: %w", err)
	}
	upload := &pendingUpload{
		writer:   sw,
		digester: sha256.New(),
	}
	id := uuid.NewString()
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

// dropUpload removes the session from the registry's map without cancelling
// the staged writer — used by finishBlobUpload, which has already taken
// ownership of the writer and will Commit or Cancel it itself.
func (r *Registry) dropUpload(id string) {
	r.uploadsMu.Lock()
	delete(r.uploads, id)
	r.uploadsMu.Unlock()
}

// cancelUpload removes the session and cancels the underlying staged writer.
// Used when a PATCH fails partway through and the session can no longer be
// trusted. Cancellation always uses the session ctx so cleanup runs even
// when the triggering request has been cancelled by the client.
func (r *Registry) cancelUpload(id string) {
	r.uploadsMu.Lock()
	upload, ok := r.uploads[id]
	delete(r.uploads, id)
	r.uploadsMu.Unlock()
	if ok {
		upload.cancel(r.sessionCtx)
	}
}

// Write implements io.Writer for streaming chunks into the upload session's
// staged writer. The running digester is advanced for the bytes that were
// actually persisted, never for bytes that failed to write.
func (u *pendingUpload) Write(p []byte) (int, error) {
	if u.writer == nil {
		return 0, errors.New("upload session: write after commit/cancel")
	}
	n, err := u.writer.Write(p)
	if n > 0 {
		u.digester.Write(p[:n])
		u.written += int64(n)
	}
	return n, err
}

// cancel discards the staged writer. Safe to call from any goroutine; the
// caller is responsible for serialising with concurrent Writes (the registry
// achieves this by removing the session from its map before calling cancel).
func (u *pendingUpload) cancel(ctx context.Context) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.writer == nil {
		return
	}
	_ = u.writer.Cancel(ctx)
	u.writer = nil
	u.finished = true
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
