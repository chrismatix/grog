package output

import (
	"strings"
	"testing"

	"grog/internal/label"
	"grog/internal/model"
	"grog/internal/proto/gen"
)

// makeDockerOutput is a one-liner used by these tests to build the proto
// shape the validator inspects. Production code populates these fields via
// the docker handler's Write path.
func makeDockerOutput(localTag, pushDest, imageID string) *gen.Output {
	return &gen.Output{
		Kind: &gen.Output_DockerImage{
			DockerImage: &gen.DockerImageOutput{
				LocalTag:        localTag,
				PushDestination: pushDest,
				ImageId:         imageID,
			},
		},
	}
}

func TestValidateOciPushImageIdentity_SingleOutput_OK(t *testing.T) {
	target := &model.Target{Label: label.TL("pkg", "tgt")}
	outputs := []*gen.Output{makeDockerOutput("a", "a", "sha256:abc")}
	if err := validateOciPushImageIdentity(target, outputs); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestValidateOciPushImageIdentity_MultipleMatching_OK(t *testing.T) {
	target := &model.Target{Label: label.TL("pkg", "tgt")}
	outputs := []*gen.Output{
		makeDockerOutput("a:1", "repo/a:1", "sha256:abc"),
		makeDockerOutput("b:1", "repo/b:1", "sha256:abc"),
		makeDockerOutput("c:1", "repo/c:1", "sha256:abc"),
	}
	if err := validateOciPushImageIdentity(target, outputs); err != nil {
		t.Fatalf("expected no error for matching image_ids, got %v", err)
	}
}

func TestValidateOciPushImageIdentity_Divergent_Errors(t *testing.T) {
	target := &model.Target{Label: label.TL("pkg", "tgt")}
	outputs := []*gen.Output{
		makeDockerOutput("a:1", "repo/a:1", "sha256:abc"),
		makeDockerOutput("b:1", "repo/b:1", "sha256:def"),
	}
	err := validateOciPushImageIdentity(target, outputs)
	if err == nil {
		t.Fatalf("expected divergent image_ids to error")
	}
	if !strings.Contains(err.Error(), "repo/a:1") || !strings.Contains(err.Error(), "repo/b:1") {
		t.Errorf("error %q should name both destinations", err.Error())
	}
}

func TestValidateOciPushImageIdentity_IgnoresPlainDocker(t *testing.T) {
	// A plain docker:: output (push_destination empty) sitting alongside a
	// single oci-push:: output should not be considered for the identity
	// check — only oci-push outputs need to match.
	target := &model.Target{Label: label.TL("pkg", "tgt")}
	outputs := []*gen.Output{
		makeDockerOutput("internal-cache", "", "sha256:111"),
		makeDockerOutput("push-tag", "repo/x:1", "sha256:222"),
	}
	if err := validateOciPushImageIdentity(target, outputs); err != nil {
		t.Fatalf("plain docker:: outputs should be ignored, got %v", err)
	}
}
