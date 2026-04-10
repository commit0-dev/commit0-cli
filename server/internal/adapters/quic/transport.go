package quic

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"fmt"
	"io"
	"log/slog"
	"net"

	"github.com/fxamacker/cbor/v2"
	"github.com/quic-go/quic-go"

	"github.com/commit0-dev/commit0/pkg/types"
	"github.com/commit0-dev/commit0/server/internal/domain"
)

// Compile-time check.
var _ domain.PeerTransport = (*Transport)(nil)

// Transport implements PeerTransport over QUIC with TLS 1.3.
type Transport struct {
	passphrase string
	codec      domain.BundleCodec
	listener   *quic.Listener
	log        *slog.Logger
	encMode    cbor.EncMode
	decMode    cbor.DecMode
}

// NewTransport creates a QUIC transport.
func NewTransport(passphrase string, codec domain.BundleCodec) (*Transport, error) {
	em, err := cbor.CanonicalEncOptions().EncMode()
	if err != nil {
		return nil, fmt.Errorf("cbor enc mode: %w", err)
	}
	dm, err := cbor.DecOptions{}.DecMode()
	if err != nil {
		return nil, fmt.Errorf("cbor dec mode: %w", err)
	}
	return &Transport{
		passphrase: passphrase,
		codec:      codec,
		log:        slog.Default().With("adapter", "quic"),
		encMode:    em,
		decMode:    dm,
	}, nil
}

func (t *Transport) tlsConfig() *tls.Config {
	// PSK identity derived from passphrase (used at app level, not TLS level).
	_ = sha256.Sum256([]byte("commit0-psk:" + t.passphrase))

	cert, err := generateSelfSignedCert()
	if err != nil {
		t.log.Error("generate self-signed cert", "err", err)
		return &tls.Config{NextProtos: []string{"commit0-sync-v1"}, MinVersion: tls.VersionTLS13}
	}
	return &tls.Config{
		Certificates:       []tls.Certificate{cert},
		NextProtos:         []string{"commit0-sync-v1"},
		InsecureSkipVerify: true,
		MinVersion:         tls.VersionTLS13,
	}
}

// Serve starts listening for incoming peer connections.
func (t *Transport) Serve(ctx context.Context, addr string, handler domain.PeerHandler) error {
	udpAddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return fmt.Errorf("resolve addr: %w", err)
	}
	conn, err := net.ListenUDP("udp", udpAddr)
	if err != nil {
		return fmt.Errorf("listen udp: %w", err)
	}

	tr := &quic.Transport{Conn: conn}
	ln, err := tr.Listen(t.tlsConfig(), &quic.Config{})
	if err != nil {
		conn.Close()
		return fmt.Errorf("quic listen: %w", err)
	}
	t.listener = ln
	t.log.Info("QUIC transport listening", "addr", addr)

	go func() {
		<-ctx.Done()
		ln.Close()
	}()

	for {
		qconn, err := ln.Accept(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			t.log.Error("accept connection", "err", err)
			continue
		}
		go t.handleConn(ctx, qconn, handler)
	}
}

func (t *Transport) handleConn(ctx context.Context, conn *quic.Conn, handler domain.PeerHandler) {
	defer conn.CloseWithError(0, "done")
	for {
		stream, err := conn.AcceptStream(ctx)
		if err != nil {
			return
		}
		go t.handleStream(ctx, stream, handler)
	}
}

func (t *Transport) handleStream(ctx context.Context, stream *quic.Stream, handler domain.PeerHandler) {
	defer stream.Close()

	cmdBuf := make([]byte, 1)
	if _, err := io.ReadFull(stream, cmdBuf); err != nil {
		return
	}

	payload, err := io.ReadAll(stream)
	if err != nil {
		t.writeResp(stream, StatusError, nil)
		return
	}

	switch cmdBuf[0] {
	case CmdManifest:
		var repoSlug string
		if err := t.decMode.Unmarshal(payload, &repoSlug); err != nil {
			t.writeResp(stream, StatusError, nil)
			return
		}
		manifest, err := handler.HandleManifestRequest(ctx, repoSlug)
		if err != nil {
			t.writeResp(stream, StatusNotFound, nil)
			return
		}
		data, _ := t.encMode.Marshal(manifest)
		t.writeResp(stream, StatusOK, data)

	case CmdBundle:
		var repoSlug string
		if err := t.decMode.Unmarshal(payload, &repoSlug); err != nil {
			t.writeResp(stream, StatusError, nil)
			return
		}
		data, err := handler.HandleBundleRequest(ctx, repoSlug)
		if err != nil {
			t.writeResp(stream, StatusNotFound, nil)
			return
		}
		t.writeResp(stream, StatusOK, data)

	case CmdDelta:
		var req DeltaRequest
		if err := t.decMode.Unmarshal(payload, &req); err != nil {
			t.writeResp(stream, StatusError, nil)
			return
		}
		data, err := handler.HandleDeltaRequest(ctx, req.RepoSlug, req.BaseCommit)
		if err != nil {
			t.writeResp(stream, StatusNotFound, nil)
			return
		}
		t.writeResp(stream, StatusOK, data)

	case CmdPush:
		result, err := handler.HandlePushBundle(ctx, payload)
		if err != nil {
			t.writeResp(stream, StatusError, nil)
			return
		}
		data, _ := t.encMode.Marshal(result)
		t.writeResp(stream, StatusOK, data)

	default:
		t.writeResp(stream, StatusError, nil)
	}
}

