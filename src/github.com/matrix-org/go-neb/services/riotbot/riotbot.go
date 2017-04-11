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

// ServiceType of the Riotbot service
const ServiceType = "riotbot"

// "Tutorial flow structure
var tutorialConfig *TutorialConfig

// Tutorial instances
var tutorials []Tutorial

// Tutorial represents the current totorial instances
type Tutorial struct {
	roomID      string
	userID      string
	currentStep int16
	timer       *time.Timer
}

func (t Tutorial) nextStep(cli *gomatrix.Client) {
	msg := gomatrix.TextMessage{
		Body:    "Next tutorial step",
		MsgType: "m.notice",
	}
	if _, e := cli.SendMessageEvent(t.roomID, "m.room.message", msg); e != nil {
		log.Print("Failed to send message")
	}
	return
	// return &gomatrix.TextMessage{MsgType: "m.notice", Body: response}, nil
}

// Service represents the Riotbot service. It has no Config fields.
type Service struct {
	types.DefaultService
}

// TutorialConfig represents the tutorial flow / steps
type TutorialConfig struct {
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
	delay := tutorialConfig.Tutorial.Steps[0].Delay
	timer := time.NewTimer(time.Millisecond * delay)
	tutorial := Tutorial{roomID: roomID, userID: userID, currentStep: 0, timer: timer}
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

	var tutorialConfigFileName = getScriptPath() + "/tutorial.yml"
	tutorialConfigYaml, err := ioutil.ReadFile(tutorialConfigFileName)
	if err != nil {
		log.Fatalf("Failed to read tutorial yaml config file (%s): %v ", tutorialConfigFileName, err)
	}
	log.Printf("Config %s", tutorialConfigYaml)
	if err = yaml.Unmarshal(tutorialConfigYaml, &tutorialConfig); err != nil {
		log.Fatalf("Failed to unmarshal tutorial config yaml: %v", err)
	}
}
