package handler

import (
	"encoding/json"
	"github.com/imulab/go-scim/pkg/core/spec"
	"github.com/imulab/go-scim/pkg/protocol/http"
	"github.com/imulab/go-scim/pkg/protocol/log"
)

type ServiceProviderConfig struct {
	Log   log.Logger
	SPC   *spec.ServiceProviderConfig
	cache []byte
}

func (h *ServiceProviderConfig) Handle(_ http.Request, response http.Response) {
	h.Log.Info("get service provider config")

	if len(h.cache) == 0 {
		if raw, err := json.Marshal(h.SPC); err != nil {
			WriteError(response, err)
			return
		} else {
			h.cache = raw
		}
	}

	response.WriteBody(h.cache)
	response.WriteSCIMContentType()
	response.WriteStatus(200)
}
