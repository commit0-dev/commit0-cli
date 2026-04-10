// Package quic implements the P2P data plane transport over QUIC with TLS 1.3 PSK.
package quic

// Command types for the QUIC wire protocol.
// Each QUIC stream carries: [1 byte command][CBOR payload] → [1 byte status][CBOR response].
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
