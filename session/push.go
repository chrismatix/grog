package session

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"sync/atomic"

	"github.com/alitto/pond/v2"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/remote/transport"

	"grog/internal/caching"
	"grog/internal/config"
	"grog/internal/console"
	"grog/internal/dockerproxy"
)

var digestRe = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)

// pushTagWorkers bounds concurrent HEAD/Write operations against the
// destination registry during a multi-tag push. Small enough not to swamp the
// registry, large enough to keep latency low when many tags are requested.
const pushTagWorkers = 4

// PushOptions describes a request to push a built image to an external registry.
type PushOptions struct {
	// ManifestDigest is the OCI manifest digest of a built image, e.g.
	// "sha256:abc…" — typically BuildResult.DockerImages[x].ManifestDigest.
	ManifestDigest string
	// Repository is the destination repository without a tag, e.g.
	// "us-docker.pkg.dev/project/repo/api".
	Repository string
	// Tags are optional human-readable tags to also point at the digest (e.g.
	// "v1", "latest"). The digest reference is always the canonical output.
	Tags []string
}

// PushResult is the outcome of PushImage.
type PushResult struct {
	// Reference is the immutable pinned reference, "<repository>@<digest>".
	Reference string
	// Digest is the manifest digest that was pushed.
	Digest string
	// Tags are the fully-qualified tag references that now point at the digest.
	Tags []string
	// Skipped is true when the destination already contained the digest (and
	// all requested tags), so no blobs were transferred.
	Skipped bool
}

// PushImage copies an image identified by its manifest digest from grog's
// content-addressable store to an external registry, without going through the
// local Docker daemon. Authentication uses the ambient Docker keychain
// (~/.docker/config.json and credential helpers, e.g. gcloud/ECR/GAR).
//
// It is convergent: if the destination already has the digest (and any
// requested tags), it is a no-op. Only the default "fs" docker backend is
// supported; with the "registry" backend the image already lives in a remote
// registry and should be copied with standard tooling.
func (s *Session) PushImage(ctx context.Context, opts PushOptions) (*PushResult, error) {
	if config.Global.Docker.Backend == config.DockerBackendRegistry {
		return nil, fmt.Errorf("session: PushImage is only supported with the fs docker backend; " +
			"with the registry backend the image already resides in a remote registry")
	}

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil, fmt.Errorf("session: closed")
	}
	cas := s.cas
	s.mu.Unlock()

	// Route logs through the session-injected logger (e.g. tflog under the
	// Terraform provider) rather than the default.
	ctx = console.WithLogger(ctx, s.logger)
	return pushFromCAS(ctx, cas, authn.DefaultKeychain, opts)
}

// ImageExists reports whether the given manifest digest is present at the
// destination repository. Used for drift detection: a true 404 reports the
// image as missing, but transient / auth / 5xx errors are returned so callers
// (e.g. grog_image_push.Read) can keep state instead of churning during a
// registry outage.
func (s *Session) ImageExists(ctx context.Context, repository, manifestDigest string) (bool, error) {
	if !digestRe.MatchString(manifestDigest) {
		return false, fmt.Errorf("session: invalid manifest digest %q", manifestDigest)
	}
	if repository == "" {
		return false, fmt.Errorf("session: repository is required")
	}
	ref, err := name.NewDigest(repository + "@" + manifestDigest)
	if err != nil {
		return false, fmt.Errorf("session: invalid destination %q: %w", repository, err)
	}
	_, err = remote.Head(ref, remote.WithAuthFromKeychain(authn.DefaultKeychain), remote.WithContext(ctx))
	if err == nil {
		return true, nil
	}
	if isNotFound(err) {
		return false, nil
	}
	return false, fmt.Errorf("session: HEAD %s: %w", ref, err)
}

// isNotFound reports whether a go-containerregistry error indicates the
// manifest/blob is genuinely absent (HTTP 404 / MANIFEST_UNKNOWN etc.) rather
// than a transient network or auth failure.
func isNotFound(err error) bool {
	var tErr *transport.Error
	if errors.As(err, &tErr) {
		return tErr.StatusCode == http.StatusNotFound
	}
	return false
}

