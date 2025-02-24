package networkdelay

import (
	"sync"

	"github.com/mr-tron/base58"
	"github.com/pkg/errors"

	"github.com/iotaledger/hive.go/core/generics/model"
	"github.com/iotaledger/hive.go/core/serix"

	"github.com/iotaledger/goshimmer/packages/protocol/models/payload"
	"github.com/iotaledger/goshimmer/packages/protocol/models/payloadtype"
)

func init() {
	err := serix.DefaultAPI.RegisterTypeSettings(Payload{}, serix.TypeSettings{}.WithObjectType(uint32(new(Payload).Type())))
	if err != nil {
		panic(errors.Wrapf(err, "error registering Transaction type settings"))
	}
	err = serix.DefaultAPI.RegisterInterfaceObjects((*payload.Payload)(nil), new(Payload))
	if err != nil {
		panic(errors.Wrap(err, "error registering Transaction as Payload interface"))
	}
}

const (
	// PayloadName defines the name of the networkdelay payload.
	PayloadName = "networkdelay"
)

// ID represents a 32 byte ID of a network delay payload.
type ID [32]byte

// String returns a human-friendly representation of the ID.
func (id ID) String() string {
	return base58.Encode(id[:])
}

// Payload represents the network delay payload type.
type Payload struct {
	model.Immutable[Payload, *Payload, payloadModel] `serix:"0"`
}

type payloadModel struct {
	ID       ID    `serix:"0"`
	SentTime int64 `serix:"1"` // [ns]

	bytes      []byte
	bytesMutex sync.RWMutex
}

// NewPayload creates a new  network delay payload.
func NewPayload(id ID, sentTime int64) *Payload {
	return model.NewImmutable[Payload](
		&payloadModel{
			ID:       id,
			SentTime: sentTime,
		},
	)
}

// ID returns the ID of the Payload.
func (p *Payload) ID() ID {
	return p.M.ID
}

// SentTime returns the type of the Payload.
func (p *Payload) SentTime() int64 {
	return p.M.SentTime
}

// region Payload implementation ///////////////////////////////////////////////////////////////////////////////////////

// Type represents the identifier which addresses the network delay Payload type.
var Type = payload.NewType(payloadtype.NetworkDelay, PayloadName)

// Type returns the type of the Payload.
func (p *Payload) Type() payload.Type {
	return Type
}

// // endregion ///////////////////////////////////////////////////////////////////////////////////////////////////////////
