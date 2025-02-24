//nolint:dupl
package tipmanager

import (
	"fmt"
	"testing"
	"time"

	"github.com/iotaledger/hive.go/core/generics/event"
	"github.com/iotaledger/hive.go/core/generics/lo"
	"github.com/iotaledger/hive.go/core/types"
	"github.com/stretchr/testify/require"

	"github.com/iotaledger/goshimmer/packages/core/commitment"
	"github.com/iotaledger/goshimmer/packages/core/epoch"
	"github.com/iotaledger/goshimmer/packages/protocol/engine/notarization"
	"github.com/iotaledger/goshimmer/packages/protocol/engine/tangle"
	"github.com/iotaledger/goshimmer/packages/protocol/engine/tangle/booker"
	"github.com/iotaledger/goshimmer/packages/protocol/engine/tangle/booker/markermanager"
	"github.com/iotaledger/goshimmer/packages/protocol/engine/tangle/booker/markers"
	"github.com/iotaledger/goshimmer/packages/protocol/models"
)

func TestTipManager_DataBlockTips(t *testing.T) {
	tf := NewTestFramework(t)

	tipManager := tf.TipManager

	// without any tip -> genesis
	{
		tf.AssertEqualBlocks(tipManager.Tips(2), tf.BlockIDs("Genesis"))
	}

	// Block 1
	{
		tf.CreateBlock("Block1")
		tf.IssueBlocks("Block1")
		tf.WaitUntilAllTasksProcessed()

		tf.AssertTipCount(1)
		tf.AssertEqualBlocks(tipManager.Tips(2), tf.BlockIDs("Block1"))
		tf.AssertTipsAdded(1)
		tf.AssertTipsRemoved(0)
	}

	// Block 2
	{
		tf.CreateBlock("Block2")
		tf.IssueBlocksAndSetAccepted("Block2")
		tf.WaitUntilAllTasksProcessed()

		tf.AssertTipCount(2)
		tf.AssertEqualBlocks(tipManager.Tips(2), tf.BlockIDs("Block1", "Block2"))
		tf.AssertTipsAdded(2)
		tf.AssertTipsRemoved(0)
	}

	// Block 3
	{
		tf.CreateBlock("Block3", models.WithStrongParents(tf.BlockIDs("Block1", "Block2")))
		tf.IssueBlocksAndSetAccepted("Block3")
		tf.WaitUntilAllTasksProcessed()

		tf.AssertTipCount(1)
		tf.AssertEqualBlocks(tipManager.Tips(2), tf.BlockIDs("Block3"))
		tf.AssertTipsAdded(3)
		tf.AssertTipsRemoved(2)
	}

	// Add Block 4-8
	{
		for count, n := range []int{4, 5, 6, 7, 8} {
			count++

			alias := fmt.Sprintf("Block%d", n)
			tf.CreateBlock(alias, models.WithStrongParents(tf.BlockIDs("Block1")))
			tf.IssueBlocksAndSetAccepted(alias)
			tf.WaitUntilAllTasksProcessed()

			tf.AssertTipCount(1 + count)
			tf.AssertTipsAdded(uint32(3 + count))
			tf.AssertTipsRemoved(2)
		}
	}

	// now we have 6 tips
	// Tips(4) -> 4
	{
		parents := tipManager.Tips(4)
		require.Equal(t, 4, len(parents))
	}

	// Tips(8) -> 6
	{
		tf.AssertEqualBlocks(tipManager.Tips(8), tf.BlockIDs("Block3", "Block4", "Block5", "Block6", "Block7", "Block8"))
	}

	// Tips(0) -> 1
	{
		parents := tipManager.Tips(0)
		require.Equal(t, 1, len(parents))
	}
}

// Test based on packages/tangle/images/TSC_test_scenario.png except nothing is confirmed.
func TestTipManager_TimeSinceConfirmation_Unconfirmed(t *testing.T) {
	tf := NewTestFramework(t,
		WithTipManagerOptions(WithTimeSinceConfirmationThreshold(5*time.Minute)),
		WithTangleOptions(tangle.WithBookerOptions(booker.WithMarkerManagerOptions(markermanager.WithSequenceManagerOptions[models.BlockID, *booker.Block](markers.WithMaxPastMarkerDistance(10))))),
	)
	tf.engine.EvictionState.AddRootBlock(models.EmptyBlockID)

	createTestTangleTSC(tf)

	// Even without any confirmations, it should be possible to attach to genesis.
	tf.AssertIsPastConeTimestampCorrect("Genesis", true)

	// case 0 - only one block can attach to genesis, so there should not be two subtangles starting from the genesis, but TSC allows using such tip.
	tf.AssertIsPastConeTimestampCorrect("7/1_2", true)

	tf.SetAcceptedTime(tf.Block("Marker-2/3").IssuingTime())

	// case #1
	tf.AssertIsPastConeTimestampCorrect("0/3_4", false)
	// case #2
	tf.AssertIsPastConeTimestampCorrect("1/3_4", false)
	// case #3
	tf.AssertIsPastConeTimestampCorrect("2/3_4", false)
	// case #4 (marker block)
	tf.AssertIsPastConeTimestampCorrect("Marker-1/2", false)
	// case #5
	tf.AssertIsPastConeTimestampCorrect("2/5_4", false)
	// case #6 (attach to unconfirmed block older than TSC)
	tf.AssertIsPastConeTimestampCorrect("0/1-preTSC_2", false)
	// case #7
	tf.AssertIsPastConeTimestampCorrect("3/2_4", false)
	// case #8
	tf.AssertIsPastConeTimestampCorrect("2/3+0/4_3", false)
	// case #9
	tf.AssertIsPastConeTimestampCorrect("Marker-4/5", false)
	// case #10 (attach to confirmed block older than TSC)
	tf.AssertIsPastConeTimestampCorrect("0/1-preTSCSeq2_2", false)
	// case #11
	tf.AssertIsPastConeTimestampCorrect("5/2_4", false)
	// case #12 (attach to 0/1-postTSCSeq3_4)
	tf.AssertIsPastConeTimestampCorrect("0/1-postTSCSeq3_4", false)
	// case #13 (attach to unconfirmed block younger than TSC, with confirmed past marker older than TSC)
	tf.AssertIsPastConeTimestampCorrect("0/1-postTSC_2", false)
	// case #14
	tf.AssertIsPastConeTimestampCorrect("6/2_4", false)
	// case #15
	tf.AssertIsPastConeTimestampCorrect("0/1-preTSCSeq5_4", false)
	// case #16
	tf.AssertIsPastConeTimestampCorrect("0/1-postTSC-direct_0", false)
}

