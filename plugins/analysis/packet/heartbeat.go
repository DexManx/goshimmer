package packet

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"strings"

	"github.com/iotaledger/hive.go/core/protocol/message"
	"github.com/iotaledger/hive.go/core/protocol/tlv"
	"github.com/pkg/errors"
)

var (
	// ErrInvalidHeartbeat is returned for invalid heartbeats.
	ErrInvalidHeartbeat = errors.New("invalid heartbeat")
	// ErrEmptyNetworkVersion is returned for packets not containing a network ID.
	ErrEmptyNetworkVersion = errors.New("empty network version in heartbeat")
	// ErrInvalidHeartbeatNetworkVersion is returned for malformed network version.
	ErrInvalidHeartbeatNetworkVersion = errors.New("wrong or missing network version in packet")
)

const (
	// HeartbeatMaxOutboundPeersCount is the maximum amount of outbound peer IDs a heartbeat packet can contain.
	HeartbeatMaxOutboundPeersCount = 4
	// HeartbeatMaxInboundPeersCount is the maximum amount of inbound peer IDs a heartbeat packet can contain.
	HeartbeatMaxInboundPeersCount = 4
	// HeartbeatPacketPeerIDSize is the byte size of peer IDs within the heartbeat packet.
	HeartbeatPacketPeerIDSize = sha256.Size
	// HeartbeatPacketOutboundIDCountSize is the byte size of the counter indicating the amount of outbound IDs.
	HeartbeatPacketOutboundIDCountSize = 1
	// HeartbeatPacketNetworkIDBytesCountSize is byte size of the counter indicating the amount of networkID bytes.
	HeartbeatPacketNetworkIDBytesCountSize = 1
	// HeartbeatPacketMaxNetworkIDBytesSize is the maximum length of network ID string in bytes.
	// 10 bytes should be enough for vXX.XX.XXX
	HeartbeatPacketMaxNetworkIDBytesSize = 10
	// HeartbeatPacketMinSize is the minimum byte size of a heartbeat packet.
	HeartbeatPacketMinSize = HeartbeatPacketNetworkIDBytesCountSize + HeartbeatPacketPeerIDSize + HeartbeatPacketOutboundIDCountSize
	// HeartbeatPacketMaxSize is the maximum size a heartbeat packet can have.
	HeartbeatPacketMaxSize = HeartbeatPacketNetworkIDBytesCountSize + HeartbeatPacketMaxNetworkIDBytesSize +
		HeartbeatPacketPeerIDSize + HeartbeatPacketOutboundIDCountSize +
		HeartbeatMaxOutboundPeersCount*sha256.Size + HeartbeatMaxInboundPeersCount*sha256.Size
)

// Heartbeat represents a heartbeat packet.
type Heartbeat struct {
	// NetworkID is the id of the network the node participates in. For example, "v0.2.2".
	NetworkID []byte
	// The ID of the node who sent the heartbeat.
	// Must be contained when a heartbeat is serialized.
	OwnID []byte
	// The IDs of the outbound peers. Can be empty or nil.
	// It must not exceed HeartbeatMaxOutboundPeersCount.
	OutboundIDs [][]byte
	// The IDs of the inbound peers. Can be empty or nil.
	// It must not exceed HeartbeatMaxInboundPeersCount.
	InboundIDs [][]byte
}

// HeartBeatBlockDefinition gets the heartbeatBlockDefinition.
func HeartBeatBlockDefinition() *message.Definition {
	// heartbeatBlockDefinition defines a heartbeat block's format.
	var heartbeatBlockDefinition *message.Definition
	heartBeatOnce.Do(func() {
		heartbeatBlockDefinition = &message.Definition{
			ID:             MessageTypeHeartbeat,
			MaxBytesLength: uint16(HeartbeatPacketMaxSize),
			VariableLength: true,
		}
	})
	return heartbeatBlockDefinition
}

