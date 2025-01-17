package handler

import (
	"context"
	"encoding/json"
	scimJSON "github.com/imulab/go-scim/pkg/core/json"
	"github.com/imulab/go-scim/pkg/core/prop"
	"github.com/imulab/go-scim/pkg/core/spec"
	"github.com/imulab/go-scim/pkg/protocol/db"
	"github.com/imulab/go-scim/pkg/protocol/http"
	"github.com/imulab/go-scim/pkg/protocol/log"
	"github.com/imulab/go-scim/pkg/protocol/services"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"io/ioutil"
	"net/http/httptest"
	"os"
	"testing"
)

func TestDeleteHandler(t *testing.T) {
	s := new(DeleteHandlerTestSuite)
	s.resourceBase = "../../tests/delete_handler_test_suite"
	suite.Run(t, s)
}

type DeleteHandlerTestSuite struct {
	suite.Suite
	resourceBase string
}

func (s *DeleteHandlerTestSuite) TestDelete() {
	_ = s.mustSchema("/user_schema.json")
	resourceType := s.mustResourceType("/user_resource_type.json")
	spc := s.mustServiceProviderConfig("/service_provider_config.json")

	tests := []struct{
		name		string
		getHandler	func(t *testing.T) *Delete
		getRequest	func(t *testing.T) http.Request
		expect		func(t *testing.T, rr *httptest.ResponseRecorder)
	}{
		{
			name: 	"delete resource",
			getHandler: func(t *testing.T) *Delete {
				database := db.Memory()
				err := database.Insert(context.Background(), s.mustResource("/user_001.json", resourceType))
				require.Nil(t, err)
				return &Delete{
					Log:                 log.None(),
					ResourceIDPathParam: "userId",
					Service:             &services.DeleteService{
						Logger:   log.None(),
						Database: database,
						ServiceProviderConfig: spc,
					},
				}
			},
			getRequest: func(t *testing.T) http.Request {
				return http.DefaultRequest(
					httptest.NewRequest("DELETE", "/Users/a5866759-32ca-4e2a-9808-a0fe74f94b18", nil),
					[]string{"/Users/(?P<userId>.*)"},
				)
			},
			expect: func(t *testing.T, rr *httptest.ResponseRecorder) {
				assert.Equal(t, 204, rr.Result().StatusCode)
			},
		},
		{
			name: 	"delete non-existing resource",
			getHandler: func(t *testing.T) *Delete {
				return &Delete{
					Log:                 log.None(),
					ResourceIDPathParam: "userId",
					Service:             &services.DeleteService{
						Logger:   log.None(),
						Database: db.Memory(),
						ServiceProviderConfig: spc,
					},
				}
			},
			getRequest: func(t *testing.T) http.Request {
				return http.DefaultRequest(
					httptest.NewRequest("DELETE", "/Users/foobar", nil),
					[]string{"/Users/(?P<userId>.*)"},
				)
			},
			expect: func(t *testing.T, rr *httptest.ResponseRecorder) {
				assert.Equal(t, 404, rr.Result().StatusCode)
			},
		},
	}

	for _, test := range tests {
		s.T().Run(test.name, func(t *testing.T) {
			rr := httptest.NewRecorder()
			resp := http.DefaultResponse(rr)
			test.getHandler(t).Handle(test.getRequest(t), resp)
			test.expect(t, rr)
		})
	}
}

func (s *DeleteHandlerTestSuite) mustResource(filePath string, resourceType *spec.ResourceType) *prop.Resource {
	f, err := os.Open(s.resourceBase + filePath)
	s.Require().Nil(err)

	raw, err := ioutil.ReadAll(f)
	s.Require().Nil(err)

	resource := prop.NewResource(resourceType)
	err = scimJSON.Deserialize(raw, resource)
	s.Require().Nil(err)

	return resource
}

func (s *DeleteHandlerTestSuite) mustResourceType(filePath string) *spec.ResourceType {
	f, err := os.Open(s.resourceBase + filePath)
	s.Require().Nil(err)

	raw, err := ioutil.ReadAll(f)
	s.Require().Nil(err)

	rt := new(spec.ResourceType)
	err = json.Unmarshal(raw, rt)
	s.Require().Nil(err)

	return rt
}

func (s *DeleteHandlerTestSuite) mustSchema(filePath string) *spec.Schema {
	f, err := os.Open(s.resourceBase + filePath)
	s.Require().Nil(err)

	raw, err := ioutil.ReadAll(f)
	s.Require().Nil(err)

	sch := new(spec.Schema)
	err = json.Unmarshal(raw, sch)
	s.Require().Nil(err)

	spec.SchemaHub.Put(sch)

	return sch
}

func (s *DeleteHandlerTestSuite) mustServiceProviderConfig(filePath string) *spec.ServiceProviderConfig {
	f, err := os.Open(s.resourceBase + filePath)
	s.Require().Nil(err)

	raw, err := ioutil.ReadAll(f)
	s.Require().Nil(err)

	spc := new(spec.ServiceProviderConfig)
	err = json.Unmarshal(raw, spc)
	s.Require().Nil(err)

	return spc
}
