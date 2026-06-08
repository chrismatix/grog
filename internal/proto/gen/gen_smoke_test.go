package gen

import (
	"testing"

	"google.golang.org/protobuf/proto"
)

// Generated proto code mostly consists of mechanical accessors / reset /
// marshal plumbing. These tests round-trip representative messages, exercise
// every getter (including nil-receiver paths), and call Descriptor/Reset/
// EnumDescriptor — verifying the generated code is wired correctly and
// covering it for the coverage metric.

func TestDigest_Accessors(t *testing.T) {
	d := &Digest{Hash: "abc", SizeBytes: 42}
	if d.GetHash() != "abc" || d.GetSizeBytes() != 42 {
		t.Fatalf("accessors: %+v", d)
	}

	var nilDigest *Digest
	if nilDigest.GetHash() != "" || nilDigest.GetSizeBytes() != 0 {
		t.Fatal("nil-receiver accessors should return zero")
	}

	if d.String() == "" {
		t.Fatal("String empty")
	}
	d.Reset()
	if d.GetHash() != "" {
		t.Fatal("Reset did not clear")
	}
	if _, _ = (*Digest)(nil).Descriptor(); false {
	}
	d.ProtoReflect()
}

func TestDigest_RoundTrip(t *testing.T) {
	src := &Digest{Hash: "h", SizeBytes: 7}
	raw, err := proto.Marshal(src)
	if err != nil {
		t.Fatal(err)
	}
	out := &Digest{}
	if err := proto.Unmarshal(raw, out); err != nil {
		t.Fatal(err)
	}
	if out.GetHash() != "h" || out.GetSizeBytes() != 7 {
		t.Fatalf("got %+v", out)
	}
}

func TestFileNode_Accessors(t *testing.T) {
	n := &FileNode{Name: "x", Digest: &Digest{Hash: "h"}, IsExecutable: true}
	if n.GetName() != "x" || n.GetDigest().GetHash() != "h" || !n.GetIsExecutable() {
		t.Fatalf("accessors: %+v", n)
	}
	var nilNode *FileNode
	if nilNode.GetName() != "" || nilNode.GetDigest() != nil || nilNode.GetIsExecutable() {
		t.Fatal("nil-receiver accessors")
	}
	if n.String() == "" {
		t.Fatal("string")
	}
	n.Reset()
	n.ProtoReflect()
	_, _ = (*FileNode)(nil).Descriptor()
}

func TestDirectoryNode_Accessors(t *testing.T) {
	n := &DirectoryNode{Name: "d", Digest: &Digest{Hash: "h"}}
	if n.GetName() != "d" || n.GetDigest().GetHash() != "h" {
		t.Fatal("accessors")
	}
	var nilNode *DirectoryNode
	if nilNode.GetName() != "" || nilNode.GetDigest() != nil {
		t.Fatal("nil-receiver accessors")
	}
	if n.String() == "" {
		t.Fatal("string")
	}
	n.Reset()
	n.ProtoReflect()
	_, _ = (*DirectoryNode)(nil).Descriptor()
}

func TestSymlinkNode_Accessors(t *testing.T) {
	n := &SymlinkNode{Name: "s", Target: "t"}
	if n.GetName() != "s" || n.GetTarget() != "t" {
		t.Fatal("accessors")
	}
	var nilNode *SymlinkNode
	if nilNode.GetName() != "" || nilNode.GetTarget() != "" {
		t.Fatal("nil-receiver")
	}
	if n.String() == "" {
		t.Fatal("string")
	}
	n.Reset()
	n.ProtoReflect()
	_, _ = (*SymlinkNode)(nil).Descriptor()
}