// ParseHeartbeat parses a slice of bytes (serialized packet) into a heartbeat.
func ParseHeartbeat(data []byte) (*Heartbeat, error) {
	// check minimum size
	if len(data) < HeartbeatPacketMinSize {
		return nil, errors.WithMessagef(ErrMalformedPacket, "packet doesn't reach minimum heartbeat packet size of %d", HeartbeatPacketMinSize)
	}

	if len(data) > HeartbeatPacketMaxSize {
		return nil, errors.WithMessagef(ErrMalformedPacket, "packet exceeds maximum heartbeat packet size of %d", HeartbeatPacketMaxSize)
	}
	networkIDBytesLength := int(data[HeartbeatPacketNetworkIDBytesCountSize-1])
	if networkIDBytesLength == 0 {
		return nil, ErrInvalidHeartbeatNetworkVersion
	}
	if networkIDBytesLength > HeartbeatPacketMaxNetworkIDBytesSize {
		return nil, ErrInvalidHeartbeatNetworkVersion
	}
	// sanity check: packet len - min packet - networkIDLength % id size = 0,
	// since we're only dealing with IDs from that offset
	if (len(data)-HeartbeatPacketMinSize-networkIDBytesLength)%HeartbeatPacketPeerIDSize != 0 {
		return nil, errors.WithMessage(ErrMalformedPacket, "heartbeat packet is malformed since the data length after the min. packet size offset isn't conforming with peer ID sizes")
	}
	offset := HeartbeatPacketNetworkIDBytesCountSize
	// copy network id
	networkID := make([]byte, networkIDBytesLength)
	copy(networkID, data[offset:offset+networkIDBytesLength])

	// networkID always starts with a "v", lets check it
	networkIDString := string(networkID)
	if !strings.HasPrefix(networkIDString, "v") {
		return nil, ErrInvalidHeartbeatNetworkVersion
	}

	offset += networkIDBytesLength

	// copy own ID
	ownID := make([]byte, HeartbeatPacketPeerIDSize)
	copy(ownID, data[offset:offset+HeartbeatPacketPeerIDSize])

	offset += HeartbeatPacketPeerIDSize

	// read outbound IDs count
	outboundIDCount := int(data[offset])
	offset += HeartbeatPacketOutboundIDCountSize
	if outboundIDCount > HeartbeatMaxOutboundPeersCount {
		return nil, errors.WithMessagef(ErrMalformedPacket, "heartbeat packet exceeds maximum outbound IDs of %d", HeartbeatMaxOutboundPeersCount)
	}

	// check whether we'd have the amount of data needed for the advertised outbound id count
	if (len(data)-HeartbeatPacketMinSize-networkIDBytesLength)/HeartbeatPacketPeerIDSize < outboundIDCount {
		return nil, errors.WithMessage(ErrMalformedPacket, "heartbeat packet is malformed since remaining data length wouldn't fit advertised outbound IDs count")
	}

	// outbound IDs can be zero
	outboundIDs := make([][]byte, outboundIDCount)

	if outboundIDCount != 0 {
		for i := range outboundIDs {
			outboundIDs[i] = make([]byte, HeartbeatPacketPeerIDSize)
			copy(outboundIDs[i], data[offset+i*HeartbeatPacketPeerIDSize:offset+(i+1)*HeartbeatPacketPeerIDSize])
		}
	}

	// (packet size - (min packet size + read outbound IDs)) / ID size = inbound IDs count
	inboundIDCount := (len(data) - (offset + outboundIDCount*HeartbeatPacketPeerIDSize)) / HeartbeatPacketPeerIDSize
	if inboundIDCount > HeartbeatMaxInboundPeersCount {
		return nil, errors.WithMessagef(ErrMalformedPacket, "heartbeat packet exceeds maximum inbound IDs of %d", HeartbeatMaxInboundPeersCount)
	}

	// inbound IDs can be zero
	inboundIDs := make([][]byte, inboundIDCount)
	offset += outboundIDCount * HeartbeatPacketPeerIDSize
	for i := range inboundIDs {
		inboundIDs[i] = make([]byte, HeartbeatPacketPeerIDSize)
		copy(inboundIDs[i], data[offset+i*HeartbeatPacketPeerIDSize:offset+(i+1)*HeartbeatPacketPeerIDSize])
	}

	return &Heartbeat{NetworkID: networkID, OwnID: ownID, OutboundIDs: outboundIDs, InboundIDs: inboundIDs}, nil
}

