package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/commit0-dev/commit0/pkg/types"
	"github.com/commit0-dev/commit0/server/internal/domain"
)

// ── Fakes ─────────────────────────────────────────────────────────────────

type fakeExporter struct {
	bundle   *types.GraphBundle
	manifest *types.SyncManifest
	err      error
}

func (f *fakeExporter) ExportBundle(_ context.Context, _ string) (*types.GraphBundle, error) {
	if f.err != nil {
		return nil, f.err
	}
	if f.bundle != nil {
		return f.bundle, nil
	}
	return &types.GraphBundle{RepoSlug: "test/repo"}, nil
}

func (f *fakeExporter) ExportManifest(_ context.Context, _ string) (*types.SyncManifest, error) {
	if f.err != nil {
		return nil, f.err
	}
	if f.manifest != nil {
		return f.manifest, nil
	}
	return &types.SyncManifest{RepoSlug: "test/repo"}, nil
}

type fakeImporter struct {
	result *types.SyncResult
	err    error
}

func (f *fakeImporter) ImportBundle(_ context.Context, bundle *types.GraphBundle) (*types.SyncResult, error) {
	if f.err != nil {
		return nil, f.err
	}
	if f.result != nil {
		return f.result, nil
	}
	return &types.SyncResult{RepoSlug: bundle.RepoSlug, NodesImported: len(bundle.Nodes)}, nil
}

type fakeCodec struct {
	encoded []byte
	decoded *types.GraphBundle
	hash    string
	encErr  error
	decErr  error
	hashErr error
}

func (f *fakeCodec) Encode(_ *types.GraphBundle) ([]byte, error) {
	if f.encErr != nil {
		return nil, f.encErr
	}
	if f.encoded != nil {
		return f.encoded, nil
	}
	return []byte("bundle-bytes"), nil
}

func (f *fakeCodec) Decode(_ []byte) (*types.GraphBundle, error) {
	if f.decErr != nil {
		return nil, f.decErr
	}
	if f.decoded != nil {
		return f.decoded, nil
	}
	return &types.GraphBundle{RepoSlug: "decoded-repo"}, nil
}

func (f *fakeCodec) HashBundle(_ *types.GraphBundle) (string, error) {
	if f.hashErr != nil {
		return "", f.hashErr
	}
	if f.hash != "" {
		return f.hash, nil
	}
	return "abc123hashvalue", nil
}

type fakeAuth struct {
	sig     string
	signErr error
	verErr  error
}

func (f *fakeAuth) SignBundle(_ string) (string, error) {
	if f.signErr != nil {
		return "", f.signErr
	}
	return f.sig, nil
}

func (f *fakeAuth) VerifyBundle(_, _ string) error {
	return f.verErr
}

type fakePeerStore struct {
	peers []types.PeerInfo
	err   error
}

func (f *fakePeerStore) UpsertPeer(_ context.Context, p *types.PeerInfo) error { return nil }
func (f *fakePeerStore) GetPeer(_ context.Context, name string) (*types.PeerInfo, error) {
	if f.err != nil {
		return nil, f.err
	}
	for _, p := range f.peers {
		if p.Name == name {
			return &p, nil
		}
	}
	return nil, domain.NotFound("peer not found")
}
func (f *fakePeerStore) ListPeers(_ context.Context) ([]types.PeerInfo, error) {
	return f.peers, f.err
}
func (f *fakePeerStore) DeletePeer(_ context.Context, _ string) error { return nil }

type fakeScopeStore struct {
	inScope bool
	err     error
}

func (f *fakeScopeStore) AddToScope(_ context.Context, _ string) error    { return nil }
func (f *fakeScopeStore) RemoveFromScope(_ context.Context, _ string) error { return nil }
func (f *fakeScopeStore) ListScope(_ context.Context) ([]types.SyncScope, error) {
	return nil, nil
}
func (f *fakeScopeStore) IsInScope(_ context.Context, _ string) (bool, error) {
	return f.inScope, f.err
}

type fakeTransport struct {
	manifest *types.SyncManifest
	bundle   *types.GraphBundle
	result   *types.SyncResult
	err      error
}

