package session

import (
	"grog/internal/proto/gen"
)

// BuildResult is the structured outcome of building a single target, suitable
// for consumption by embedders such as the Terraform provider.
type BuildResult struct {
	// Label is the fully-qualified target label, e.g. "//services/api:image".
	Label string
	// ChangeHash is grog's content hash of the target definition + inputs +
	// dependency output hashes. It is the cache key for this target.
	ChangeHash string
	// OutputHash is grog's hash of the produced outputs.
	OutputHash string
	// CacheHit reports whether the target was served from cache rather than
	// freshly executed.
	CacheHit bool
	// OCIImages holds the target's container image outputs keyed by their output
	// identifier (the local image tag declared in the BUILD file, e.g.
	// "api:latest"). A target may declare more than one image output. The name
	// is OCI- rather than Docker-specific because the manifest is OCI-format
	// regardless of which tool produced it.
	OCIImages map[string]OCIImage
}

// OCIImage describes a single container image output produced by a target and
// stored content-addressed in grog's CAS.
type OCIImage struct {
	// Identifier is the output identifier / local tag from the BUILD file.
	Identifier string
	// ImageID is the sha256 of the image config.
	ImageID string
	// ManifestDigest is the OCI manifest digest, e.g. "sha256:abc…". This is
	// the immutable, content-addressed handle used to push the image to a
	// registry via Session.PushImage.
	ManifestDigest string
}

// newBuildResult projects a proto TargetResult onto the public BuildResult,
// extracting OCI image outputs into the identifier-keyed map.
func newBuildResult(targetLabel string, cacheHit bool, tr *gen.TargetResult) *BuildResult {
	res := &BuildResult{
		Label:     targetLabel,
		CacheHit:  cacheHit,
		OCIImages: make(map[string]OCIImage),
	}
	if tr == nil {
		return res
	}
	res.ChangeHash = tr.GetChangeHash()
	res.OutputHash = tr.GetOutputHash()

	for _, out := range tr.GetOutputs() {
		img := out.GetDockerImage()
		if img == nil {
			continue
		}
		identifier := img.GetLocalTag()
		var manifestDigest string
		if d := img.GetManifestDigest(); d != nil {
			manifestDigest = d.GetHash()
		}
		res.OCIImages[identifier] = OCIImage{
			Identifier:     identifier,
			ImageID:        img.GetImageId(),
			ManifestDigest: manifestDigest,
		}
	}
	return res
}