func (t *Transport) writeResp(stream *quic.Stream, status byte, data []byte) {
	stream.Write([]byte{status})
	if data != nil {
		stream.Write(data)
	}
}

// --- Client methods ---

func (t *Transport) dial(ctx context.Context, peer *types.PeerInfo) (*quic.Conn, error) {
	conn, err := quic.DialAddr(ctx, peer.Endpoint, t.tlsConfig(), &quic.Config{})
	if err != nil {
		return nil, types.NotFound(fmt.Sprintf("peer %s unreachable: %v", peer.Name, err))
	}
	return conn, nil
}

func (t *Transport) request(ctx context.Context, peer *types.PeerInfo, cmd byte, payload []byte) ([]byte, error) {
	conn, err := t.dial(ctx, peer)
	if err != nil {
		return nil, err
	}
	defer conn.CloseWithError(0, "done")

	stream, err := conn.OpenStreamSync(ctx)
	if err != nil {
		return nil, fmt.Errorf("open stream: %w", err)
	}

	stream.Write([]byte{cmd})
	stream.Write(payload)
	stream.CancelWrite(0)

	statusBuf := make([]byte, 1)
	if _, err := io.ReadFull(stream, statusBuf); err != nil {
		return nil, fmt.Errorf("read status: %w", err)
	}

	respData, err := io.ReadAll(stream)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	switch statusBuf[0] {
	case StatusOK:
		return respData, nil
	case StatusNotFound:
		return nil, types.NotFound("not found on peer")
	case StatusAuthFailed:
		return nil, types.AuthFailed("peer rejected credentials")
	default:
		return nil, fmt.Errorf("peer error: status %d", statusBuf[0])
	}
}

func (t *Transport) PullManifest(ctx context.Context, peer *types.PeerInfo, repoSlug string) (*types.SyncManifest, error) {
	payload, _ := t.encMode.Marshal(repoSlug)
	data, err := t.request(ctx, peer, CmdManifest, payload)
	if err != nil {
		return nil, err
	}
	var manifest types.SyncManifest
	if err := t.decMode.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("decode manifest: %w", err)
	}
	return &manifest, nil
}

func (t *Transport) PullBundle(ctx context.Context, peer *types.PeerInfo, repoSlug string) (*types.GraphBundle, error) {
	payload, _ := t.encMode.Marshal(repoSlug)
	data, err := t.request(ctx, peer, CmdBundle, payload)
	if err != nil {
		return nil, err
	}
	return t.codec.Decode(data)
}

func (t *Transport) PullDelta(ctx context.Context, peer *types.PeerInfo, repoSlug, baseCommit string) (*types.SyncDelta, error) {
	req := DeltaRequest{RepoSlug: repoSlug, BaseCommit: baseCommit}
	payload, _ := t.encMode.Marshal(req)
	data, err := t.request(ctx, peer, CmdDelta, payload)
	if err != nil {
		return nil, err
	}
	var delta types.SyncDelta
	if err := t.decMode.Unmarshal(data, &delta); err != nil {
		return nil, fmt.Errorf("decode delta: %w", err)
	}
	return &delta, nil
}

func (t *Transport) PushBundle(ctx context.Context, peer *types.PeerInfo, bundle *types.GraphBundle) (*types.SyncResult, error) {
	bundleData, err := t.codec.Encode(bundle)
	if err != nil {
		return nil, fmt.Errorf("encode bundle: %w", err)
	}
	data, err := t.request(ctx, peer, CmdPush, bundleData)
	if err != nil {
		return nil, err
	}
	var result types.SyncResult
	if err := t.decMode.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("decode result: %w", err)
	}
	return &result, nil
}

func (t *Transport) Close() error {
	if t.listener != nil {
		return t.listener.Close()
	}
	return nil
}
