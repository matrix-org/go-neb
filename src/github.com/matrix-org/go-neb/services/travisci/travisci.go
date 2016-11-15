package travisci

import (
	"fmt"
	"net/http"
	"sort"
	"strings"

	log "github.com/Sirupsen/logrus"
	"github.com/matrix-org/go-neb/database"
	"github.com/matrix-org/go-neb/matrix"
	"github.com/matrix-org/go-neb/services/github/client"
	"github.com/matrix-org/go-neb/services/github/webhook"
	"github.com/matrix-org/go-neb/types"
	"github.com/matrix-org/go-neb/util"
)

// ServiceType of the Travis-CI service.
const ServiceType = "travis-ci"

// Service contains the Config fields for the Travis-CI service.
//
// This service will send notifications into a Matrix room when Travis-CI sends
// webhook events to it. It requires a public domain which Travis-CI can reach.
// Notices will be sent as the service user ID.
//
// Example JSON request:
//   {
//       rooms: {
//           "!ewfug483gsfe:localhost": {
//               repos: {
//                   "matrix-org/go-neb": {
//                       template: "%{repository}#%{build_number} (%{branch} - %{commit} : %{author}): %{message}\nBuild details : %{build_url}"
//                   }
//               }
//           }
//       }
//   }
type Service struct {
	types.DefaultService
	webhookEndpointURL string
	// A map from Matrix room ID to Github-style owner/repo repositories.
	Rooms map[string]struct {
		// A map of "owner/repo" to configuration information
		Repos map[string]struct {
			// The template string to use when creating notifications.
			//
			// This is identical to the format of Slack Notifications for Travis-CI:
			// https://docs.travis-ci.com/user/notifications#Customizing-slack-notifications
			//
			// The following variables are available:
			//   repository_slug: your GitHub repo identifier (like svenfuchs/minimal)
			//   repository_name: the slug without the username
			//   build_number: build number
			//   build_id: build id
			//   branch: branch build name
			//   commit: shortened commit SHA
			//   author: commit author name
			//   commit_message: commit message of build
			//   commit_subject: first line of the commit message
			//   result: result of build
			//   message: Travis CI message to the build
			//   duration: total duration of all builds in the matrix
			//   elapsed_time: time between build start and finish
			//   compare_url: commit change view URL
			//   build_url: URL of the build detail
			Template string `json:"template"`
		} `json:"repos"`
	} `json:"rooms"`
}

// OnReceiveWebhook receives requests from Travis-CI and possibly sends requests to Matrix as a result.
//
// If the "repository.url" matches a known Github repository, a notification will be formed from the
// template for that repository and a notice will be sent to Matrix.
//
// Go-NEB cannot register with Travis-CI for webhooks automatically. The user must manually add the
// webhook endpoint URL to their .travis.yml file:
//    notifications:
//        webhooks: http://your-domain.com/notifications
//
// See https://docs.travis-ci.com/user/notifications#Webhook-notifications for more information.
func (s *Service) OnReceiveWebhook(w http.ResponseWriter, req *http.Request, cli *matrix.Client) {
	return
}

// Register makes sure the Config information supplied is valid.
func (s *Service) Register(oldService types.Service, client *matrix.Client) error {
	return nil
}

func init() {
	types.RegisterService(func(serviceID, serviceUserID, webhookEndpointURL string) types.Service {
		return &Service{
			DefaultService:     types.NewDefaultService(serviceID, serviceUserID, ServiceType),
			webhookEndpointURL: webhookEndpointURL,
		}
	})
}
