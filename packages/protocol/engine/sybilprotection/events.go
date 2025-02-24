package sybilprotection

import (
	"github.com/iotaledger/hive.go/core/generics/event"
)

// Events is a collection of events that can be triggered by the SybilProtection.
type Events struct {
	// WeightUpdated is triggered when a weight of a node is updated.
	WeightsUpdated *event.Linkable[*WeightsBatch]

	// LinkableCollection is a generic trait that allows to link multiple collections of events together.
	event.LinkableCollection[Events, *Events]
}

// NewEvents contains the constructor of the Events object (it is generated by a generic factory).
var NewEvents = event.LinkableConstructor(func() (newEvents *Events) {
	return &Events{
		WeightsUpdated: event.NewLinkable[*WeightsBatch](),
	}
})
