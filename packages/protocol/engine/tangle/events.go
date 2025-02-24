package tangle

import (
	"github.com/iotaledger/hive.go/core/generics/event"

	"github.com/iotaledger/goshimmer/packages/protocol/engine/tangle/blockdag"
	"github.com/iotaledger/goshimmer/packages/protocol/engine/tangle/booker"
	"github.com/iotaledger/goshimmer/packages/protocol/engine/tangle/virtualvoting"
)

type Events struct {
	BlockDAG      *blockdag.Events
	Booker        *booker.Events
	VirtualVoting *virtualvoting.Events

	event.LinkableCollection[Events, *Events]
}

// NewEvents contains the constructor of the Events object (it is generated by a generic factory).
var NewEvents = event.LinkableConstructor(func() (newEvents *Events) {
	return &Events{
		BlockDAG:      blockdag.NewEvents(),
		Booker:        booker.NewEvents(),
		VirtualVoting: virtualvoting.NewEvents(),
	}
})