func (f *fakeTransport) PullManifest(_ context.Context, _ *types.PeerInfo, _ string) (*types.SyncManifest, error) {
	return f.manifest, f.err
}
func (f *fakeTransport) PullBundle(_ context.Context, _ *types.PeerInfo, _ string) (*types.GraphBundle, error) {
	if f.err != nil {
		return nil, f.err
	}
	if f.bundle != nil {
		return f.bundle, nil
	}
	return &types.GraphBundle{RepoSlug: "test/repo"}, nil
}
func (f *fakeTransport) PullDelta(_ context.Context, _ *types.PeerInfo, _, _ string) (*types.SyncDelta, error) {
	return nil, nil
}
func (f *fakeTransport) PushBundle(_ context.Context, _ *types.PeerInfo, bundle *types.GraphBundle) (*types.SyncResult, error) {
	if f.err != nil {
		return nil, f.err
	}
	if f.result != nil {
		return f.result, nil
	}
	return &types.SyncResult{RepoSlug: bundle.RepoSlug}, nil
}
func (f *fakeTransport) Serve(_ context.Context, _ string, _ domain.PeerHandler) error { return nil }
func (f *fakeTransport) Close() error                                                   { return nil }

// ── helpers ───────────────────────────────────────────────────────────────

func newSyncService() *SyncService {
	return NewSyncService(
		&fakeExporter{},
		&fakeImporter{},
		&fakeCodec{},
		nil, // no auth
	)
}

// ── BuildBundle ───────────────────────────────────────────────────────────

func TestBuildBundle_HappyPath(t *testing.T) {
	svc := newSyncService()
	bundle, err := svc.BuildBundle(context.Background(), "owner/repo")
	if err != nil {
		t.Fatalf("BuildBundle error: %v", err)
	}
	if bundle.ContentHash == "" {
		t.Error("ContentHash should be set after BuildBundle")
	}
	if bundle.Signature != "" {
		t.Error("Signature should be empty when no auth configured")
	}
}

func TestBuildBundle_ExporterError(t *testing.T) {
	svc := NewSyncService(
		&fakeExporter{err: errors.New("export fail")},
		&fakeImporter{},
		&fakeCodec{},
		nil,
	)
	_, err := svc.BuildBundle(context.Background(), "owner/repo")
	if err == nil {
		t.Error("expected error from exporter")
	}
}

func TestBuildBundle_HashError(t *testing.T) {
	svc := NewSyncService(
		&fakeExporter{},
		&fakeImporter{},
		&fakeCodec{hashErr: errors.New("hash fail")},
		nil,
	)
	_, err := svc.BuildBundle(context.Background(), "owner/repo")
	if err == nil {
		t.Error("expected error from codec hash")
	}
}

func TestBuildBundle_WithAuth_SetsSignature(t *testing.T) {
	auth := &fakeAuth{sig: "mysignature"}
	svc := NewSyncService(&fakeExporter{}, &fakeImporter{}, &fakeCodec{hash: "deadbeef1234"}, auth)
	bundle, err := svc.BuildBundle(context.Background(), "owner/repo")
	if err != nil {
		t.Fatalf("BuildBundle: %v", err)
	}
	if bundle.Signature != "mysignature" {
		t.Errorf("Signature = %q, want mysignature", bundle.Signature)
	}
}

func TestBuildBundle_AuthSignError(t *testing.T) {
	auth := &fakeAuth{signErr: errors.New("sign fail")}
	svc := NewSyncService(&fakeExporter{}, &fakeImporter{}, &fakeCodec{}, auth)
	_, err := svc.BuildBundle(context.Background(), "owner/repo")
	if err == nil {
		t.Error("expected error from auth sign")
	}
}

// ── ExportToFile ──────────────────────────────────────────────────────────

func TestExportToFile_HappyPath(t *testing.T) {
	svc := newSyncService()
	path := filepath.Join(t.TempDir(), "bundle.cbor")
	err := svc.ExportToFile(context.Background(), "owner/repo", path)
	if err != nil {
		t.Fatalf("ExportToFile: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("file not created: %v", err)
	}
}

func TestExportToFile_EncodeError(t *testing.T) {
	svc := NewSyncService(&fakeExporter{}, &fakeImporter{}, &fakeCodec{encErr: errors.New("encode fail")}, nil)
	path := filepath.Join(t.TempDir(), "bundle.cbor")
	err := svc.ExportToFile(context.Background(), "owner/repo", path)
	if err == nil {
		t.Error("expected encode error")
	}
}

func TestExportToFile_WriteError(t *testing.T) {
	svc := newSyncService()
	// Try writing to a directory (path is a dir, not a file).
	err := svc.ExportToFile(context.Background(), "owner/repo", t.TempDir())
	if err == nil {
		t.Error("expected write error for directory path")
	}
}

// ── ImportFromFile ────────────────────────────────────────────────────────

func TestImportFromFile_HappyPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bundle.cbor")
	if err := os.WriteFile(path, []byte("fake-data"), 0644); err != nil {
		t.Fatal(err)
	}
	svc := newSyncService()
	result, err := svc.ImportFromFile(context.Background(), path)
	if err != nil {
		t.Fatalf("ImportFromFile: %v", err)
	}
	if result.Direction != "import" {
		t.Errorf("Direction = %q, want import", result.Direction)
	}
}

