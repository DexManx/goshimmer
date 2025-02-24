package client

import (
	"io"
	"strings"

	"github.com/iotaledger/hive.go/core/generics/lo"
	"github.com/iotaledger/hive.go/core/identity"
	"github.com/mr-tron/base58"

	"github.com/iotaledger/goshimmer/packages/app/metrics"
	"github.com/iotaledger/goshimmer/plugins/analysis/packet"
	"github.com/iotaledger/goshimmer/plugins/banner"
)

// EventDispatchers holds the Heartbeat function.
type EventDispatchers struct {
	// Heartbeat defines the Heartbeat function.
	Heartbeat func(heartbeat *packet.Heartbeat)
}

func sendHeartbeat(w io.Writer, hb *packet.Heartbeat) {
	var out strings.Builder
	for _, value := range hb.OutboundIDs {
		out.WriteString(base58.Encode(value))
	}
	var in strings.Builder
	for _, value := range hb.InboundIDs {
		in.WriteString(base58.Encode(value))
	}
	log.Debugw(
		"Heartbeat",
		"networkID", string(hb.NetworkID),
		"nodeID", base58.Encode(hb.OwnID),
		"outboundIDs", out.String(),
		"inboundIDs", in.String(),
	)

	data, err := packet.NewHeartbeatBlock(hb)
	if err != nil {
		log.Info(err, " - heartbeat block skipped")
		return
	}

	if _, err = w.Write(data); err != nil {
		log.Debugw("Error while writing to connection", "Description", err)
	}
	// trigger AnalysisOutboundBytes event
	metrics.Events.AnalysisOutboundBytes.Trigger(&metrics.AnalysisOutboundBytesEvent{AmountBytes: uint64(len(data))})
}

func createHeartbeat() *packet.Heartbeat {
	// get own ID
	nodeID := make([]byte, len(identity.ID{}))
	if deps.Local != nil {
		copy(nodeID, lo.PanicOnErr(deps.Local.ID().Bytes()))
	}

	var outboundIDs [][]byte
	var inboundIDs [][]byte

	// get outboundIDs (chosen neighbors)
	outgoingNeighbors := deps.Selection.GetOutgoingNeighbors()
	outboundIDs = make([][]byte, len(outgoingNeighbors))
	for i, neighbor := range outgoingNeighbors {
		outboundIDs[i] = make([]byte, len(identity.ID{}))
		copy(outboundIDs[i], lo.PanicOnErr(neighbor.ID().Bytes()))
	}

	// get inboundIDs (accepted neighbors)
	incomingNeighbors := deps.Selection.GetIncomingNeighbors()
	inboundIDs = make([][]byte, len(incomingNeighbors))
	for i, neighbor := range incomingNeighbors {
		inboundIDs[i] = make([]byte, len(identity.ID{}))
		copy(inboundIDs[i], lo.PanicOnErr(neighbor.ID().Bytes()))
	}

	return &packet.Heartbeat{NetworkID: []byte(banner.SimplifiedAppVersion), OwnID: nodeID, OutboundIDs: outboundIDs, InboundIDs: inboundIDs}
}
