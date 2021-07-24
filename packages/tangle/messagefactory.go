package tangle

import (
	"fmt"
	"github.com/iotaledger/hive.go/types"
	"sync"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/iotaledger/hive.go/crypto/ed25519"
	"github.com/iotaledger/hive.go/events"
	"github.com/iotaledger/hive.go/identity"
	"github.com/iotaledger/hive.go/kvstore"

	"github.com/iotaledger/goshimmer/packages/clock"
	"github.com/iotaledger/goshimmer/packages/ledgerstate"
	"github.com/iotaledger/goshimmer/packages/tangle/payload"
)

const storeSequenceInterval = 100

// region MessageFactory ///////////////////////////////////////////////////////////////////////////////////////////////

// MessageFactory acts as a factory to create new messages.
type MessageFactory struct {
	Events *MessageFactoryEvents

	tangle        *Tangle
	sequence      *kvstore.Sequence
	localIdentity *identity.LocalIdentity
	selector      TipSelector
	powTimeout    time.Duration

	worker        Worker
	workerMutex   sync.RWMutex
	issuanceMutex sync.Mutex
}

// NewMessageFactory creates a new message factory.
func NewMessageFactory(tangle *Tangle, selector TipSelector) *MessageFactory {
	sequence, err := kvstore.NewSequence(tangle.Options.Store, []byte(DBSequenceNumber), storeSequenceInterval)
	if err != nil {
		panic(fmt.Sprintf("could not create message sequence number: %v", err))
	}

	return &MessageFactory{
		Events: &MessageFactoryEvents{
			MessageConstructed: events.NewEvent(messageEventHandler),
			Error:              events.NewEvent(events.ErrorCaller),
		},

		tangle:        tangle,
		sequence:      sequence,
		localIdentity: tangle.Options.Identity,
		selector:      selector,
		worker:        ZeroWorker,
		powTimeout:    0 * time.Second,
	}
}

// SetWorker sets the PoW worker to be used for the messages.
func (f *MessageFactory) SetWorker(worker Worker) {
	f.workerMutex.Lock()
	defer f.workerMutex.Unlock()
	f.worker = worker
}

// SetTimeout sets the timeout for PoW.
func (f *MessageFactory) SetTimeout(timeout time.Duration) {
	f.powTimeout = timeout
}

// IssuePayload creates a new message including sequence number and tip selection and returns it.
// It also triggers the MessageConstructed event once it's done, which is for example used by the plugins to listen for
// messages that shall be attached to the tangle.
func (f *MessageFactory) IssuePayload(p payload.Payload, parentsCount ...int) (*Message, error) {
	payloadLen := len(p.Bytes())
	if payloadLen > payload.MaxSize {
		err := fmt.Errorf("maximum payload size of %d bytes exceeded", payloadLen)
		f.Events.Error.Trigger(err)
		return nil, err
	}

	f.issuanceMutex.Lock()

	sequenceNumber, err := f.sequence.Next()
	if err != nil {
		err = errors.Errorf("could not create sequence number: %w", err)
		f.Events.Error.Trigger(err)
		f.issuanceMutex.Unlock()
		return nil, err
	}

	countParents := 2
	if len(parentsCount) > 0 {
		countParents = parentsCount[0]
	}

	parents, err := f.selector.Tips(p, countParents)

	if err != nil {
		err = errors.Errorf("tips could not be selected: %w", err)
		f.Events.Error.Trigger(err)
		f.issuanceMutex.Unlock()
		return nil, err
	}

	likeReferences, err := f.prepareLikeReferences(parents)

	if err != nil {
		err = errors.Errorf("like references could not be prepared: %w", err)
		f.Events.Error.Trigger(err)
		f.issuanceMutex.Unlock()
		return nil, err
	}

	issuingTime := f.getIssuingTime(parents)

	issuerPublicKey := f.localIdentity.PublicKey()

	// do the PoW
	startTime := time.Now()

	nonce, err := f.doPOW(parents, nil, likeReferences, issuingTime, issuerPublicKey, sequenceNumber, p)
	for err != nil && time.Since(startTime) < f.powTimeout {
		if p.Type() != ledgerstate.TransactionType {
			parents, err = f.selector.Tips(p, countParents)
			if err != nil {
				err = errors.Errorf("tips could not be selected: %w", err)
				f.Events.Error.Trigger(err)
				f.issuanceMutex.Unlock()
				return nil, err
			}
		}

		likeReferences, err := f.prepareLikeReferences(parents)

		if err != nil {
			err = errors.Errorf("like references could not be prepared: %w", err)
			f.Events.Error.Trigger(err)
			f.issuanceMutex.Unlock()
			return nil, err
		}

		issuingTime = f.getIssuingTime(parents)
		nonce, err = f.doPOW(parents, nil, likeReferences, issuingTime, issuerPublicKey, sequenceNumber, p)
	}

	if err != nil {
		err = errors.Errorf("pow failed: %w", err)
		f.Events.Error.Trigger(err)
		f.issuanceMutex.Unlock()
		return nil, err
	}
	f.issuanceMutex.Unlock()

	// create the signature
	signature := f.sign(parents, nil, likeReferences, issuingTime, issuerPublicKey, sequenceNumber, p, nonce)

	msg := NewMessage(
		parents,
		nil,
		nil,
		nil,
		issuingTime,
		issuerPublicKey,
		sequenceNumber,
		p,
		nonce,
		signature,
	)
	f.Events.MessageConstructed.Trigger(msg)
	return msg, nil
}