// Test based on packages/tangle/images/TSC_test_scenario.png.
func TestTipManager_TimeSinceConfirmation_Confirmed(t *testing.T) {
	tf := NewTestFramework(t,
		WithTipManagerOptions(WithTimeSinceConfirmationThreshold(5*time.Minute)),
		WithTangleOptions(tangle.WithBookerOptions(booker.WithMarkerManagerOptions(markermanager.WithSequenceManagerOptions[models.BlockID, *booker.Block](markers.WithMaxPastMarkerDistance(10))))),
	)
	tf.engine.EvictionState.AddRootBlock(models.EmptyBlockID)
	createTestTangleTSC(tf)

	// Even without any confirmations, it should be possible to attach to genesis.
	tf.AssertIsPastConeTimestampCorrect("Genesis", true)
	// case 0 - only one block can attach to genesis, so there should not be two subtangles starting from the genesis, but TSC allows using such tip.
	tf.AssertIsPastConeTimestampCorrect("7/1_2", true)

	acceptedBlockIDsAliases := []string{"Marker-0/1", "0/1-preTSCSeq1_0", "0/1-preTSCSeq1_1", "0/1-preTSCSeq1_2", "0/1-postTSCSeq1_0", "0/1-postTSCSeq1_1", "0/1-postTSCSeq1_2", "0/1-postTSCSeq1_3", "0/1-postTSCSeq1_4", "0/1-postTSCSeq1_5", "Marker-1/2", "0/1-preTSCSeq2_0", "0/1-preTSCSeq2_1", "0/1-preTSCSeq2_2", "0/1-postTSCSeq2_0", "0/1-postTSCSeq2_1", "0/1-postTSCSeq2_2", "0/1-postTSCSeq2_3", "0/1-postTSCSeq2_4", "0/1-postTSCSeq2_5", "Marker-2/2", "2/2_0", "2/2_1", "2/2_2", "2/2_3", "2/2_4", "Marker-2/3"}
	acceptedMarkers := []markers.Marker{markers.NewMarker(0, 1), markers.NewMarker(1, 2), markers.NewMarker(2, 3)}
	tf.SetBlocksAccepted(acceptedBlockIDsAliases...)
	tf.SetMarkersAccepted(acceptedMarkers...)
	tf.SetAcceptedTime(tf.Block("Marker-2/3").IssuingTime())
	require.Eventually(t, tf.engine.IsBootstrapped, 1*time.Minute, 500*time.Millisecond)

	// As we advance ATT, Genesis should be beyond TSC, and thus invalid.
	tf.AssertIsPastConeTimestampCorrect("Genesis", false)

	// case 0 - only one block can attach to genesis, so there should not be two subtangles starting from the genesis, but TSC allows using such tip.
	tf.AssertIsPastConeTimestampCorrect("7/1_2", false)
	// case #1
	tf.AssertIsPastConeTimestampCorrect("0/3_4", false)
	// case #2
	tf.AssertIsPastConeTimestampCorrect("1/3_4", true)
	// case #3
	tf.AssertIsPastConeTimestampCorrect("2/3_4", true)
	// case #4 (marker block)
	tf.AssertIsPastConeTimestampCorrect("Marker-1/2", true)
	// case #5
	tf.AssertIsPastConeTimestampCorrect("2/5_4", false)
	// case #6 (attach to unconfirmed block older than TSC)
	tf.AssertIsPastConeTimestampCorrect("0/1-preTSC_2", false)
	// case #7
	tf.AssertIsPastConeTimestampCorrect("3/2_4", true)
	// case #8
	tf.AssertIsPastConeTimestampCorrect("2/3+0/4_3", false)
	// case #9
	tf.AssertIsPastConeTimestampCorrect("Marker-4/5", false)
	// case #10 (attach to confirmed block older than TSC)
	tf.AssertIsPastConeTimestampCorrect("0/1-preTSCSeq2_2", false)
	// case #11
	tf.AssertIsPastConeTimestampCorrect("5/2_4", false)
	// case #12 (attach to 0/1-postTSCSeq3_4)
	tf.AssertIsPastConeTimestampCorrect("0/1-postTSCSeq3_4", true)
	// case #13 (attach to unconfirmed block younger than TSC, with confirmed past marker older than TSC)
	tf.AssertIsPastConeTimestampCorrect("0/1-postTSC_2", false)
	// case #14
	tf.AssertIsPastConeTimestampCorrect("6/2_4", false)
	// case #15
	tf.AssertIsPastConeTimestampCorrect("0/1-preTSCSeq5_4", false)
	// case #16
	tf.AssertIsPastConeTimestampCorrect("0/1-postTSC-direct_0", false)
}