// NewHeartbeatBlock serializes the given heartbeat into a byte slice and adds a tlv header to the packet.
// block = tlv header + serialized packet.
func NewHeartbeatBlock(hb *Heartbeat) ([]byte, error) {
	if len(hb.NetworkID) > HeartbeatPacketMaxNetworkIDBytesSize {
		return nil, errors.WithMessagef(ErrInvalidHeartbeat, "heartbeat exceeds maximum length of NetworkID of %d", HeartbeatPacketMaxNetworkIDBytesSize)
	}
	if len(hb.NetworkID) == 0 {
		return nil, errors.WithMessage(ErrInvalidHeartbeat, "heartbeat NetworkID length is 0")
	}
	if len(hb.InboundIDs) > HeartbeatMaxInboundPeersCount {
		return nil, errors.WithMessagef(ErrInvalidHeartbeat, "heartbeat exceeds maximum inbound IDs of %d", HeartbeatMaxInboundPeersCount)
	}
	if len(hb.OutboundIDs) > HeartbeatMaxOutboundPeersCount {
		return nil, errors.WithMessagef(ErrInvalidHeartbeat, "heartbeat exceeds maximum outbound IDs of %d", HeartbeatMaxOutboundPeersCount)
	}

	if len(hb.OwnID) != HeartbeatPacketPeerIDSize {
		return nil, errors.WithMessage(ErrInvalidHeartbeat, "heartbeat must contain the own peer ID")
	}

	// calculate total needed bytes based on packet
	packetSize := HeartbeatPacketMinSize + len(hb.NetworkID) + len(hb.OutboundIDs)*HeartbeatPacketPeerIDSize + len(hb.InboundIDs)*HeartbeatPacketPeerIDSize
	packet := make([]byte, packetSize)

	// network id size
	packet[0] = byte(len(hb.NetworkID))

	offset := HeartbeatPacketNetworkIDBytesCountSize

	copy(packet[offset:offset+len(hb.NetworkID)], hb.NetworkID)

	offset += len(hb.NetworkID)

	// own nodeId
	copy(packet[offset:offset+HeartbeatPacketPeerIDSize], hb.OwnID)

	// outbound id count
	packet[offset+HeartbeatPacketPeerIDSize] = byte(len(hb.OutboundIDs))
	offset += HeartbeatPacketPeerIDSize + HeartbeatPacketOutboundIDCountSize

	// copy contents of hb.OutboundIDs
	for i, outboundID := range hb.OutboundIDs {
		copy(packet[offset+i*HeartbeatPacketPeerIDSize:offset+(i+1)*HeartbeatPacketPeerIDSize], outboundID[:HeartbeatPacketPeerIDSize])
	}

	// advance offset to after outbound IDs
	offset += len(hb.OutboundIDs) * HeartbeatPacketPeerIDSize

	// copy contents of hb.InboundIDs
	for i, inboundID := range hb.InboundIDs {
		copy(packet[offset+i*HeartbeatPacketPeerIDSize:offset+(i+1)*HeartbeatPacketPeerIDSize], inboundID[:HeartbeatPacketPeerIDSize])
	}

	// create a buffer for tlv header plus the packet
	buf := bytes.NewBuffer(make([]byte, 0, tlv.HeaderMessageDefinition.MaxBytesLength+uint16(packetSize)))
	// write tlv header into buffer
	if err := tlv.WriteHeader(buf, MessageTypeHeartbeat, uint16(packetSize)); err != nil {
		return nil, err
	}
	// write serialized packet bytes into the buffer
	if err := binary.Write(buf, binary.BigEndian, packet); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}