func (f *MessageFactory) prepareLikeReferences(parents MessageIDs) (MessageIDs, error) {
	branchIDs := make(ledgerstate.BranchIDs)

	for _, parent := range parents {
		branchID, err := f.tangle.Booker.MessageBranchID(parent)
		if err != nil {
			err = errors.Errorf("branchID can't be retrieved: %w", err)
			f.Events.Error.Trigger(err)
			return nil, err
		}

		branchIDs.Add(branchID)
	}
	// FIXME: replace with actual implementation
	_, dislikedBranches, err := f.tangle.OTVConsensusManager.Opinion(branchIDs)
	if err != nil {
		err = errors.Errorf("opinions could not be retrieved: %w", err)
		f.Events.Error.Trigger(err)
		return nil, err
	}
	likeReferencesMap := make(map[MessageID]types.Empty)
	likeReferences := MessageIDs{}
	// TODO: ask jonas why multiple tuples are returned
	for dislikedBranch := range dislikedBranches {
		likedInstead, err := f.tangle.OTVConsensusManager.LikedInstead(dislikedBranch)
		if err != nil {
			err = errors.Errorf("branch liked instead could not be retrieved: %w", err)
			f.Events.Error.Trigger(err)
			return nil, err
		}

		for _, likeRef := range likedInstead {

			transactionID := likeRef.Liked.TransactionID()
			// TODO: replace with oldest instead of newest
			latestAttachmentTime := time.Unix(0, 0)
			latestAttachmentMessageID := MessageID{}
			// TODO: which message should we select? is selecting latest message correct?
			f.tangle.Storage.Attachments(transactionID).Consume(func(attachment *Attachment) {
				f.tangle.Storage.Message(attachment.MessageID()).Consume(func(message *Message) {
					if message.IssuingTime().After(latestAttachmentTime) {
						latestAttachmentTime = message.IssuingTime()
						latestAttachmentMessageID = message.ID()
					}
				})
			})
			// TODO: should we check max parent age in like reference parent? what if original message is older than maxparent age even though the branch still exists (parasite chain attack?)
			// add like reference to a message only once if it appears in multiple conflict sets
			if _, ok := likeReferencesMap[latestAttachmentMessageID]; !ok {
				likeReferencesMap[latestAttachmentMessageID] = types.Void
				likeReferences = append(likeReferences, latestAttachmentMessageID)
			}
		}
	}
	return likeReferences, nil
}

func (f *MessageFactory) getIssuingTime(parents MessageIDs) time.Time {
	issuingTime := clock.SyncedTime()

	// due to the ParentAge check we must ensure that we set the right issuing time.

	for _, parent := range parents {
		f.tangle.Storage.Message(parent).Consume(func(msg *Message) {
			if msg.ID() != EmptyMessageID && !msg.IssuingTime().Before(issuingTime) {
				issuingTime = msg.IssuingTime()
			}
		})
	}

	return issuingTime
}

