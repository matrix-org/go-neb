// Package alertmanager implements a Service capable of processing webhooks from prometheus alertmanager.
package alertmanager

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/matrix-org/go-neb/database"
	"github.com/matrix-org/go-neb/types"
	"github.com/matrix-org/gomatrix"
	log "github.com/sirupsen/logrus"
	html "html/template"
	"net/http"
	"strings"
	text "text/template"
)

// ServiceType of the Alertmanager service.
const ServiceType = "alertmanager"

// Service contains the Config fields for the Alertmanager service.
//
// This service will send notifications into a Matrix room when Alertmanager sends
// webhook events to it. It requires a public domain which Alertmanager can reach.
// Notices will be sent as the service user ID.
//
// For the template strings, take a look at https://golang.org/pkg/text/template/
// and the html variant https://golang.org/pkg/html/template/.
// The data they get is a webhookNotification
//
// You can set msg_type to either m.text or m.notice
//
// Example JSON request:
//    {
//        rooms: {
//            "!ewfug483gsfe:localhost": {
//                "text_template": "your plain text template goes here",
//                "html_template": "your html template goes here",
//                "msg_type": "m.text"
//            },
//        }
//    }
type Service struct {
	types.DefaultService
	webhookEndpointURL string
	// The URL which should be added to alertmanagers config - Populated by Go-NEB after Service registration.
	WebhookURL string `json:"webhook_url"`
	// A map of matrix rooms to templates
	Rooms map[string]struct {
		TextTemplate string `json:"text_template"`
		HTMLTemplate string `json:"html_template"`
		MsgType      string `json:"msg_type"`
	} `json:"rooms"`
}

// WebhookNotification is the payload from Alertmanager
type WebhookNotification struct {
	Version           string            `json:"version"`
	GroupKey          string            `json:"groupKey"`
	Status            string            `json:"status"`
	Receiver          string            `json:"receiver"`
	GroupLabels       map[string]string `json:"groupLabels"`
	CommonLabels      map[string]string `json:"commonLabels"`
	CommonAnnotations map[string]string `json:"commonAnnotations"`
	ExternalURL       string            `json:"externalURL"`
	Alerts            []struct {
		Status       string            `json:"status"`
		Labels       map[string]string `json:"labels"`
		Annotations  map[string]string `json:"annotations"`
		StartsAt     string            `json:"startsAt"`
		EndsAt       string            `json:"endsAt"`
		GeneratorURL string            `json:"generatorURL"`
		SilenceURL   string
	} `json:"alerts"`
}

// OnReceiveWebhook receives requests from Alertmanager and sends requests to Matrix as a result.
func (s *Service) OnReceiveWebhook(w http.ResponseWriter, req *http.Request, cli *gomatrix.Client) {
	decoder := json.NewDecoder(req.Body)
	var notif WebhookNotification
	if err := decoder.Decode(&notif); err != nil {
		log.WithError(err).Error("Alertmanager webhook received an invalid JSON payload")
		w.WriteHeader(400)
		return
	}

	// add the silence link for each alert
	// see 'newSilenceFromAlertLabels' in
	// https://github.com/prometheus/alertmanager/blob/master/ui/app/src/Views/SilenceForm/Parsing.elm
	for i := range notif.Alerts {
		alert := &notif.Alerts[i]
		filters := []string{}
		for label, val := range alert.Labels {
			filters = append(filters, fmt.Sprintf("%s%%3D\"%s\"", label, val))
		}
		alert.SilenceURL = fmt.Sprintf("%s#silences/new?filter={%s}", notif.ExternalURL, strings.Join(filters, ","))
	}

	for roomID, templates := range s.Rooms {
		var msg interface{}
		// we don't check whether the templates parse because we already did when storing them in the db
		textTemplate, _ := text.New("textTemplate").Parse(templates.TextTemplate)
		var bodyBuffer bytes.Buffer
		if err := textTemplate.Execute(&bodyBuffer, notif); err != nil {
			log.WithError(err).Error("Alertmanager webhook failed to execute text template")
			w.WriteHeader(500)
			return
		}
		if templates.HTMLTemplate != "" {
			// we don't check whether the templates parse because we already did when storing them in the db
			htmlTemplate, _ := html.New("htmlTemplate").Parse(templates.HTMLTemplate)
			var formattedBodyBuffer bytes.Buffer
			if err := htmlTemplate.Execute(&formattedBodyBuffer, notif); err != nil {
				log.WithError(err).Error("Alertmanager webhook failed to execute HTML template")
				w.WriteHeader(500)
				return
			}
			msg = gomatrix.HTMLMessage{
				Body:          bodyBuffer.String(),
				MsgType:       templates.MsgType,
				Format:        "org.matrix.custom.html",
				FormattedBody: formattedBodyBuffer.String(),
			}
		} else {
			msg = gomatrix.TextMessage{
				Body:    bodyBuffer.String(),
				MsgType: templates.MsgType,
			}
		}

		log.WithFields(log.Fields{
			"message": msg,
			"room_id": roomID,
		}).Print("Sending Alertmanager notification to room")
		if _, e := cli.SendMessageEvent(roomID, "m.room.message", msg); e != nil {
			log.WithError(e).WithField("room_id", roomID).Print(
				"Failed to send Alertmanager notification to room.")
		}
	}
	w.WriteHeader(200)
}

// Register makes sure the Config information supplied is valid.
func (s *Service) Register(oldService types.Service, client *gomatrix.Client) error {
	s.WebhookURL = s.webhookEndpointURL
	for _, templates := range s.Rooms {
		// validate that we have at least a plain text template
		if templates.TextTemplate == "" {
			return fmt.Errorf("plain text template missing")
		}

		// validate the plain text template is valid
		_, err := text.New("textTemplate").Parse(templates.TextTemplate)
		if err != nil {
			return fmt.Errorf("plain text template is invalid: %v", err)
		}

		if templates.HTMLTemplate != "" {
			// validate that the html template is valid
			_, err := html.New("htmlTemplate").Parse(templates.HTMLTemplate)
			if err != nil {
				return fmt.Errorf("html template is invalid: %v", err)
			}
		}
		// validate that the msgtype is either m.notice or m.text
		if templates.MsgType != "m.notice" && templates.MsgType != "m.text" {
			return fmt.Errorf("msg_type is neither 'm.notice' nor 'm.text'")
		}
	}
	s.joinRooms(client)
	return nil
}

// PostRegister deletes this service if there are no registered repos.
func (s *Service) PostRegister(oldService types.Service) {
	// At least one room still active
	if len(s.Rooms) > 0 {
		return
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
