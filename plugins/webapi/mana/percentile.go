package mana

import (
	"net/http"
	"time"

	"github.com/labstack/echo"
	"github.com/mr-tron/base58"
	"github.com/pkg/errors"

	"github.com/iotaledger/hive.go/core/generics/lo"
	"github.com/iotaledger/hive.go/core/identity"

	"github.com/iotaledger/goshimmer/packages/app/jsonmodels"
	"github.com/iotaledger/goshimmer/packages/protocol/engine/throughputquota/mana1/manamodels"
)

// getPercentileHandler handles the request.
func getPercentileHandler(c echo.Context) error {
	var request jsonmodels.GetPercentileRequest
	if err := c.Bind(&request); err != nil {
		return c.JSON(http.StatusBadRequest, jsonmodels.GetPercentileResponse{Error: err.Error()})
	}
	ID, err := identity.DecodeIDBase58(request.IssuerID)
	if err != nil {
		return c.JSON(http.StatusBadRequest, jsonmodels.GetPercentileResponse{Error: err.Error()})
	}
	if request.IssuerID == "" {
		ID = deps.Local.ID()
	}

	accessPercentile := manamodels.Percentile(ID, deps.Protocol.Engine().ThroughputQuota.BalanceByIDs())
	consensusPercentile := manamodels.Percentile(ID, lo.PanicOnErr(deps.Protocol.Engine().SybilProtection.Weights().Map()))
	if err != nil {
		if errors.Is(err, manamodels.ErrIssuerNotFoundInBaseManaVector) {
			consensusPercentile = 0
		} else {
			return c.JSON(http.StatusBadRequest, jsonmodels.GetManaResponse{Error: err.Error()})
		}
	}
	return c.JSON(http.StatusOK, jsonmodels.GetPercentileResponse{
		ShortIssuerID:      ID.String(),
		IssuerID:           base58.Encode(lo.PanicOnErr(ID.Bytes())),
		Access:             accessPercentile,
		AccessTimestamp:    time.Now().Unix(),
		Consensus:          consensusPercentile,
		ConsensusTimestamp: time.Now().Unix(),
	})
}