func TestImportFromFile_NoFile(t *testing.T) {
	svc := newSyncService()
	_, err := svc.ImportFromFile(context.Background(), "/no/such/file.cbor")
	if err == nil {
		t.Error("expected read error")
	}
}

func TestImportFromFile_DecodeError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.cbor")
	if err := os.WriteFile(path, []byte("data"), 0644); err != nil {
		t.Fatal(err)
	}
	svc := NewSyncService(&fakeExporter{}, &fakeImporter{}, &fakeCodec{decErr: errors.New("decode fail")}, nil)
	_, err := svc.ImportFromFile(context.Background(), path)
	if err == nil {
		t.Error("expected decode error")
	}
}

// ── ImportBundle ──────────────────────────────────────────────────────────

func TestImportBundle_HappyPath(t *testing.T) {
	codec := &fakeCodec{hash: "testhash"}
	svc := NewSyncService(&fakeExporter{}, &fakeImporter{}, codec, nil)
	bundle := &types.GraphBundle{RepoSlug: "owner/repo", ContentHash: "testhash"}
	result, err := svc.ImportBundle(context.Background(), bundle)
	if err != nil {
		t.Fatalf("ImportBundle: %v", err)
	}
	if result.Direction != "import" {
		t.Errorf("Direction = %q", result.Direction)
	}
}

func TestImportBundle_HashMismatch(t *testing.T) {
	codec := &fakeCodec{hash: "actualhash"}
	svc := NewSyncService(&fakeExporter{}, &fakeImporter{}, codec, nil)
	bundle := &types.GraphBundle{RepoSlug: "r", ContentHash: "expectednothash"}
	_, err := svc.ImportBundle(context.Background(), bundle)
	if err == nil {
		t.Error("expected hash mismatch error")
	}
}

func TestImportBundle_EmptyContentHash_Accepted(t *testing.T) {
	// Empty ContentHash means skip integrity check.
	svc := newSyncService()
	bundle := &types.GraphBundle{RepoSlug: "r", ContentHash: ""}
	result, err := svc.ImportBundle(context.Background(), bundle)
	if err != nil {
		t.Fatalf("empty hash should be accepted: %v", err)
	}
	_ = result
}

func TestImportBundle_SignatureVerifyFail(t *testing.T) {
	auth := &fakeAuth{verErr: errors.New("bad signature")}
	svc := NewSyncService(&fakeExporter{}, &fakeImporter{}, &fakeCodec{}, auth)
	bundle := &types.GraphBundle{
		RepoSlug:    "r",
		ContentHash: "abc123hashvalue",
		Signature:   "badsig",
	}
	_, err := svc.ImportBundle(context.Background(), bundle)
	if err == nil {
		t.Error("expected signature verify error")
	}
}

func TestImportBundle_ImporterError(t *testing.T) {
	svc := NewSyncService(&fakeExporter{}, &fakeImporter{err: errors.New("import fail")}, &fakeCodec{}, nil)
	bundle := &types.GraphBundle{RepoSlug: "r"}
	_, err := svc.ImportBundle(context.Background(), bundle)
	if err == nil {
		t.Error("expected importer error")
	}
}

func TestImportBundle_HashError(t *testing.T) {
	svc := NewSyncService(&fakeExporter{}, &fakeImporter{}, &fakeCodec{hashErr: errors.New("hash fail")}, nil)
	bundle := &types.GraphBundle{RepoSlug: "r"}
	_, err := svc.ImportBundle(context.Background(), bundle)
	if err == nil {
		t.Error("expected hash error")
	}
}

// ── ImportFromBytes ───────────────────────────────────────────────────────