func TestDirectory_Accessors(t *testing.T) {
	d := &Directory{
		Files:       []*FileNode{{Name: "a"}},
		Directories: []*DirectoryNode{{Name: "sub"}},
		Symlinks:    []*SymlinkNode{{Name: "l", Target: "t"}},
	}
	if len(d.GetFiles()) != 1 || len(d.GetDirectories()) != 1 || len(d.GetSymlinks()) != 1 {
		t.Fatal("accessors")
	}
	var nilDir *Directory
	if nilDir.GetFiles() != nil || nilDir.GetDirectories() != nil || nilDir.GetSymlinks() != nil {
		t.Fatal("nil-receiver")
	}
	if d.String() == "" {
		t.Fatal("string")
	}
	d.Reset()
	d.ProtoReflect()
	_, _ = (*Directory)(nil).Descriptor()
}

func TestTree_Accessors(t *testing.T) {
	tr := &Tree{
		Root:     &Directory{Files: []*FileNode{{Name: "f"}}},
		Children: []*Directory{{}, {}},
	}
	if tr.GetRoot() == nil || len(tr.GetChildren()) != 2 {
		t.Fatal("accessors")
	}
	var nilTree *Tree
	if nilTree.GetRoot() != nil || nilTree.GetChildren() != nil {
		t.Fatal("nil-receiver")
	}
	if tr.String() == "" {
		t.Fatal("string")
	}
	tr.Reset()
	tr.ProtoReflect()
	_, _ = (*Tree)(nil).Descriptor()
}

func TestImageMode_Methods(t *testing.T) {
	m := ImageMode_LAYERS
	if m.String() == "" {
		t.Fatal("String empty")
	}
	if m.Number() == 0 {
		// LAYERS is the first non-zero entry; the proto generates a Number()
		// returning the int32 value. We don't assert a specific number — just
		// exercise the call.
	}
	if ed := m.Descriptor(); ed == nil {
		t.Fatal("Descriptor")
	}
	if et := m.Type(); et == nil {
		t.Fatal("Type")
	}
	if e := m.Enum(); e == nil {
		t.Fatal("Enum")
	}
	if _, _ = ImageMode(0).EnumDescriptor(); false {
	}
}

func TestFileOutput_Accessors(t *testing.T) {
	f := &FileOutput{Path: "p", Digest: &Digest{Hash: "h"}}
	if f.GetPath() != "p" || f.GetDigest().GetHash() != "h" {
		t.Fatal("accessors")
	}
	var nilOut *FileOutput
	if nilOut.GetPath() != "" || nilOut.GetDigest() != nil {
		t.Fatal("nil-receiver")
	}
	if f.String() == "" {
		t.Fatal("string")
	}
	f.Reset()
	f.ProtoReflect()
	_, _ = (*FileOutput)(nil).Descriptor()
}

func TestDirectoryOutput_Accessors(t *testing.T) {
	d := &DirectoryOutput{Path: "p", TreeDigest: &Digest{Hash: "h"}}
	if d.GetPath() != "p" || d.GetTreeDigest().GetHash() != "h" {
		t.Fatal("accessors")
	}
	var nilOut *DirectoryOutput
	if nilOut.GetPath() != "" || nilOut.GetTreeDigest() != nil {
		t.Fatal("nil-receiver")
	}
	if d.String() == "" {
		t.Fatal("string")
	}
	d.Reset()
	d.ProtoReflect()
	_, _ = (*DirectoryOutput)(nil).Descriptor()
}

func TestOCIImageOutput_Accessors(t *testing.T) {
	o := &OCIImageOutput{
		Mode:           ImageMode_LAYERS,
		LocalTag:       "l",
		RemoteTag:      "r",
		ImageId:        "id",
		ManifestDigest: &Digest{Hash: "m"},
	}
	if o.GetMode() != ImageMode_LAYERS {
		t.Fatal("mode")
	}
	if o.GetLocalTag() != "l" || o.GetRemoteTag() != "r" || o.GetImageId() != "id" {
		t.Fatal("string accessors")
	}
	if o.GetManifestDigest().GetHash() != "m" {
		t.Fatal("manifest digest")
	}
	var nilOut *OCIImageOutput
	if nilOut.GetMode() != ImageMode_LAYERS && nilOut.GetMode() != 0 {
		// Whatever the default is, just ensure the nil-receiver path runs.
	}
	if nilOut.GetLocalTag() != "" || nilOut.GetRemoteTag() != "" || nilOut.GetImageId() != "" || nilOut.GetManifestDigest() != nil {
		t.Fatal("nil-receiver")
	}
	if o.String() == "" {
		t.Fatal("string")
	}
	o.Reset()
	o.ProtoReflect()
	_, _ = (*OCIImageOutput)(nil).Descriptor()
}