// Test based on packages/tangle/images/TSC_test_scenario.png.
func TestTipManager_TimeSinceConfirmation_MultipleParents(t *testing.T) {
	tf := NewTestFramework(t,
		WithTipManagerOptions(WithTimeSinceConfirmationThreshold(5*time.Minute)),
		WithTangleOptions(tangle.WithBookerOptions(booker.WithMarkerManagerOptions(markermanager.WithSequenceManagerOptions[models.BlockID, *booker.Block](markers.WithMaxPastMarkerDistance(10))))),
	)
	tf.engine.EvictionState.AddRootBlock(models.EmptyBlockID)

	createTestTangleMultipleParents(tf)

	acceptedBlockIDsAliases := []string{"Marker-0/1", "Marker-0/2", "Marker-0/3"}
	acceptedMarkers := []markers.Marker{markers.NewMarker(0, 1), markers.NewMarker(0, 2), markers.NewMarker(0, 3)}
	tf.SetBlocksAccepted(acceptedBlockIDsAliases...)
	tf.SetMarkersAccepted(acceptedMarkers...)
	tf.SetAcceptedTime(tf.Block("Marker-0/3").IssuingTime())
	require.Eventually(t, tf.engine.IsBootstrapped, 1*time.Minute, 500*time.Millisecond)

	// As we advance ATT, Genesis should be beyond TSC, and thus invalid.
	tf.AssertIsPastConeTimestampCorrect("Genesis", false)

	tf.AssertIsPastConeTimestampCorrect("Marker-0/1", false)

	tf.AssertIsPastConeTimestampCorrect("IncorrectTip2", false)

	// case #1
	tf.AssertIsPastConeTimestampCorrect("IncorrectTip", false)
}

func createTestTangleMultipleParents(tf *TestFramework) {
	markersMap := make(map[string]*markers.Markers)

	// SEQUENCE 0
	{
		tf.CreateBlock("Marker-0/1", models.WithStrongParents(tf.BlockIDs("Genesis")), models.WithIssuingTime(time.Now().Add(-9*time.Minute)))
		tf.IssueBlocks("Marker-0/1").WaitUntilAllTasksProcessed()

		tf.CreateBlock("Marker-0/2", models.WithStrongParents(tf.BlockIDs("Marker-0/1")))
		tf.IssueBlocks("Marker-0/2").WaitUntilAllTasksProcessed()

		tf.CreateBlock("Marker-0/3", models.WithStrongParents(tf.BlockIDs("Marker-0/2")))
		tf.IssueBlocks("Marker-0/3").WaitUntilAllTasksProcessed()

		tf.CreateBlock("Marker-0/4", models.WithStrongParents(tf.BlockIDs("Marker-0/3")))
		tf.IssueBlocks("Marker-0/4").WaitUntilAllTasksProcessed()

		tf.CreateBlock("IncorrectTip", models.WithStrongParents(tf.BlockIDs("Marker-0/1", "Marker-0/3")))
		tf.IssueBlocks("IncorrectTip").WaitUntilAllTasksProcessed()

		tf.CreateBlock("IncorrectTip2", models.WithStrongParents(tf.BlockIDs("Marker-0/1")))
		tf.IssueBlocks("IncorrectTip2").WaitUntilAllTasksProcessed()

		tf.CheckMarkers(lo.MergeMaps(markersMap, map[string]*markers.Markers{
			"Marker-0/1":   markers.NewMarkers(markers.NewMarker(0, 1)),
			"Marker-0/2":   markers.NewMarkers(markers.NewMarker(0, 2)),
			"Marker-0/3":   markers.NewMarkers(markers.NewMarker(0, 3)),
			"Marker-0/4":   markers.NewMarkers(markers.NewMarker(0, 4)),
			"IncorrectTip": markers.NewMarkers(markers.NewMarker(0, 3)),
		}))
	}
}

