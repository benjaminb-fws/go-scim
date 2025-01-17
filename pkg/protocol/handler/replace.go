package handler

import (
	"github.com/imulab/go-scim/pkg/core/errors"
	"github.com/imulab/go-scim/pkg/core/json"
	"github.com/imulab/go-scim/pkg/core/prop"
	"github.com/imulab/go-scim/pkg/core/spec"
	"github.com/imulab/go-scim/pkg/protocol/http"
	"github.com/imulab/go-scim/pkg/protocol/log"
	"github.com/imulab/go-scim/pkg/protocol/services"
)

type Replace struct {
	Log                 log.Logger
	Service             *services.ReplaceService
	ResourceIDPathParam string
	ResourceType        *spec.ResourceType
}

func (h *Replace) Handle(request http.Request, response http.Response) {
	var (
		resourceIDParam string
		payload         *prop.Resource
	)
	{
		resourceIDParam = request.PathParam(h.ResourceIDPathParam)
		h.Log.Info("request to replace resource [id=%s]", resourceIDParam)

		raw, err := request.Body()
		if err != nil {
			h.Log.Error("failed to read request body for replacing resource [id=%s]: %s", resourceIDParam, err.Error())
			WriteError(response, errors.Internal("failed to read request body"))
			return
		}

		payload = prop.NewResource(h.ResourceType)
		err = json.Deserialize(raw, payload)
		if err != nil {
			h.Log.Error("failed to parse request body for replacing resource [id=%s]: %s", resourceIDParam, err.Error())
			WriteError(response, err)
			return
		}
	}

	rr, err := h.Service.ReplaceResource(request.Context(), &services.ReplaceRequest{
		ResourceID:    resourceIDParam,
		Payload:       payload,
		MatchCriteria: interpretConditionalHeader(request),
	})
	if err != nil {
		WriteError(response, err)
		return
	}

	if rr.NewVersion == rr.OldVersion {
		response.WriteLocation(rr.Location)
		response.WriteETag(rr.NewVersion)
		response.WriteStatus(204)
	} else {
		raw, err := json.Serialize(rr.Resource, json.Options())
		if err != nil {
			WriteError(response, err)
			return
		}
		response.WriteBody(raw)
		response.WriteLocation(rr.Location)
		response.WriteETag(rr.NewVersion)
		response.WriteSCIMContentType()
		response.WriteStatus(200)
	}
}
