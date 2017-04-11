// Package riotbot implements a Service for user onboarding in Riot.
package riotbot

import (
	"io/ioutil"
	"log"
	"path/filepath"
	"runtime"
	"time"

	yaml "gopkg.in/yaml.v2"

	"github.com/matrix-org/go-neb/types"
	"github.com/matrix-org/gomatrix"
)

// Service represents the Riotbot service. It has no Config fields.
type Service struct {
	types.DefaultService
}

// TutorialFlow represents the tutorial flow / steps
type TutorialFlow struct {
	ResourcesBaseURL string `yaml:"resources_base_url"`
	Tutorial         struct {
		Steps []struct {
			Text  string        `yaml:"text"`
			Image string        `yaml:"image"`
			Sound string        `yaml:"sound"`
			Video string        `yaml:"video"`
			Delay time.Duration `yaml:"delay"`
		} `yaml:"steps"`
	} `yaml:"tutorial"`
}

// ServiceType of the Riotbot service
const ServiceType = "riotbot"

// "Tutorial flow structure
var tutorialFlow *TutorialFlow

// Tutorial instances
var tutorials []Tutorial

// Tutorial represents the current totorial instances
type Tutorial struct {
	roomID      string
	userID      string
	currentStep int
	timer       *time.Timer
}

// NewTutorial creates a new Tutorial instance
func NewTutorial(roomID string, userID string, timer *time.Timer) Tutorial {
	t := Tutorial{
		roomID:      roomID,
		userID:      userID,
		currentStep: -1,
		timer:       timer,
	}
	return t
}

func (t Tutorial) nextStep(cli *gomatrix.Client) {
	t.currentStep++
	// Check that there is a valid mtutorial step to process
	if t.currentStep < len(tutorialFlow.Tutorial.Steps) {
		base := tutorialFlow.ResourcesBaseURL
		step := tutorialFlow.Tutorial.Steps[t.currentStep]
		// Check message type
		if step.Image != "" {
			msg := gomatrix.ImageMessage{
				MsgType: "m.image",
				Body:    "Hi I am Riotbot",
				URL:     base + step.Image,
			}

			log.Printf("Sending message %v", msg)
			if _, e := cli.SendMessageEvent(t.roomID, "m.room.message", msg); e != nil {
				log.Print("Failed to send image message")
			}
		} else {
			msg := gomatrix.TextMessage{
				MsgType: "m.notice",
				Body:    "Next tutorial step",
			}
			if _, e := cli.SendMessageEvent(t.roomID, "m.room.message", msg); e != nil {
				log.Print("Failed to send message")
			}
		}

		// TODO -- If last step, clean up tutorial instance
	} else {
		// End of tutorial -- TODO remove tutorial instance
	}
}

// Commands supported:
//    !help some request
// Responds with some user help.
func (e *Service) Commands(cli *gomatrix.Client) []types.Command {
	return []types.Command{
		types.Command{
			Path: []string{"help"},
			Command: func(roomID, userID string, args []string) (interface{}, error) {
				response := initTutorialFlow(cli, roomID, userID)
				return &gomatrix.TextMessage{MsgType: "m.notice", Body: response}, nil
			},
		},
	}
}

func initTutorialFlow(cli *gomatrix.Client, roomID string, userID string) string {
	delay := tutorialFlow.Tutorial.Steps[0].Delay
	timer := time.NewTimer(time.Millisecond * delay)
	tutorial := NewTutorial(roomID, userID, timer)
	tutorials = append(tutorials, tutorial)
	go func(tutorial Tutorial) {
		<-timer.C
		tutorial.nextStep(cli)
	}(tutorial)
	log.Printf("Starting tutorial: %v", tutorial)
	return "Starting tutorial"
}

func getScriptPath() string {
	_, script, _, ok := runtime.Caller(1)
	if !ok {
		log.Fatal("Failed to get script dir")
	}

	return filepath.Dir(script)
}

func init() {
	types.RegisterService(func(serviceID, serviceUserID, webhookEndpointURL string) types.Service {
		return &Service{
			DefaultService: types.NewDefaultService(serviceID, serviceUserID, ServiceType),
		}
	})

	var tutorialFlowFileName = getScriptPath() + "/tutorial.yml"
	tutorialFlowYaml, err := ioutil.ReadFile(tutorialFlowFileName)
	if err != nil {
		log.Fatalf("Failed to read tutorial yaml config file (%s): %v ", tutorialFlowFileName, err)
	}
	if err = yaml.Unmarshal(tutorialFlowYaml, &tutorialFlow); err != nil {
		log.Fatalf("Failed to unmarshal tutorial config yaml: %v", err)
	}
}