func createTestTangleTSC(tf *TestFramework) {
	markersMap := make(map[string]*markers.Markers)
	var lastBlockAlias string

	// SEQUENCE 0
	{
		tf.CreateBlock("Marker-0/1", models.WithStrongParents(tf.BlockIDs("Genesis")), models.WithIssuingTime(time.Now().Add(-9*time.Minute)))
		tf.IssueBlocks("Marker-0/1").WaitUntilAllTasksProcessed()
		tf.PreventNewMarkers(true)
		lastBlockAlias = issueBlocks(tf, "0/1-preTSC", 3, []string{"Marker-0/1"}, time.Minute*8)
		lastBlockAlias = issueBlocks(tf, "0/1-postTSC", 3, []string{lastBlockAlias}, time.Minute)
		tf.PreventNewMarkers(false)
		tf.CreateBlock("Marker-0/2", models.WithStrongParents(tf.BlockIDs(lastBlockAlias)))
		tf.IssueBlocks("Marker-0/2").WaitUntilAllTasksProcessed()
		tf.PreventNewMarkers(true)
		lastBlockAlias = issueBlocks(tf, "0/2", 5, []string{"Marker-0/2"}, 0)
		tf.PreventNewMarkers(false)
		tf.CreateBlock("Marker-0/3", models.WithStrongParents(tf.BlockIDs(lastBlockAlias)))
		tf.IssueBlocks("Marker-0/3").WaitUntilAllTasksProcessed()
		tf.PreventNewMarkers(true)
		lastBlockAlias = issueBlocks(tf, "0/3", 5, []string{"Marker-0/3"}, 0)
		tf.PreventNewMarkers(false)
		tf.CreateBlock("Marker-0/4", models.WithStrongParents(tf.BlockIDs(lastBlockAlias)))
		tf.IssueBlocks("Marker-0/4").WaitUntilAllTasksProcessed()
		tf.PreventNewMarkers(true)
		_ = issueBlocks(tf, "0/4", 5, []string{"Marker-0/4"}, 0)
		tf.PreventNewMarkers(false)

		// issue block for test case #16
		tf.CreateBlock("0/1-postTSC-direct_0", models.WithStrongParents(tf.BlockIDs("Marker-0/1")))
		tf.IssueBlocks("0/1-postTSC-direct_0").WaitUntilAllTasksProcessed()

		tf.CheckMarkers(lo.MergeMaps(markersMap, map[string]*markers.Markers{
			"Marker-0/1":    markers.NewMarkers(markers.NewMarker(0, 1)),
			"0/1-preTSC_0":  markers.NewMarkers(markers.NewMarker(0, 1)),
			"0/1-preTSC_1":  markers.NewMarkers(markers.NewMarker(0, 1)),
			"0/1-preTSC_2":  markers.NewMarkers(markers.NewMarker(0, 1)),
			"0/1-postTSC_0": markers.NewMarkers(markers.NewMarker(0, 1)),
			"0/1-postTSC_1": markers.NewMarkers(markers.NewMarker(0, 1)),
			"0/1-postTSC_2": markers.NewMarkers(markers.NewMarker(0, 1)),
			"Marker-0/2":    markers.NewMarkers(markers.NewMarker(0, 2)),
			"Marker-0/3":    markers.NewMarkers(markers.NewMarker(0, 3)),
			"Marker-0/4":    markers.NewMarkers(markers.NewMarker(0, 4)),
		}))
	}

	// SEQUENCE 1
	{
		tf.PreventNewMarkers(true)
		lastBlockAlias = issueBlocks(tf, "0/1-preTSCSeq1", 3, []string{"Marker-0/1"}, time.Minute*6)
		lastBlockAlias = issueBlocks(tf, "0/1-postTSCSeq1", 6, []string{lastBlockAlias}, time.Minute*4)
		tf.PreventNewMarkers(false)
		tf.CreateBlock("Marker-1/2", models.WithStrongParents(tf.BlockIDs(lastBlockAlias)), models.WithIssuingTime(time.Now().Add(-3*time.Minute)))
		tf.IssueBlocks("Marker-1/2").WaitUntilAllTasksProcessed()
		tf.PreventNewMarkers(true)
		lastBlockAlias = issueBlocks(tf, "1/2", 5, []string{"Marker-1/2"}, 0)
		tf.PreventNewMarkers(false)
		tf.CreateBlock("Marker-1/3", models.WithStrongParents(tf.BlockIDs(lastBlockAlias)))
		tf.IssueBlocks("Marker-1/3").WaitUntilAllTasksProcessed()
		tf.PreventNewMarkers(true)
		_ = issueBlocks(tf, "1/3", 5, []string{"Marker-1/3"}, 0)
		tf.PreventNewMarkers(false)

		tf.CheckMarkers(lo.MergeMaps(markersMap, map[string]*markers.Markers{
			"0/1-preTSCSeq1_0":  markers.NewMarkers(markers.NewMarker(0, 1)),
			"0/1-preTSCSeq1_1":  markers.NewMarkers(markers.NewMarker(0, 1)),
			"0/1-preTSCSeq1_2":  markers.NewMarkers(markers.NewMarker(0, 1)),
			"0/1-postTSCSeq1_0": markers.NewMarkers(markers.NewMarker(0, 1)),
			"0/1-postTSCSeq1_1": markers.NewMarkers(markers.NewMarker(0, 1)),
			"0/1-postTSCSeq1_2": markers.NewMarkers(markers.NewMarker(0, 1)),
			"0/1-postTSCSeq1_3": markers.NewMarkers(markers.NewMarker(0, 1)),
			"0/1-postTSCSeq1_4": markers.NewMarkers(markers.NewMarker(0, 1)),
			"0/1-postTSCSeq1_5": markers.NewMarkers(markers.NewMarker(0, 1)),
			"Marker-1/2":        markers.NewMarkers(markers.NewMarker(1, 2)),
			"1/2_0":             markers.NewMarkers(markers.NewMarker(1, 2)),
			"1/2_1":             markers.NewMarkers(markers.NewMarker(1, 2)),
			"1/2_2":             markers.NewMarkers(markers.NewMarker(1, 2)),
			"1/2_3":             markers.NewMarkers(markers.NewMarker(1, 2)),
			"1/2_4":             markers.NewMarkers(markers.NewMarker(1, 2)),
			"Marker-1/3":        markers.NewMarkers(markers.NewMarker(1, 3)),
			"1/3_0":             markers.NewMarkers(markers.NewMarker(1, 3)),
			"1/3_1":             markers.NewMarkers(markers.NewMarker(1, 3)),
			"1/3_2":             markers.NewMarkers(markers.NewMarker(1, 3)),
			"1/3_3":             markers.NewMarkers(markers.NewMarker(1, 3)),
			"1/3_4":             markers.NewMarkers(markers.NewMarker(1, 3)),
		}))
	}

	// SEQUENCE 2
	{
		tf.PreventNewMarkers(true)
		lastBlockAlias = issueBlocks(tf, "0/1-preTSCSeq2", 3, []string{"Marker-0/1"}, time.Minute*6)
		lastBlockAlias = issueBlocks(tf, "0/1-postTSCSeq2", 6, []string{lastBlockAlias}, time.Minute*4)
		tf.PreventNewMarkers(false)
		tf.CreateBlock("Marker-2/2", models.WithStrongParents(tf.BlockIDs(lastBlockAlias)), models.WithIssuingTime(time.Now().Add(-3*time.Minute)))
		tf.IssueBlocks("Marker-2/2").WaitUntilAllTasksProcessed()
		tf.PreventNewMarkers(true)
		lastBlockAlias = issueBlocks(tf, "2/2", 5, []string{"Marker-2/2"}, 0)
		tf.PreventNewMarkers(false)
		tf.CreateBlock("Marker-2/3", models.WithStrongParents(tf.BlockIDs(lastBlockAlias)))
		tf.IssueBlocks("Marker-2/3").WaitUntilAllTasksProcessed()
		tf.PreventNewMarkers(true)
		_ = issueBlocks(tf, "2/3", 5, []string{"Marker-2/3"}, 0)
		tf.PreventNewMarkers(false)

		tf.CheckMarkers(lo.MergeMaps(markersMap, map[string]*markers.Markers{
			"0/1-preTSCSeq2_0":  markers.NewMarkers(markers.NewMarker(0, 1)),
			"0/1-preTSCSeq2_1":  markers.NewMarkers(markers.NewMarker(0, 1)),
			"0/1-preTSCSeq2_2":  markers.NewMarkers(markers.NewMarker(0, 1)),
			"0/1-postTSCSeq2_0": markers.NewMarkers(markers.NewMarker(0, 1)),
			"0/1-postTSCSeq2_1": markers.NewMarkers(markers.NewMarker(0, 1)),
			"0/1-postTSCSeq2_2": markers.NewMarkers(markers.NewMarker(0, 1)),
			"0/1-postTSCSeq2_3": markers.NewMarkers(markers.NewMarker(0, 1)),
			"0/1-postTSCSeq2_4": markers.NewMarkers(markers.NewMarker(0, 1)),
			"0/1-postTSCSeq2_5": markers.NewMarkers(markers.NewMarker(0, 1)),
			"Marker-2/2":        markers.NewMarkers(markers.NewMarker(2, 2)),
			"2/2_0":             markers.NewMarkers(markers.NewMarker(2, 2)),
			"2/2_1":             markers.NewMarkers(markers.NewMarker(2, 2)),
			"2/2_2":             markers.NewMarkers(markers.NewMarker(2, 2)),
			"2/2_3":             markers.NewMarkers(markers.NewMarker(2, 2)),
			"2/2_4":             markers.NewMarkers(markers.NewMarker(2, 2)),
			"Marker-2/3":        markers.NewMarkers(markers.NewMarker(2, 3)),
			"2/3_0":             markers.NewMarkers(markers.NewMarker(2, 3)),
			"2/3_1":             markers.NewMarkers(markers.NewMarker(2, 3)),
			"2/3_2":             markers.NewMarkers(markers.NewMarker(2, 3)),
			"2/3_3":             markers.NewMarkers(markers.NewMarker(2, 3)),
			"2/3_4":             markers.NewMarkers(markers.NewMarker(2, 3)),
		}))
	}

	// SEQUENCE 2 + 0
	{
		tf.CreateBlock("Marker-2/5", models.WithStrongParents(tf.BlockIDs("0/4_4", "2/3_4")))
		tf.IssueBlocks("Marker-2/5").WaitUntilAllTasksProcessed()
		tf.PreventNewMarkers(true)
		_ = issueBlocks(tf, "2/5", 5, []string{"Marker-2/5"}, 0)
		tf.PreventNewMarkers(false)

		tf.CheckMarkers(lo.MergeMaps(markersMap, map[string]*markers.Markers{
			"Marker-2/5": markers.NewMarkers(markers.NewMarker(2, 5)),
			"2/5_0":      markers.NewMarkers(markers.NewMarker(2, 5)),
			"2/5_1":      markers.NewMarkers(markers.NewMarker(2, 5)),
			"2/5_2":      markers.NewMarkers(markers.NewMarker(2, 5)),
			"2/5_3":      markers.NewMarkers(markers.NewMarker(2, 5)),
			"2/5_4":      markers.NewMarkers(markers.NewMarker(2, 5)),
		}))
	}

	// SEQUENCE 3
	{
		tf.PreventNewMarkers(true)
		lastBlockAlias = issueBlocks(tf, "0/1-postTSCSeq3", 5, []string{"0/1-postTSCSeq2_0"}, 0)
		tf.PreventNewMarkers(false)
		tf.CreateBlock("Marker-3/2", models.WithStrongParents(tf.BlockIDs(lastBlockAlias)))
		tf.IssueBlocks("Marker-3/2").WaitUntilAllTasksProcessed()
		tf.PreventNewMarkers(true)
		_ = issueBlocks(tf, "3/2", 5, []string{"Marker-3/2"}, 0)
		tf.PreventNewMarkers(false)

		tf.CheckMarkers(lo.MergeMaps(markersMap, map[string]*markers.Markers{
			"0/1-postTSCSeq3_0": markers.NewMarkers(markers.NewMarker(0, 1)),
			"0/1-postTSCSeq3_1": markers.NewMarkers(markers.NewMarker(0, 1)),
			"0/1-postTSCSeq3_2": markers.NewMarkers(markers.NewMarker(0, 1)),
			"0/1-postTSCSeq3_3": markers.NewMarkers(markers.NewMarker(0, 1)),
			"0/1-postTSCSeq3_4": markers.NewMarkers(markers.NewMarker(0, 1)),
			"Marker-3/2":        markers.NewMarkers(markers.NewMarker(3, 2)),
			"3/2_0":             markers.NewMarkers(markers.NewMarker(3, 2)),
			"3/2_1":             markers.NewMarkers(markers.NewMarker(3, 2)),
			"3/2_2":             markers.NewMarkers(markers.NewMarker(3, 2)),
			"3/2_3":             markers.NewMarkers(markers.NewMarker(3, 2)),
			"3/2_4":             markers.NewMarkers(markers.NewMarker(3, 2)),
		}))
	}

	// SEQUENCE 2 + 0 (two past markers) -> SEQUENCE 4
	{
		tf.PreventNewMarkers(true)
		lastBlockAlias = issueBlocks(tf, "2/3+0/4", 5, []string{"0/4_4", "2/3_4"}, 0)
		tf.CreateBlock("Marker-4/5", models.WithStrongParents(tf.BlockIDs(lastBlockAlias)))
		tf.IssueBlocks("Marker-4/5").WaitUntilAllTasksProcessed()
		tf.PreventNewMarkers(false)

		tf.CheckMarkers(lo.MergeMaps(markersMap, map[string]*markers.Markers{
			"2/3+0/4_0":  markers.NewMarkers(markers.NewMarker(2, 3), markers.NewMarker(0, 4)),
			"2/3+0/4_1":  markers.NewMarkers(markers.NewMarker(2, 3), markers.NewMarker(0, 4)),
			"2/3+0/4_2":  markers.NewMarkers(markers.NewMarker(2, 3), markers.NewMarker(0, 4)),
			"2/3+0/4_3":  markers.NewMarkers(markers.NewMarker(2, 3), markers.NewMarker(0, 4)),
			"Marker-4/5": markers.NewMarkers(markers.NewMarker(4, 5)),
		}))
	}
	// SEQUENCE 5
	{
		tf.PreventNewMarkers(true)
		lastBlockAlias = issueBlocks(tf, "0/1-preTSCSeq5", 6, []string{"0/1-preTSCSeq2_2"}, time.Minute*6)
		tf.PreventNewMarkers(false)
		tf.CreateBlock("Marker-5/2", models.WithStrongParents(tf.BlockIDs(lastBlockAlias)))
		tf.IssueBlocks("Marker-5/2").WaitUntilAllTasksProcessed()
		tf.PreventNewMarkers(true)
		_ = issueBlocks(tf, "5/2", 5, []string{"Marker-5/2"}, 0)
		tf.PreventNewMarkers(false)

		tf.CheckMarkers(lo.MergeMaps(markersMap, map[string]*markers.Markers{
			"0/1-preTSCSeq5_0": markers.NewMarkers(markers.NewMarker(0, 1)),
			"0/1-preTSCSeq5_1": markers.NewMarkers(markers.NewMarker(0, 1)),
			"0/1-preTSCSeq5_2": markers.NewMarkers(markers.NewMarker(0, 1)),
			"0/1-preTSCSeq5_3": markers.NewMarkers(markers.NewMarker(0, 1)),
			"0/1-preTSCSeq5_4": markers.NewMarkers(markers.NewMarker(0, 1)),
			"Marker-5/2":       markers.NewMarkers(markers.NewMarker(5, 2)),
			"5/2_0":            markers.NewMarkers(markers.NewMarker(5, 2)),
			"5/2_1":            markers.NewMarkers(markers.NewMarker(5, 2)),
			"5/2_2":            markers.NewMarkers(markers.NewMarker(5, 2)),
			"5/2_3":            markers.NewMarkers(markers.NewMarker(5, 2)),
			"5/2_4":            markers.NewMarkers(markers.NewMarker(5, 2)),
		}))
	}

	// SEQUENCE 6
	{
		tf.PreventNewMarkers(true)
		lastBlockAlias = issueBlocks(tf, "0/1-postTSCSeq6", 6, []string{"0/1-preTSCSeq2_2"}, 0)
		tf.PreventNewMarkers(false)
		tf.CreateBlock("Marker-6/2", models.WithStrongParents(tf.BlockIDs(lastBlockAlias)))
		tf.IssueBlocks("Marker-6/2").WaitUntilAllTasksProcessed()
		tf.PreventNewMarkers(true)
		_ = issueBlocks(tf, "6/2", 5, []string{"Marker-6/2"}, 0)
		tf.PreventNewMarkers(false)

		tf.CheckMarkers(lo.MergeMaps(markersMap, map[string]*markers.Markers{
			"0/1-postTSCSeq6_0": markers.NewMarkers(markers.NewMarker(0, 1)),
			"0/1-postTSCSeq6_1": markers.NewMarkers(markers.NewMarker(0, 1)),
			"0/1-postTSCSeq6_2": markers.NewMarkers(markers.NewMarker(0, 1)),
			"0/1-postTSCSeq6_3": markers.NewMarkers(markers.NewMarker(0, 1)),
			"0/1-postTSCSeq6_4": markers.NewMarkers(markers.NewMarker(0, 1)),
			"Marker-6/2":        markers.NewMarkers(markers.NewMarker(6, 2)),
			"6/2_0":             markers.NewMarkers(markers.NewMarker(6, 2)),
			"6/2_1":             markers.NewMarkers(markers.NewMarker(6, 2)),
			"6/2_2":             markers.NewMarkers(markers.NewMarker(6, 2)),
			"6/2_3":             markers.NewMarkers(markers.NewMarker(6, 2)),
			"6/2_4":             markers.NewMarkers(markers.NewMarker(6, 2)),
		}))
	}

	// SEQUENCE 7 (without markers)
	{
		_ = issueBlocks(tf, "7/1", 3, []string{"Genesis"}, 0)

		tf.CheckMarkers(lo.MergeMaps(markersMap, map[string]*markers.Markers{
			"7/1_0": markers.NewMarkers(markers.NewMarker(7, 1)),
			"7/1_1": markers.NewMarkers(markers.NewMarker(7, 2)),
			"7/1_2": markers.NewMarkers(markers.NewMarker(7, 3)),
		}))
	}
}

