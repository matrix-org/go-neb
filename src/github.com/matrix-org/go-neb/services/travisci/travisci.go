package travisci

import (
	"fmt"
	"net/http"
	"regexp"

	log "github.com/Sirupsen/logrus"
	"github.com/matrix-org/go-neb/database"
	"github.com/matrix-org/go-neb/matrix"
	"github.com/matrix-org/go-neb/types"
)

// ServiceType of the Travis-CI service.
const ServiceType = "travis-ci"

// DefaultTemplate contains the template that will be used if none is supplied.
// This matches the default mentioned at: https://docs.travis-ci.com/user/notifications#Customizing-slack-notifications
const DefaultTemplate = (`%{repository}#%{build_number} (%{branch} - %{commit} : %{author}): %{message}
	Change view : %{compare_url}
	Build details : %{build_url}`)

var ownerRepoRegex = regexp.MustCompile(`^([A-z0-9-_.]+)/([A-z0-9-_.]+)$`)

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

// The payload from Travis-CI
type webhookNotification struct {
	Number         string `json:"number"`
	Status         string `json:"status"`
	StartedAt      string `json:"started_at"`
	FinishedAt     string `json:"finished_at"`
	StatusMessage  string `json:"status_message"`
	Commit         string `json:"commit"`
	Branch         string `json:"branch"`
	Message        string `json:"message"`
	CompareURL     string `json:"compare_url"`
	CommittedAt    string `json:"committed_at"`
	CommitterName  string `json:"committer_name"`
	CommitterEmail string `json:"committer_email"`
	AuthorName     string `json:"author_name"`
	AuthorEmail    string `json:"author_email"`
	Type           string `json:"type"`
	BuildURL       string `json:"build_url"`
	Repository     struct {
		Name      string `json:"name"`
		OwnerName string `json:"owner_name"`
		URL       string `json:"url"`
	} `json:"repository"`
}

// The template variables a user can use in their messages
type notificationTemplate struct {
	RepositorySlug string
	RepositoryName string
	BuildNumber    string
	BuildID        string
	Branch         string
	Commit         string
	Author         string
	CommitMessage  string
	CommitSubject  string
	Result         string
	Message        string
	Duration       string
	ElapsedTime    string
	CompareURL     string
	BuildURL       string
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
	for _, roomData := range s.Rooms {
		for repo := range roomData.Repos {
			match := ownerRepoRegex.FindStringSubmatch(repo)
			if len(match) == 0 {
				return fmt.Errorf("Repository '%s' is not a valid repository name.", repo)
			}
		}
	}
	return nil
}

// PostRegister deletes this service if there are no registered repos.
func (s *Service) PostRegister(oldService types.Service) {
	for _, roomData := range s.Rooms {
		for _ = range roomData.Repos {
			return // at least 1 repo exists
		}
	}
	// Delete this service since no repos are configured
	logger := log.WithFields(log.Fields{
		"service_type": s.ServiceType(),
		"service_id":   s.ServiceID(),
	})
	logger.Info("Removing service as no repositories are registered.")
	if err := database.GetServiceDB().DeleteService(s.ServiceID()); err != nil {
		logger.WithError(err).Error("Failed to delete service")
	}
}

func init() {
	types.RegisterService(func(serviceID, serviceUserID, webhookEndpointURL string) types.Service {
		return &Service{
			DefaultService:     types.NewDefaultService(serviceID, serviceUserID, ServiceType),
			webhookEndpointURL: webhookEndpointURL,
		}
	})
}