// Shutdown closes the MessageFactory and persists the sequence number.
func (f *MessageFactory) Shutdown() {
	if err := f.sequence.Release(); err != nil {
		f.Events.Error.Trigger(fmt.Errorf("could not release message sequence number: %w", err))
	}
}

func (f *MessageFactory) doPOW(strongParents []MessageID, weakParents []MessageID, likeParents []MessageID, issuingTime time.Time, key ed25519.PublicKey, seq uint64, payload payload.Payload) (uint64, error) {
	// create a dummy message to simplify marshaling
	dummy := NewMessage(strongParents, weakParents, nil, likeParents, issuingTime, key, seq, payload, 0, ed25519.EmptySignature).Bytes()

	f.workerMutex.RLock()
	defer f.workerMutex.RUnlock()
	return f.worker.DoPOW(dummy)
}

func (f *MessageFactory) sign(strongParents []MessageID, weakParents []MessageID, likeParents []MessageID, issuingTime time.Time, key ed25519.PublicKey, seq uint64, payload payload.Payload, nonce uint64) ed25519.Signature {
	// create a dummy message to simplify marshaling
	dummy := NewMessage(strongParents, weakParents, nil, nil, issuingTime, key, seq, payload, nonce, ed25519.EmptySignature)
	dummyBytes := dummy.Bytes()

	contentLength := len(dummyBytes) - len(dummy.Signature())
	return f.localIdentity.Sign(dummyBytes[:contentLength])
}

// endregion ///////////////////////////////////////////////////////////////////////////////////////////////////////////

// region MessageFactoryEvents /////////////////////////////////////////////////////////////////////////////////////////

// MessageFactoryEvents represents events happening on a message factory.
type MessageFactoryEvents struct {
	// Fired when a message is built including tips, sequence number and other metadata.
	MessageConstructed *events.Event

	// Fired when an error occurred.
	Error *events.Event
}

func messageEventHandler(handler interface{}, params ...interface{}) {
	handler.(func(*Message))(params[0].(*Message))
}

// endregion ///////////////////////////////////////////////////////////////////////////////////////////////////////////

// region TipSelector //////////////////////////////////////////////////////////////////////////////////////////////////

// A TipSelector selects two tips, parent2 and parent1, for a new message to attach to.
type TipSelector interface {
	Tips(p payload.Payload, countParents int) (parents MessageIDs, err error)
}

// endregion ///////////////////////////////////////////////////////////////////////////////////////////////////////////

// region TipSelectorFunc //////////////////////////////////////////////////////////////////////////////////////////////

// The TipSelectorFunc type is an adapter to allow the use of ordinary functions as tip selectors.
type TipSelectorFunc func(p payload.Payload, countParents int) (parents MessageIDs, err error)

// Tips calls f().
func (f TipSelectorFunc) Tips(p payload.Payload, countParents int) (parents MessageIDs, err error) {
	return f(p, countParents)
}

// endregion ///////////////////////////////////////////////////////////////////////////////////////////////////////////

// region Worker ///////////////////////////////////////////////////////////////////////////////////////////////////////

// A Worker performs the PoW for the provided message in serialized byte form.
type Worker interface {
	DoPOW([]byte) (nonce uint64, err error)
}

// endregion ///////////////////////////////////////////////////////////////////////////////////////////////////////////

// region WorkerFunc ///////////////////////////////////////////////////////////////////////////////////////////////////

// The WorkerFunc type is an adapter to allow the use of ordinary functions as a PoW performer.
type WorkerFunc func([]byte) (uint64, error)

// DoPOW calls f(msg).
func (f WorkerFunc) DoPOW(msg []byte) (uint64, error) {
	return f(msg)
}

// endregion ///////////////////////////////////////////////////////////////////////////////////////////////////////////

// region ZeroWorker ///////////////////////////////////////////////////////////////////////////////////////////////////

// ZeroWorker is a PoW worker that always returns 0 as the nonce.
var ZeroWorker = WorkerFunc(func([]byte) (uint64, error) { return 0, nil })

// endregion ///////////////////////////////////////////////////////////////////////////////////////////////////////////