func issueBlocks(tf *TestFramework, blockPrefix string, blockCount int, parents []string, timestampOffset time.Duration) string {
	blockAlias := fmt.Sprintf("%s_%d", blockPrefix, 0)

	tf.CreateBlock(blockAlias, models.WithStrongParents(tf.BlockIDs(parents...)), models.WithIssuingTime(time.Now().Add(-timestampOffset)))
	tf.IssueBlocks(blockAlias).WaitUntilAllTasksProcessed()

	for i := 1; i < blockCount; i++ {
		alias := fmt.Sprintf("%s_%d", blockPrefix, i)
		tf.CreateBlock(alias, models.WithStrongParents(tf.BlockIDs(blockAlias)), models.WithIssuingTime(time.Now().Add(-timestampOffset)))
		tf.IssueBlocks(alias).WaitUntilAllTasksProcessed()
		// fmt.Println("issuing block", tf.Block(alias).ID())
		blockAlias = alias
	}
	return blockAlias
}

func TestTipManager_TimeSinceConfirmation_RootBlockParent(t *testing.T) {
	tf := NewTestFramework(t,
		WithTipManagerOptions(WithTimeSinceConfirmationThreshold(30*time.Second)),
		WithTangleOptions(tangle.WithBookerOptions(booker.WithMarkerManagerOptions(markermanager.WithSequenceManagerOptions[models.BlockID, *booker.Block](markers.WithMaxPastMarkerDistance(10))))),
	)
	tf.engine.EvictionState.AddRootBlock(models.EmptyBlockID)

	tf.CreateBlock("Block1", models.WithStrongParents(tf.BlockIDs("Genesis")), models.WithIssuingTime(time.Now().Add(-50*time.Second)))
	tf.IssueBlocks("Block1").WaitUntilAllTasksProcessed()
	tf.CreateBlock("Block2", models.WithStrongParents(tf.BlockIDs("Block1")), models.WithIssuingTime(time.Now()))
	tf.IssueBlocks("Block2").WaitUntilAllTasksProcessed()
	tf.CreateBlock("Block3", models.WithStrongParents(tf.BlockIDs("Block2")), models.WithIssuingTime(time.Now().Add(5*time.Second)))
	tf.IssueBlocks("Block3").WaitUntilAllTasksProcessed()
	tf.CreateBlock("Block4", models.WithStrongParents(tf.BlockIDs("Block3")), models.WithIssuingTime(time.Now().Add(10*time.Second)))
	tf.IssueBlocks("Block4").WaitUntilAllTasksProcessed()
	tf.CheckMarkers(map[string]*markers.Markers{
		"Block1": markers.NewMarkers(markers.NewMarker(0, 1)),
		"Block2": markers.NewMarkers(markers.NewMarker(0, 2)),
		"Block3": markers.NewMarkers(markers.NewMarker(0, 3)),
		"Block4": markers.NewMarkers(markers.NewMarker(0, 4)),
	})

	acceptedBlockIDsAliases := []string{"Block1", "Block2"}
	acceptedMarkers := []markers.Marker{markers.NewMarker(0, 1), markers.NewMarker(0, 2)}
	tf.SetBlocksAccepted(acceptedBlockIDsAliases...)
	tf.SetMarkersAccepted(acceptedMarkers...)
	tf.SetAcceptedTime(tf.Block("Block2").IssuingTime())
	tf.BlockDAG.EvictionState.AddRootBlock(tf.Block("Block1").ID())
	tf.BlockDAG.EvictionState.EvictUntil(tf.Block("Block1").ID().Index())
	tf.BlockDAG.EvictionState.RemoveRootBlock(models.EmptyBlockID)

	require.Eventually(t, tf.engine.IsBootstrapped, 1*time.Minute, 500*time.Millisecond)

	tf.CreateBlock("Block5", models.WithStrongParents(tf.BlockIDs("Block1")), models.WithIssuingTime(time.Now()))
	tf.IssueBlocks("Block5").WaitUntilAllTasksProcessed()
	tf.CheckMarkers(map[string]*markers.Markers{
		"Block5": markers.NewMarkers(markers.NewMarker(1, 1)),
	})

	tf.AssertIsPastConeTimestampCorrect("Block3", true)
	tf.AssertIsPastConeTimestampCorrect("Block2", true)
	tf.AssertIsPastConeTimestampCorrect("Block1", false)
	tf.AssertIsPastConeTimestampCorrect("Block5", false)
}

