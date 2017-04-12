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
	ResourcesBaseURL string        `yaml:"resources_base_url"`
	BotName          string        `yaml:"bot_name"`
	InitialDelay     time.Duration `yaml:"initial_delay"`
	Tutorial         struct {
		Steps []struct {
			Type  string        `yaml:"type"`
			Body  string        `yaml:"text"`
			Src   string        `yaml:"src"`
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
	cli         *gomatrix.Client
}

// NewTutorial creates a new Tutorial instance
func NewTutorial(roomID string, userID string, cli *gomatrix.Client) Tutorial {
	t := Tutorial{
		roomID:      roomID,
		userID:      userID,
		currentStep: -1,
		timer:       nil,
		cli:         cli,
	}
	return t
}

func (t *Tutorial) restart() {
	if t.timer != nil {
		t.timer.Stop()
	}
	t.currentStep = -1
	t.queueNextStep(tutorialFlow.InitialDelay)
}

func (t *Tutorial) queueNextStep(delay time.Duration) {
	if t.timer != nil {
		t.timer.Stop()
	}

	if delay > 0 {
		t.timer = time.NewTimer(time.Millisecond * delay)
		<-t.timer.C
		t.nextStep()
	} else {
		t.nextStep()
	}
}

func (t Tutorial) nextStep() {
	t.currentStep++
	// Check that there is a valid mtutorial step to process
	if t.currentStep < len(tutorialFlow.Tutorial.Steps) {
		base := tutorialFlow.ResourcesBaseURL
		step := tutorialFlow.Tutorial.Steps[t.currentStep]
		// Check message type
		switch step.Type {
		case "image":
			msg := gomatrix.ImageMessage{
				MsgType: "m.image",
				Body:    step.Body,
				URL:     base + step.Src,
			}

			if _, e := t.cli.SendMessageEvent(t.roomID, "m.room.message", msg); e != nil {
				log.Print("Failed to send Image message")
			}
		case "notice":
			msg := gomatrix.TextMessage{
				MsgType: "m.notice",
				Body:    step.Body,
			}
			if _, e := t.cli.SendMessageEvent(t.roomID, "m.room.message", msg); e != nil {
				log.Printf("Failed to send Notice message - %s", step.Body)
			}
		default: // text
			msg := gomatrix.TextMessage{
				MsgType: "m.text",
				Body:    step.Body,
			}
			if _, e := t.cli.SendMessageEvent(t.roomID, "m.room.message", msg); e != nil {
				log.Printf("Failed to send Text message - %s", step.Body)
			}
		}

		// TODO -- If last step, clean up tutorial instance

		// Set up timer for next step
		if step.Delay > 0 {
			t.timer = time.NewTimer(time.Millisecond * tutorialFlow.InitialDelay)
		}
	} else {
		log.Println("Tutorial instance ended")
		// End of tutorial -- TODO remove tutorial instance
	}
}

// Commands supported:
//    !start
// Starts the tutorial.
func (e *Service) Commands(cli *gomatrix.Client) []types.Command {
	return []types.Command{
		types.Command{
			Path: []string{"start"},
			Command: func(roomID, userID string, args []string) (interface{}, error) {
				response := initTutorialFlow(cli, roomID, userID)
				return &gomatrix.TextMessage{MsgType: "m.notice", Body: response}, nil
			},
		},
	}
}

func initTutorialFlow(cli *gomatrix.Client, roomID string, userID string) string {
	// Check if there is an existing tutorial for this user and restart it, if found
	for t := range tutorials {
		tutorial := tutorials[t]
		if tutorial.userID == userID {
			tutorial.restart()
			log.Printf("Restarting Riot tutorial %d", t)
			return "Restarting Riot tutorial"
		}
	}
	log.Print("Existing tutorial instance not found for this user")

	// Start a new instance of the riot tutorial
	tutorial := NewTutorial(roomID, userID, cli)
	tutorials = append(tutorials, tutorial)
	go tutorial.queueNextStep(tutorialFlow.InitialDelay)
	log.Printf("Starting Riot tutorial: %v", tutorial)
	return "Starting Riot tutorial"
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
