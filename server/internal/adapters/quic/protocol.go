// Package quic implements the P2P data plane transport over QUIC with TLS 1.3.
package quic

import (
	"encoding/binary"
	"fmt"
	"io"
)

// Wire protocol: [1 cmd][4 bytes payload length (big-endian)][payload]
//           →    [1 status][4 bytes response length (big-endian)][response]

// Command types for the QUIC wire protocol.
const (
	CmdManifest byte = 0x01 // Request: repoSlug (string) → Response: SyncManifest
	CmdBundle   byte = 0x02 // Request: repoSlug (string) → Response: compressed CBOR bundle
	CmdDelta    byte = 0x03 // Request: {repoSlug, baseCommit} → Response: compressed CBOR delta
	CmdPush     byte = 0x04 // Request: compressed CBOR bundle → Response: SyncResult
)

// Status codes returned by the QUIC server.
const (
	StatusOK         byte = 0x00
	StatusNotFound   byte = 0x01
	StatusAuthFailed byte = 0x02
	StatusError      byte = 0x03
)

// DeltaRequest is the CBOR-encoded request for CmdDelta.
type DeltaRequest struct {
	RepoSlug   string `cbor:"1,keyasint"`
	BaseCommit string `cbor:"2,keyasint"`
}

// maxPayloadSize is 256 MB — safety limit to prevent OOM.
const maxPayloadSize = 256 * 1024 * 1024

// writeFrame writes [1 byte header][4 byte length][payload] to w.
func writeFrame(w io.Writer, header byte, payload []byte) error {
	if _, err := w.Write([]byte{header}); err != nil {
		return fmt.Errorf("write header: %w", err)
	}
	lenBuf := make([]byte, 4)
	binary.BigEndian.PutUint32(lenBuf, uint32(len(payload)))
	if _, err := w.Write(lenBuf); err != nil {
		return fmt.Errorf("write length: %w", err)
	}
	if len(payload) > 0 {
		if _, err := w.Write(payload); err != nil {
			return fmt.Errorf("write payload: %w", err)
		}
	}
	return nil
}

// readFrame reads [1 byte header][4 byte length][payload] from r.
func readFrame(r io.Reader) (header byte, payload []byte, err error) {
	hdr := make([]byte, 1)
	if _, err := io.ReadFull(r, hdr); err != nil {
		return 0, nil, fmt.Errorf("read header: %w", err)
	}
	lenBuf := make([]byte, 4)
	if _, err := io.ReadFull(r, lenBuf); err != nil {
		return 0, nil, fmt.Errorf("read length: %w", err)
	}
	length := binary.BigEndian.Uint32(lenBuf)
	if length > maxPayloadSize {
		return 0, nil, fmt.Errorf("payload too large: %d bytes", length)
	}
	payload = make([]byte, length)
	if length > 0 {
		if _, err := io.ReadFull(r, payload); err != nil {
			return 0, nil, fmt.Errorf("read payload: %w", err)
		}
	}
	return hdr[0], payload, nil
}
