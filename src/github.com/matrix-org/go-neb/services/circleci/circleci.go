// Package circleci implements a Service capable of processing webhooks from CircleCI.
package circleci

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	log "github.com/Sirupsen/logrus"
	"github.com/matrix-org/go-neb/database"
	"github.com/matrix-org/go-neb/types"
	"github.com/matrix-org/gomatrix"
	"io/ioutil"
	"strconv"
)

// ServiceType of the CircleCI service.
const ServiceType = "circleci"

// DefaultTemplate contains the template that will be used if none is supplied.
// This matches the default mentioned at: https://docs.travis-ci.com/user/notifications#Customizing-slack-notifications
const DefaultTemplate = (`%{repository}#%{build_number} (%{branch} - %{commit} : %{author}): %{message}
	Build details : %{build_url}`)

// Matches 'owner/repo'
var ownerRepoRegex = regexp.MustCompile(`^([A-z0-9-_.]+)/([A-z0-9-_.]+)$`)

var httpClient = &http.Client{}

// Service contains the Config fields for the Travis-CI service.
//
// This service will send notifications into a Matrix room when CircleCI sends
// webhook events to it. It requires a public domain which CircleCI can reach.
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
	// The URL which should be added to .circleci/config.yml - Populated by Go-NEB after Service registration.
	WebhookURL string `json:"webhook_url"`
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
			//   repository_slug: your Git* repo identifier (like svenfuchs/minimal)
			//   repository_name: the slug without the username
			//   build_number: build number
			//   build_id: build id
			//   branch: branch build name
			//   commit: shortened commit SHA
			//   author: commit author name
			//   commit_message: commit message of build
			//   commit_subject: first line of the commit message
			//   result: result of build
			//   message: CircleCI message to the build
			//   duration: total duration of all builds in the matrix
			//   elapsed_time: time between build start and finish
			//   compare_url: commit change view URL
			//   build_url: URL of the build detail
			Template string `json:"template"`
		} `json:"repos"`
	} `json:"rooms"`
}

// Converts a webhook notification into a map of template var name to value
func notifToTemplate(n WebhookNotification) map[string]string {
	t := make(map[string]string)
	//Get Payload to variable
	p := n.Payload
	t["repository_slug"] = p.Username + "/" + p.Reponame
	t["repository"] = t["repository_slug"] // Deprecated form but still used everywhere in people's templates
	t["repository_name"] = p.Reponame
	t["build_number"] = strconv.Itoa(p.BuildNum)
	t["build_id"] = t["build_number"] // CircleCI doesn't have a difference between number and ID but to be consistent with TravisCI
	t["branch"] = p.Branch
	shaLength := len(p.VcsRevision)
	if shaLength > 10 {
		shaLength = 10
	}
	t["commit"] = p.VcsRevision[:shaLength] // shortened commit SHA
	t["author"] = p.CommitterName      // author: commit author name
	// commit_message: commit message of build
	// commit_subject: first line of the commit message
	t["commit_message"] = p.Body
	subjAndMsg := strings.SplitN(p.Body, "\n", 2)
	t["commit_subject"] = subjAndMsg[0]
	if p.Status != "" {
		t["result"] = p.Status
	}
	t["message"] = p.Outcome // message: Travis CI message to the build

	if !p.StartTime.IsZero()  && !p.StopTime.IsZero() {
			t["duration"] = p.StopTime.Sub(p.StartTime).String()
			t["elapsed_time"] = t["duration"]
	}

	t["build_url"] = p.BuildURL
	return t
}

func outputForTemplate(circleciTmpl string, tmpl map[string]string) (out string) {
	if circleciTmpl == "" {
		circleciTmpl = DefaultTemplate
	}
	out = circleciTmpl
	for tmplVar, tmplValue := range tmpl {
		out = strings.Replace(out, "%{"+tmplVar+"}", tmplValue, -1)
	}
	return out
}

// OnReceiveWebhook receives requests from CircleCI and possibly sends requests to Matrix as a result.
//
// If the repository matches a known Git* repository, a notification will be formed from the
// template for that repository and a notice will be sent to Matrix.
//
// Go-NEB cannot register with CircleCI for webhooks automatically. The user must manually add the
// webhook endpoint URL to their .circleci/config.yml file:
//    notify:
//		webhooks:
//			- url: https://example.com/hooks/circle
//
// See https://circleci.com/docs/1.0/configuration/#notify for more information.
func (s *Service) OnReceiveWebhook(w http.ResponseWriter, req *http.Request, cli *gomatrix.Client) {
	body, err := ioutil.ReadAll(req.Body)
	if err != nil {
		log.WithFields(log.Fields{
			"Body":  req.Body,
			log.ErrorKey: err,
		}).Warn("Failed to Read Body")
		w.WriteHeader(403)
		return
	}
	//fmt.Printf("%s\n", body)

	var notif WebhookNotification
	if err := json.Unmarshal(body, &notif); err != nil {
		log.WithError(err).Error("CircleCI webhook received an invalid JSON body")
		w.WriteHeader(400)
		return
	}
	if notif.Payload.Username == "" || notif.Payload.Reponame == "" {
		log.WithField("repo", notif.Payload).Error("CircleCI webhook missing repository fields")
		w.WriteHeader(400)
		return
	}
	whForRepo := notif.Payload.Username + "/" + notif.Payload.Reponame
	tmplData := notifToTemplate(notif)

	logger := log.WithFields(log.Fields{
		"repo": whForRepo,
	})

	for roomID, roomData := range s.Rooms {
		for ownerRepo, repoData := range roomData.Repos {
			if ownerRepo != whForRepo {
				continue
			}
			msg := gomatrix.TextMessage{
				Body:    outputForTemplate(repoData.Template, tmplData),
				MsgType: "m.notice",
			}

			logger.WithFields(log.Fields{
				"message": msg,
				"room_id": roomID,
			}).Print("Sending CircleCI notification to room")
			if _, e := cli.SendMessageEvent(roomID, "m.room.message", msg); e != nil {
				logger.WithError(e).WithField("room_id", roomID).Print(
					"Failed to send CircleCI notification to room.")
			}
		}
	}
	w.WriteHeader(200)
}

// Register makes sure the Config information supplied is valid.
func (s *Service) Register(oldService types.Service, client *gomatrix.Client) error {
	s.WebhookURL = s.webhookEndpointURL
	for _, roomData := range s.Rooms {
		for repo := range roomData.Repos {
			match := ownerRepoRegex.FindStringSubmatch(repo)
			if len(match) == 0 {
				return fmt.Errorf("Repository '%s' is not a valid repository name.", repo)
			}
		}
	}
	s.joinRooms(client)
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

func (s *Service) joinRooms(client *gomatrix.Client) {
	for roomID := range s.Rooms {
		if _, err := client.JoinRoom(roomID, "", nil); err != nil {
			log.WithFields(log.Fields{
				log.ErrorKey: err,
				"room_id":    roomID,
				"user_id":    client.UserID,
			}).Error("Failed to join room")
		}
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