func TestTipManager_FutureTips(t *testing.T) {
	// MinimumCommittableAge will also be 10 seconds
	epoch.GenesisTime = time.Now().Add(-100 * time.Second).Unix()
	tf := NewTestFramework(t, WithNotarizationOptions(notarization.WithMinCommittableEpochAge(10*time.Second)))
	tf.engine.EvictionState.AddRootBlock(models.EmptyBlockID)

	tf.engine.Events.NotarizationManager.EpochCommitted.Hook(event.NewClosure(func(details *notarization.EpochCommittedDetails) {
		fmt.Println(">>", details.Commitment.ID())
	}))

	// Let's add a few blocks to epoch 1
	{
		blockTime := time.Unix(epoch.GenesisTime+5, 0)
		tf.CreateBlock("Block1.1", models.WithStrongParents(tf.BlockIDs("Genesis")), models.WithIssuingTime(blockTime))
		tf.CreateBlock("Block1.2", models.WithStrongParents(tf.BlockIDs("Block1.1")), models.WithIssuingTime(blockTime))
		tf.CreateBlock("Block1.3", models.WithStrongParents(tf.BlockIDs("Block1.2")), models.WithIssuingTime(blockTime))
		tf.CreateBlock("Block1.4", models.WithStrongParents(tf.BlockIDs("Block1.2", "Block1.3")), models.WithIssuingTime(blockTime))

		tf.IssueBlocks("Block1.1", "Block1.2", "Block1.3", "Block1.4")
		tf.WaitUntilAllTasksProcessed()
		tf.SetBlocksAccepted("Block1.1", "Block1.2", "Block1.3", "Block1.4")
		tf.SetAcceptedTime(tf.Block("Block1.4").IssuingTime())
		tf.WaitUntilAllTasksProcessed()

		tf.AssertTipsAdded(4)
		tf.AssertTipsRemoved(3)
		tf.AssertTips(tf.BlockIDs("Block1.4"))
	}

	// Let's add a few blocks to epoch 2
	{
		blockTime := time.Unix(epoch.GenesisTime+15, 0)
		tf.CreateBlock("Block2.1", models.WithStrongParents(tf.BlockIDs("Block1.4")), models.WithIssuingTime(blockTime))

		tf.IssueBlocks("Block2.1")
		tf.WaitUntilAllTasksProcessed()
		tf.SetBlocksAccepted("Block2.1")
		tf.SetAcceptedTime(tf.Block("Block2.1").IssuingTime())
		tf.WaitUntilAllTasksProcessed()

		tf.AssertTipsAdded(5)
		tf.AssertTipsRemoved(4)
		tf.AssertTips(tf.BlockIDs("Block2.1"))
	}

	// Let's add a few blocks to epoch 3
	{
		blockTime := time.Unix(epoch.GenesisTime+25, 0)
		tf.CreateBlock("Block3.1", models.WithStrongParents(tf.BlockIDs("Block2.1")), models.WithIssuingTime(blockTime))

		tf.IssueBlocks("Block3.1")
		tf.WaitUntilAllTasksProcessed()
		tf.SetBlocksAccepted("Block3.1")
		tf.SetAcceptedTime(tf.Block("Block3.1").IssuingTime())
		tf.WaitUntilAllTasksProcessed()

		tf.AssertTipsAdded(6)
		tf.AssertTipsRemoved(5)
		tf.AssertTips(tf.BlockIDs("Block3.1"))
	}

	commitment2_1 := tf.FormCommitment(2, []string{}, 1)
	commitment2_2 := tf.FormCommitment(2, []string{"Block2.1"}, 1)
	commitment3_1 := commitment.New(3, commitment2_1.ID(), types.Identifier{1}, 0)

	// Let's introduce a future tip, in epoch 4
	{
		blockTime := time.Unix(epoch.GenesisTime+35, 0)
		tf.CreateBlock("Block4.1", models.WithStrongParents(tf.BlockIDs("Block3.1")), models.WithIssuingTime(blockTime), models.WithCommitment(commitment2_1))
		tf.CreateBlock("Block4.2", models.WithStrongParents(tf.BlockIDs("Block3.1")), models.WithIssuingTime(blockTime), models.WithCommitment(commitment2_2))
		tf.CreateBlock("Block4.3", models.WithStrongParents(tf.BlockIDs("Block3.1")), models.WithIssuingTime(blockTime), models.WithCommitment(commitment3_1))
		tf.CreateBlock("Block4.4", models.WithStrongParents(tf.BlockIDs("Block4.2")), models.WithIssuingTime(blockTime), models.WithCommitment(commitment2_2))

		tf.IssueBlocks("Block4.1", "Block4.2", "Block4.3", "Block4.4")
		tf.WaitUntilAllTasksProcessed()

		tf.AssertTipsAdded(6)
		tf.AssertTipsRemoved(5)
		tf.AssertTips(tf.BlockIDs("Block3.1"))
		tf.AssertFutureTips(map[epoch.Index]map[commitment.ID]models.BlockIDs{
			2: {
				commitment2_1.ID(): tf.BlockIDs("Block4.1"),
				commitment2_2.ID(): tf.BlockIDs("Block4.2", "Block4.4"),
			},
			3: {
				commitment3_1.ID(): tf.BlockIDs("Block4.3"),
			},
		})
	}

	// We accept a block of epoch 4, rendering epoch 2 committable and refreshing the tippool
	{
		tf.SetBlocksAccepted("Block4.2")
		tf.SetAcceptedTime(tf.Block("Block4.2").IssuingTime())
		tf.WaitUntilAllTasksProcessed()

		tf.AssertFutureTips(map[epoch.Index]map[commitment.ID]models.BlockIDs{
			3: {
				commitment3_1.ID(): tf.BlockIDs("Block4.3"),
			},
		})

		tf.AssertTipsAdded(7)
		tf.AssertTipsRemoved(6)
		tf.AssertTips(tf.BlockIDs("Block4.4"))

		tf.AssertEqualBlocks(tf.TipManager.Tips(1), tf.BlockIDs("Block4.4"))
	}
}
