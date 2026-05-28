package session

import (
	"path/filepath"

	"grog/internal/label"
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
	// "api:latest"). The name is OCI- rather than Docker-specific because the
	// manifest is OCI-format regardless of which tool produced it.
	OCIImages map[string]OCIImage
	// Files holds the target's file outputs keyed by their package-relative
	// path (the identifier declared in the BUILD file's `outputs`, e.g.
	// "bin/app"). Use FileOutput.Path for the workspace-absolute path.
	Files map[string]FileOutput
	// Directories holds the target's directory outputs keyed by their
	// package-relative path. Use DirectoryOutput.Path for the workspace-
	// absolute path.
	Directories map[string]DirectoryOutput
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

// FileOutput describes a single file output produced by a target.
type FileOutput struct {
	// Path is the workspace-absolute path to the produced file. Re-derived from
	// the current workspace root on every read, so cached results stay valid
	// across checkouts at different paths.
	Path string
	// Digest is the file's content hash (algorithm per grog.toml).
	Digest string
	// IsExecutable reports whether grog marked the file executable (used for
	// `bin_output` targets).
	IsExecutable bool
}

// DirectoryOutput describes a single directory output produced by a target.
// grog hashes directories as Merkle trees so the digest covers every file
// inside.
type DirectoryOutput struct {
	// Path is the workspace-absolute path to the produced directory.
	Path string
	// Digest is the directory's Merkle tree digest.
	Digest string
}

// newBuildResult projects a proto TargetResult onto the public BuildResult,
// extracting OCI image, file, and directory outputs into identifier-keyed maps.
// targetLabel + workspaceRoot are used to compute workspace-absolute paths for
// file/dir outputs (the proto stores them package-relative).
func newBuildResult(targetLabel label.TargetLabel, cacheHit bool, tr *gen.TargetResult, workspaceRoot string) *BuildResult {
	res := &BuildResult{
		Label:       targetLabel.String(),
		CacheHit:    cacheHit,
		OCIImages:   make(map[string]OCIImage),
		Files:       make(map[string]FileOutput),
		Directories: make(map[string]DirectoryOutput),
	}
	if tr == nil {
		return res
	}
	res.ChangeHash = tr.GetChangeHash()
	res.OutputHash = tr.GetOutputHash()

	packageDir := filepath.Join(workspaceRoot, targetLabel.Package)

	for _, out := range tr.GetOutputs() {
		switch {
		case out.GetDockerImage() != nil:
			img := out.GetDockerImage()
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
		case out.GetFile() != nil:
			f := out.GetFile()
			relPath := f.GetPath()
			var digest string
			if d := f.GetDigest(); d != nil {
				digest = d.GetHash()
			}
			res.Files[relPath] = FileOutput{
				Path:         filepath.Join(packageDir, relPath),
				Digest:       digest,
				IsExecutable: f.GetIsExecutable(),
			}
		case out.GetDirectory() != nil:
			d := out.GetDirectory()
			relPath := d.GetPath()
			var digest string
			if td := d.GetTreeDigest(); td != nil {
				digest = td.GetHash()
			}
			res.Directories[relPath] = DirectoryOutput{
				Path:   filepath.Join(packageDir, relPath),
				Digest: digest,
			}
		}
	}
	return res
}
