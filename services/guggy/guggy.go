// Package guggy implements a Service which adds !commands for Guggy.
package guggy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"math"
	"net/http"
	"strings"

	"github.com/matrix-org/go-neb/types"
	"github.com/matrix-org/gomatrix"
	log "github.com/sirupsen/logrus"
)

// ServiceType of the Guggy service
const ServiceType = "guggy"

var httpClient = &http.Client{}

type guggyQuery struct {
	// "mp4" or "gif"
	Format string `json:"format"`
	// Query sentence
	Sentence string `json:"sentence"`
}

type guggyGifResult struct {
	ReqID  string  `json:"reqId"`
	GIF    string  `json:"gif"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

// Service contains the Config fields for the Guggy service.
//
// Example request:
//   {
//       "api_key": "fkweugfyuwegfweyg"
//   }
type Service struct {
	types.DefaultService
	// The Guggy API key to use when making HTTP requests to Guggy.
	APIKey string `json:"api_key"`
}

// Commands supported:
//    !guggy some search query without quotes
// Responds with a suitable GIF into the same room as the command.
func (s *Service) Commands(client *gomatrix.Client) []types.Command {
	return []types.Command{
		types.Command{
			Path: []string{"guggy"},
			Command: func(roomID, userID string, args []string) (interface{}, error) {
				return s.cmdGuggy(client, roomID, userID, args)
			},
		},
	}
}
func (s *Service) cmdGuggy(client *gomatrix.Client, roomID, userID string, args []string) (interface{}, error) {
	// only 1 arg which is the text to search for.
	querySentence := strings.Join(args, " ")
	gifResult, err := s.text2gifGuggy(querySentence)
	if err != nil {
		return nil, fmt.Errorf("Failed to query Guggy: %s", err.Error())
	}

	if gifResult.GIF == "" {
		return gomatrix.TextMessage{
			MsgType: "m.notice",
			Body:    "No GIF found!",
		}, nil
	}

	resUpload, err := client.UploadLink(gifResult.GIF)
	if err != nil {
		return nil, fmt.Errorf("Failed to upload Guggy image to matrix: %s", err.Error())
	}

	return gomatrix.ImageMessage{
		MsgType: "m.image",
		Body:    querySentence,
		URL:     resUpload.ContentURI,
		Info: gomatrix.ImageInfo{
			Height:   uint(math.Floor(gifResult.Height)),
			Width:    uint(math.Floor(gifResult.Width)),
			Mimetype: "image/gif",
		},
	}, nil
}

// text2gifGuggy returns info about a gif
func (s *Service) text2gifGuggy(querySentence string) (*guggyGifResult, error) {
	log.Info("Transforming to GIF query ", querySentence)

	var query guggyQuery
	query.Format = "gif"
	query.Sentence = querySentence

	reqBody, err := json.Marshal(query)
	if err != nil {
		return nil, err
	}

	reader := bytes.NewReader(reqBody)

	req, err := http.NewRequest("POST", "https://text2gif.guggy.com/guggify", reader)
	if err != nil {
		log.Error(err)
		return nil, err
	}
	req.Header.Add("Content-Type", "application/json")
	req.Header.Add("apiKey", s.APIKey)

	res, err := httpClient.Do(req)
	if res != nil {
		defer res.Body.Close()
	}
	if err != nil {
		log.Error(err)
		return nil, err
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		resBytes, err := ioutil.ReadAll(res.Body)
		if err != nil {
			log.WithError(err).Error("Failed to decode Guggy response body")
		}
		log.WithFields(log.Fields{
			"code": res.StatusCode,
			"body": string(resBytes),
		}).Error("Failed to query Guggy")
		return nil, fmt.Errorf("Failed to decode response (HTTP %d)", res.StatusCode)
	}
	var result guggyGifResult
	if err := json.NewDecoder(res.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("Failed to decode response (HTTP %d): %s", res.StatusCode, err.Error())
	}

	return &result, nil
}

func init() {
	types.RegisterService(func(serviceID, serviceUserID, webhookEndpointURL string) types.Service {
		return &Service{
			DefaultService: types.NewDefaultService(serviceID, serviceUserID, ServiceType),
		}
	})
}