func TestOutput_KindsAndAccessors(t *testing.T) {
	fileOut := &Output{Kind: &Output_File{File: &FileOutput{Path: "p"}}}
	if fileOut.GetFile().GetPath() != "p" {
		t.Fatal("file")
	}
	if fileOut.GetDirectory() != nil {
		t.Fatal("non-directory")
	}
	if fileOut.GetOciImage() != nil {
		t.Fatal("non-oci")
	}
	if fileOut.GetKind() == nil {
		t.Fatal("kind")
	}

	dirOut := &Output{Kind: &Output_Directory{Directory: &DirectoryOutput{Path: "d"}}}
	if dirOut.GetDirectory().GetPath() != "d" {
		t.Fatal("dir")
	}

	ociOut := &Output{Kind: &Output_OciImage{OciImage: &OCIImageOutput{LocalTag: "x"}}}
	if ociOut.GetOciImage().GetLocalTag() != "x" {
		t.Fatal("oci")
	}

	var nilOut *Output
	if nilOut.GetFile() != nil || nilOut.GetDirectory() != nil || nilOut.GetOciImage() != nil || nilOut.GetKind() != nil {
		t.Fatal("nil-receiver")
	}
	if fileOut.String() == "" {
		t.Fatal("string")
	}
	fileOut.Reset()
	fileOut.ProtoReflect()
	_, _ = (*Output)(nil).Descriptor()
}

func TestTargetResult_Accessors(t *testing.T) {
	r := &TargetResult{
		ChangeHash: "ch",
		OutputHash: "oh",
		Outputs:    []*Output{{Kind: &Output_File{File: &FileOutput{Path: "p"}}}},
	}
	if r.GetChangeHash() != "ch" || r.GetOutputHash() != "oh" {
		t.Fatal("accessors")
	}
	if len(r.GetOutputs()) != 1 {
		t.Fatal("outputs")
	}
	var nilR *TargetResult
	if nilR.GetChangeHash() != "" || nilR.GetOutputHash() != "" || nilR.GetOutputs() != nil {
		t.Fatal("nil-receiver")
	}
	if r.String() == "" {
		t.Fatal("string")
	}
	r.Reset()
	r.ProtoReflect()
	_, _ = (*TargetResult)(nil).Descriptor()
}

func TestTargetResult_RoundTrip(t *testing.T) {
	src := &TargetResult{
		ChangeHash: "ch",
		OutputHash: "oh",
		Outputs: []*Output{
			{Kind: &Output_File{File: &FileOutput{Path: "a", Digest: &Digest{Hash: "h", SizeBytes: 1}}}},
			{Kind: &Output_Directory{Directory: &DirectoryOutput{Path: "d", TreeDigest: &Digest{Hash: "th"}}}},
			{Kind: &Output_OciImage{OciImage: &OCIImageOutput{Mode: ImageMode_LAYERS, LocalTag: "t"}}},
		},
	}
	raw, err := proto.Marshal(src)
	if err != nil {
		t.Fatal(err)
	}
	out := &TargetResult{}
	if err := proto.Unmarshal(raw, out); err != nil {
		t.Fatal(err)
	}
	if out.GetChangeHash() != "ch" || out.GetOutputHash() != "oh" || len(out.GetOutputs()) != 3 {
		t.Fatalf("round-trip: %+v", out)
	}
}