func TestImportFromBytes_HappyPath(t *testing.T) {
	svc := newSyncService()
	result, err := svc.ImportFromBytes(context.Background(), []byte("raw-bytes"))
	if err != nil {
		t.Fatalf("ImportFromBytes: %v", err)
	}
	_ = result
}

func TestImportFromBytes_DecodeError(t *testing.T) {
	svc := NewSyncService(&fakeExporter{}, &fakeImporter{}, &fakeCodec{decErr: errors.New("dec fail")}, nil)
	_, err := svc.ImportFromBytes(context.Background(), []byte("bad"))
	if err == nil {
		t.Error("expected decode error")
	}
}

// ── Manifest ──────────────────────────────────────────────────────────────

func TestManifest_HappyPath(t *testing.T) {
	svc := newSyncService()
	m, err := svc.Manifest(context.Background(), "owner/repo")
	if err != nil {
		t.Fatalf("Manifest: %v", err)
	}
	if m.ContentHash == "" {
		t.Error("ContentHash should be populated by Manifest")
	}
}

func TestManifest_ExporterError(t *testing.T) {
	svc := NewSyncService(
		&fakeExporter{err: errors.New("export fail")},
		&fakeImporter{},
		&fakeCodec{},
		nil,
	)
	_, err := svc.Manifest(context.Background(), "r")
	if err == nil {
		t.Error("expected manifest exporter error")
	}
}

func TestManifest_BundleExportError_ReturnsPartialManifest(t *testing.T) {
	// First call (ExportManifest) succeeds, second call (ExportBundle) fails.
	// nolint:nilerr comment in the source says: return partial manifest without hash.
	callN := 0
	exp := &callCountExporter{
		onCall: func(n int) (*types.GraphBundle, error) {
			if n == 1 {
				return nil, errors.New("bundle fail")
			}
			return &types.GraphBundle{RepoSlug: "r"}, nil
		},
	}
	svc := NewSyncService(exp, &fakeImporter{}, &fakeCodec{}, nil)
	m, err := svc.Manifest(context.Background(), "r")
	if err != nil {
		t.Fatalf("expected partial manifest, got error: %v", err)
	}
	// ContentHash will be empty since bundle export failed.
	_ = m
	_ = callN
}

// callCountExporter tracks ExportBundle call count.
type callCountExporter struct {
	bundleCallN int
	onCall      func(n int) (*types.GraphBundle, error)
}

func (e *callCountExporter) ExportBundle(_ context.Context, _ string) (*types.GraphBundle, error) {
	e.bundleCallN++
	return e.onCall(e.bundleCallN)
}

func (e *callCountExporter) ExportManifest(_ context.Context, _ string) (*types.SyncManifest, error) {
	return &types.SyncManifest{RepoSlug: "r"}, nil
}

// ── Pull ──────────────────────────────────────────────────────────────────

func TestPull_NoTransport_Error(t *testing.T) {
	svc := newSyncService()
	_, err := svc.Pull(context.Background(), "peer1", "owner/repo")
	if err == nil {
		t.Error("expected error when transport not configured")
	}
}

func TestPull_OutOfScope_Error(t *testing.T) {
	svc := newSyncService()
	svc.SetTransport(&fakeTransport{}, &fakePeerStore{}, &fakeScopeStore{inScope: false})
	_, err := svc.Pull(context.Background(), "peer1", "owner/repo")
	if err == nil {
		t.Error("expected out-of-scope error")
	}
}

func TestPull_ScopeCheckError(t *testing.T) {
	svc := newSyncService()
	svc.SetTransport(&fakeTransport{}, &fakePeerStore{}, &fakeScopeStore{err: errors.New("scope fail")})
	_, err := svc.Pull(context.Background(), "peer1", "owner/repo")
	if err == nil {
		t.Error("expected scope check error")
	}
}

func TestPull_PeerNotFound(t *testing.T) {
	svc := newSyncService()
	svc.SetTransport(&fakeTransport{}, &fakePeerStore{err: errors.New("peer not found")}, &fakeScopeStore{inScope: true})
	_, err := svc.Pull(context.Background(), "noexist", "owner/repo")
	if err == nil {
		t.Error("expected peer not found error")
	}
}

