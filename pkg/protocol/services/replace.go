package services

import (
	"context"
	"github.com/imulab/go-scim/pkg/core/errors"
	"github.com/imulab/go-scim/pkg/core/prop"
	"github.com/imulab/go-scim/pkg/protocol/db"
	"github.com/imulab/go-scim/pkg/protocol/event"
	"github.com/imulab/go-scim/pkg/protocol/lock"
	"github.com/imulab/go-scim/pkg/protocol/log"
	"github.com/imulab/go-scim/pkg/protocol/services/filter"
)

type (
	ReplaceRequest struct {
		ResourceID    string
		Payload       *prop.Resource
		MatchCriteria func(resource *prop.Resource) bool
	}
	ReplaceResponse struct {
		Resource   *prop.Resource
		Location   string
		OldVersion string
		NewVersion string
	}
	ReplaceService struct {
		Logger   log.Logger
		Lock     lock.Lock
		Filters  []filter.ForResource
		Database db.DB
		Event    event.Publisher
	}
)

func (s *ReplaceService) ReplaceResource(ctx context.Context, request *ReplaceRequest) (*ReplaceResponse, error) {
	s.Logger.Debug("received replace request [id=%s]", request.ResourceID)

	ref, err := s.Database.Get(ctx, request.ResourceID, nil)
	if err != nil {
		return nil, err
	} else if request.MatchCriteria != nil && !request.MatchCriteria(ref) {
		return nil, errors.PreConditionFailed("resource [id=%s] does not meet pre condition", request.ResourceID)
	}

	defer s.Lock.Unlock(ctx, ref)
	if err := s.Lock.Lock(ctx, ref); err != nil {
		s.Logger.Error("failed to obtain lock for resource [id=%s]: %s", request.ResourceID, err.Error())
		return nil, err
	}

	for _, f := range s.Filters {
		if err := f.FilterRef(ctx, request.Payload, ref); err != nil {
			s.Logger.Error("replace request encounter error during filter for resource [id=%s]: %s", request.ResourceID, err.Error())
			return nil, err
		}
	}

	// Only replace when version is bumped
	if request.Payload.Version() != ref.Version() {
		err = s.Database.Replace(ctx, request.Payload)
		if err != nil {
			s.Logger.Error("resource [id=%s] failed to save into persistence: %s", request.ResourceID, err.Error())
			return nil, err
		}
		s.Logger.Debug("resource [id=%s] saved in persistence", request.ResourceID)

		if s.Event != nil {
			s.Event.ResourceUpdated(ctx, request.Payload)
		}
	}

	return &ReplaceResponse{
		Resource:   request.Payload,
		Location:   request.Payload.Location(),
		OldVersion: ref.Version(),
		NewVersion: request.Payload.Version(),
	}, nil
}