// pushFromCAS contains the registry-movement logic, decoupled from Session so it
// can be exercised directly in tests with a hand-built CAS and keychain.
func pushFromCAS(ctx context.Context, cas *caching.Cas, keychain authn.Keychain, opts PushOptions) (*PushResult, error) {
	if !digestRe.MatchString(opts.ManifestDigest) {
		return nil, fmt.Errorf("session: invalid manifest digest %q (want sha256:<64-hex>)", opts.ManifestDigest)
	}
	if opts.Repository == "" {
		return nil, fmt.Errorf("session: Repository is required")
	}

	logger := console.GetLogger(ctx)
	authOpt := remote.WithAuthFromKeychain(keychain)

	dstDigestRef, err := name.NewDigest(opts.Repository + "@" + opts.ManifestDigest)
	if err != nil {
		return nil, fmt.Errorf("session: invalid destination %q: %w", opts.Repository, err)
	}

	// Pre-compute tag refs in order so result.Tags is deterministic regardless
	// of how the parallel network work below races.
	tagRefs := make([]name.Tag, 0, len(opts.Tags))
	for _, tag := range opts.Tags {
		tagRef, terr := name.NewTag(opts.Repository + ":" + tag)
		if terr != nil {
			return nil, fmt.Errorf("session: invalid tag %q: %w", tag, terr)
		}
		tagRefs = append(tagRefs, tagRef)
	}
	tagStrs := make([]string, len(tagRefs))
	for i, t := range tagRefs {
		tagStrs[i] = t.String()
	}

	result := &PushResult{
		Reference: dstDigestRef.String(),
		Digest:    opts.ManifestDigest,
		Tags:      tagStrs,
	}

	// Convergence: is the digest already at the destination? Treat any non-404
	// failure as "not present but worth flagging" — we can still push (which
	// will surface the real error if it's persistent), but the user should see
	// that the convergence check failed.
	digestPresent := false
	if _, err := remote.Head(dstDigestRef, authOpt, remote.WithContext(ctx)); err == nil {
		digestPresent = true
	} else if !isNotFound(err) {
		logger.Warnf("session: HEAD %s during convergence check failed (%v); will attempt to push", dstDigestRef, err)
	}

	// Resolve the image we want to publish. Prefer the destination (avoids
	// touching CAS) when the digest is already there; otherwise serve it out of
	// CAS via a short-lived loopback OCI registry.
	var img v1.Image
	if digestPresent {
		img, err = remote.Image(dstDigestRef, authOpt, remote.WithContext(ctx))
		if err != nil {
			return nil, fmt.Errorf("session: reading existing image %s: %w", dstDigestRef, err)
		}
	} else {
		proxy, perr := dockerproxy.New(ctx, cas)
		if perr != nil {
			return nil, fmt.Errorf("session: starting loopback registry: %w", perr)
		}
		defer func() { _ = proxy.Close() }()

		srcRef, serr := name.NewDigest(proxy.Addr()+"/grog@"+opts.ManifestDigest, name.Insecure)
		if serr != nil {
			return nil, fmt.Errorf("session: building source ref: %w", serr)
		}
		img, err = remote.Image(srcRef, remote.WithContext(ctx))
		if err != nil {
			return nil, fmt.Errorf("session: reading image %s from CAS: %w", opts.ManifestDigest, err)
		}
		if err := remote.Write(dstDigestRef, img, authOpt, remote.WithContext(ctx)); err != nil {
			return nil, fmt.Errorf("session: pushing %s: %w", dstDigestRef, err)
		}
	}

	// Ensure each requested tag points at the digest. Convergent per tag, and
	// run in parallel through grog's worker-pool library so many tags don't
	// serialize on registry latency.
	var allTagsPresent atomic.Bool
	allTagsPresent.Store(true)

	pool := pond.NewPool(pushTagWorkers)
	defer pool.StopAndWait()
	group := pool.NewGroupContext(ctx)
	for _, tagRef := range tagRefs {
		group.SubmitErr(func() error {
			if desc, herr := remote.Head(tagRef, authOpt, remote.WithContext(ctx)); herr == nil &&
				desc.Digest.String() == opts.ManifestDigest {
				return nil // tag already resolves to the digest
			} else if herr != nil && !isNotFound(herr) {
				logger.Warnf("session: HEAD %s failed (%v); will attempt to write the tag", tagRef, herr)
			}
			allTagsPresent.Store(false)
			if werr := remote.Write(tagRef, img, authOpt, remote.WithContext(ctx)); werr != nil {
				return fmt.Errorf("session: tagging %s: %w", tagRef, werr)
			}
			return nil
		})
	}
	if err := group.Wait(); err != nil {
		return nil, err
	}

	result.Skipped = digestPresent && allTagsPresent.Load()
	return result, nil
}