func TestPull_AlreadyUpToDate(t *testing.T) {
	hash := "samehash"
	transport := &fakeTransport{
		manifest: &types.SyncManifest{ContentHash: hash},
	}
	exporter := &fakeExporter{manifest: &types.SyncManifest{ContentHash: hash}}
	svc := NewSyncService(exporter, &fakeImporter{}, &fakeCodec{hash: hash}, nil)
	svc.SetTransport(transport, &fakePeerStore{peers: []types.PeerInfo{{Name: "peer1"}}}, &fakeScopeStore{inScope: true})

	result, err := svc.Pull(context.Background(), "peer1", "owner/repo")
	if err != nil {
		t.Fatalf("Pull: %v", err)
	}
	if result.NodesImported != 0 {
		t.Errorf("already up-to-date should import 0 nodes, got %d", result.NodesImported)
	}
	if result.Direction != "pull" {
		t.Errorf("Direction = %q", result.Direction)
	}
}

// ── Push ──────────────────────────────────────────────────────────────────

func TestPush_NoTransport_Error(t *testing.T) {
	svc := newSyncService()
	_, err := svc.Push(context.Background(), "peer1", "owner/repo")
	if err == nil {
		t.Error("expected error when transport not configured")
	}
}

func TestPush_PeerNotFound(t *testing.T) {
	svc := newSyncService()
	svc.SetTransport(&fakeTransport{}, &fakePeerStore{err: errors.New("no peer")}, nil)
	_, err := svc.Push(context.Background(), "noexist", "owner/repo")
	if err == nil {
		t.Error("expected peer-not-found error")
	}
}

func TestPush_HappyPath(t *testing.T) {
	svc := newSyncService()
	svc.SetTransport(
		&fakeTransport{},
		&fakePeerStore{peers: []types.PeerInfo{{Name: "peer1"}}},
		nil,
	)
	result, err := svc.Push(context.Background(), "peer1", "owner/repo")
	if err != nil {
		t.Fatalf("Push: %v", err)
	}
	if result.Direction != "push" {
		t.Errorf("Direction = %q, want push", result.Direction)
	}
	if result.PeerName != "peer1" {
		t.Errorf("PeerName = %q", result.PeerName)
	}
}

// ── PeerHandler methods ───────────────────────────────────────────────────

func TestHandleManifestRequest(t *testing.T) {
	svc := newSyncService()
	m, err := svc.HandleManifestRequest(context.Background(), "r")
	if err != nil {
		t.Fatalf("HandleManifestRequest: %v", err)
	}
	_ = m
}

func TestHandleBundleRequest(t *testing.T) {
	svc := newSyncService()
	data, err := svc.HandleBundleRequest(context.Background(), "r")
	if err != nil {
		t.Fatalf("HandleBundleRequest: %v", err)
	}
	if len(data) == 0 {
		t.Error("expected non-empty bundle data")
	}
}

func TestHandleDeltaRequest_FallsBackToFullBundle(t *testing.T) {
	svc := newSyncService()
	data, err := svc.HandleDeltaRequest(context.Background(), "r", "abc123")
	if err != nil {
		t.Fatalf("HandleDeltaRequest: %v", err)
	}
	if len(data) == 0 {
		t.Error("expected non-empty data (full bundle fallback)")
	}
}

func TestHandlePushBundle(t *testing.T) {
	svc := newSyncService()
	result, err := svc.HandlePushBundle(context.Background(), []byte("raw"))
	if err != nil {
		t.Fatalf("HandlePushBundle: %v", err)
	}
	_ = result
}

// ── NotifyPeers ───────────────────────────────────────────────────────────

func TestNotifyPeers_NoPeersOrTransport_NoPanic(t *testing.T) {
	svc := newSyncService()
	// No transport or peers set — should be a no-op.
	svc.NotifyPeers(context.Background(), "r")
}

func TestNotifyPeers_WithPeers(t *testing.T) {
	svc := newSyncService()
	svc.SetTransport(
		&fakeTransport{},
		&fakePeerStore{peers: []types.PeerInfo{{Name: "peer1"}}},
		nil,
	)
	// Should not panic even when ExportManifest is called.
	svc.NotifyPeers(context.Background(), "r")
}

func TestNotifyPeers_ListPeersError_NoPanic(t *testing.T) {
	svc := newSyncService()
	svc.SetTransport(
		&fakeTransport{},
		&fakePeerStore{err: errors.New("list fail")},
		nil,
	)
	svc.NotifyPeers(context.Background(), "r")
}

// ── SetIndexService ───────────────────────────────────────────────────────

func TestSetIndexService_Nil(t *testing.T) {
	svc := newSyncService()
	svc.SetIndexService(nil) // should not panic
}
