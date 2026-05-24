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
	// DockerImages holds the target's docker outputs keyed by their output
	// identifier (the local image tag declared in the BUILD file, e.g.
	// "api:latest"). A target may declare more than one docker output.
	DockerImages map[string]DockerImage
}

// DockerImage describes a single docker image output produced by a target and
// stored content-addressed in grog's CAS.
type DockerImage struct {
	// Identifier is the output identifier / local tag from the BUILD file.
	Identifier string
	// ImageID is the sha256 of the image config (docker image ID).
	ImageID string
	// ManifestDigest is the OCI manifest digest, e.g. "sha256:abc…". This is
	// the immutable, content-addressed handle used to push the image to a
	// registry via Session.PushImage.
	ManifestDigest string
}

// newBuildResult projects a proto TargetResult onto the public BuildResult,
// extracting docker outputs into the identifier-keyed map.
func newBuildResult(targetLabel string, cacheHit bool, tr *gen.TargetResult) *BuildResult {
	res := &BuildResult{
		Label:        targetLabel,
		CacheHit:     cacheHit,
		DockerImages: make(map[string]DockerImage),
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
		res.DockerImages[identifier] = DockerImage{
			Identifier:     identifier,
			ImageID:        img.GetImageId(),
			ManifestDigest: manifestDigest,
		}
	}
	return res
}
