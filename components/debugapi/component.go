package debugapi

import (
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"
	"go.uber.org/dig"

	"github.com/iotaledger/hive.go/app"
	"github.com/iotaledger/hive.go/ierrors"
	"github.com/iotaledger/inx-app/pkg/httpserver"
	"github.com/iotaledger/iota-core/components/restapi"
	"github.com/iotaledger/iota-core/pkg/protocol"
	restapipkg "github.com/iotaledger/iota-core/pkg/restapi"
	iotago "github.com/iotaledger/iota.go/v4"
)

const (
	RouteValidators = "/validators"

	RouteChainManagerAllChainsDot      = "/all-chains"
	RouteChainManagerAllChainsRendered = "/all-chains/rendered"

	RouteCommitmentByIndexBlockIDs = "/commitments/by-index/:" + restapipkg.ParameterSlotIndex + "/blocks"

	RouteCommitmentByIndexTransactionIDs = "/commitments/by-index/:" + restapipkg.ParameterSlotIndex + "/transactions"
)

func init() {
	Component = &app.Component{
		Name:      "DebugAPIV3",
		DepsFunc:  func(cDeps dependencies) { deps = cDeps },
		Configure: configure,
		IsEnabled: func(c *dig.Container) bool {
			return restapi.ParamsRestAPI.Enabled
		},
	}
}

var (
	Component *app.Component
	deps      dependencies

	features = []string{}
)

type dependencies struct {
	dig.In

	Protocol         *protocol.Protocol
	AppInfo          *app.Info
	RestRouteManager *restapi.RestRouteManager
}

func configure() error {
	// check if RestAPI plugin is disabled
	if !Component.App().IsComponentEnabled(restapi.Component.Identifier()) {
		Component.LogPanic("RestAPI plugin needs to be enabled to use the DebugAPIV3 plugin")
	}

	routeGroup := deps.RestRouteManager.AddRoute("debug/v3")

	deps.Protocol.MainEngineInstance().Events.Notarization.SlotCommitted.Hook(storeTransactionsPerSlot)

	routeGroup.GET(RouteValidators, func(c echo.Context) error {
		resp, err := validatorsSummary()
		if err != nil {
			return err
		}

		return httpserver.JSONResponse(c, http.StatusOK, resp)
	}, checkNodeSynced())

	routeGroup.GET(RouteChainManagerAllChainsDot, func(c echo.Context) error {
		resp, err := chainManagerAllChainsDot()
		if err != nil {
			return err
		}

		return c.String(http.StatusOK, resp)
	}, checkNodeSynced())

	routeGroup.GET(RouteChainManagerAllChainsRendered, func(c echo.Context) error {
		renderedBytes, err := chainManagerAllChainsRendered()
		if err != nil {
			return err
		}

		return c.Blob(http.StatusOK, "image/png", renderedBytes)
	}, checkNodeSynced())
	//

	routeGroup.GET(RouteCommitmentByIndexBlockIDs, func(c echo.Context) error {
		indexUint64, err := httpserver.ParseUint64Param(c, restapipkg.ParameterSlotIndex)
		if err != nil {
			return err
		}

		resp, err := getSlotBlockIDs(iotago.SlotIndex(indexUint64))
		if err != nil {
			return err
		}

		return httpserver.JSONResponse(c, http.StatusOK, resp)
	}, checkNodeSynced())

	routeGroup.GET(RouteCommitmentByIndexTransactionIDs, func(c echo.Context) error {
		index, err := httpserver.ParseUint64Param(c, restapipkg.ParameterSlotIndex)
		if err != nil {
			return err
		}

		resp, err := getSlotTransactionIDs(iotago.SlotIndex(index))
		if err != nil {
			return err
		}

		return httpserver.JSONResponse(c, http.StatusOK, resp)
	}, checkNodeSynced())

	return nil
}

// AddFeature adds a feature to the RouteInfo endpoint.
func AddFeature(feature string) {
	features = append(features, strings.ToLower(feature))
}

func checkNodeSynced() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			if !deps.Protocol.SyncManager.IsNodeSynced() {
				return ierrors.Wrap(echo.ErrServiceUnavailable, "node is not synced")
			}

			return next(c)
		}
	}
}

func checkUpcomingUnsupportedProtocolVersion() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			// todo update with protocol upgrades support
			// if !deps.ProtocolManager.NextPendingSupported() {
			//	return ierrors.Wrap(echo.ErrServiceUnavailable, "node does not support the upcoming protocol upgrade")
			// }

			return next(c)
		}
	}
}

func responseByHeader(c echo.Context, obj any) error {
	mimeType, err := httpserver.GetAcceptHeaderContentType(c, httpserver.MIMEApplicationVendorIOTASerializerV1, echo.MIMEApplicationJSON)
	if err != nil && err != httpserver.ErrNotAcceptable {
		return err
	}

	// default to echo.MIMEApplicationJSON
	switch mimeType {
	case httpserver.MIMEApplicationVendorIOTASerializerV1:
		b, err := deps.Protocol.LatestAPI().Encode(obj)
		if err != nil {
			return err
		}

		return c.Blob(http.StatusOK, httpserver.MIMEApplicationVendorIOTASerializerV1, b)

	default:
		j, err := deps.Protocol.LatestAPI().JSONEncode(obj)
		if err != nil {
			return err
		}

		return c.Blob(http.StatusOK, echo.MIMEApplicationJSON, j)
	}
}
