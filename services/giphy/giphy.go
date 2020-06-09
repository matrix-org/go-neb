// Package giphy implements a Service which adds !commands for Giphy.
package giphy

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/matrix-org/go-neb/types"
	log "github.com/sirupsen/logrus"
	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/event"
	mevt "maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"
)

// ServiceType of the Giphy service.
const ServiceType = "giphy"

type image struct {
	URL string `json:"url"`
	// Giphy returns ints as strings..
	Width  string `json:"width"`
	Height string `json:"height"`
	Size   string `json:"size"`
}

type result struct {
	Slug   string `json:"slug"`
	Images struct {
		Downsized image `json:"downsized"`
		Original  image `json:"original"`
	} `json:"images"`
}

type giphySearch struct {
	Data result `json:"data"`
}

// Service contains the Config fields for the Giphy Service.
//
// Example request:
//   {
//       "api_key": "dc6zaTOxFJmzC",
//       "use_downsized": false
//   }
type Service struct {
	types.DefaultService
	// The Giphy API key to use when making HTTP requests to Giphy.
	// The public beta API key is "dc6zaTOxFJmzC".
	APIKey string `json:"api_key"`
	// Whether to use the downsized image from Giphy.
	// Uses the original image when set to false.
	// Defaults to false.
	UseDownsized bool `json:"use_downsized"`
}

// Commands supported:
//   !giphy some search query without quotes
// Responds with a suitable GIF into the same room as the command.
func (s *Service) Commands(client *mautrix.Client) []types.Command {
	return []types.Command{
		types.Command{
			Path: []string{"giphy"},
			Command: func(roomID id.RoomID, userID id.UserID, args []string) (interface{}, error) {
				return s.cmdGiphy(client, roomID, userID, args)
			},
		},
	}
}

func (s *Service) cmdGiphy(client *mautrix.Client, roomID id.RoomID, userID id.UserID, args []string) (interface{}, error) {
	// only 1 arg which is the text to search for.
	query := strings.Join(args, " ")
	gifResult, err := s.searchGiphy(query)
	if err != nil {
		return nil, err
	}

	image := gifResult.Images.Original
	if s.UseDownsized {
		image = gifResult.Images.Downsized
	}

	if image.URL == "" {
		return nil, fmt.Errorf("No results")
	}
	resUpload, err := client.UploadLink(image.URL)
	if err != nil {
		return nil, err
	}

	return mevt.MessageEventContent{
		MsgType: event.MsgImage,
		Body:    gifResult.Slug,
		URL:     resUpload.ContentURI.CUString(),
		Info: &mevt.FileInfo{
			Height:   asInt(image.Height),
			Width:    asInt(image.Width),
			MimeType: "image/gif",
			Size:     asInt(image.Size),
		},
	}, nil
}

// searchGiphy returns info about a gif
func (s *Service) searchGiphy(query string) (*result, error) {
	log.Info("Searching giphy for ", query)
	u, err := url.Parse("http://api.giphy.com/v1/gifs/translate")
	if err != nil {
		return nil, err
	}
	q := u.Query()
	q.Set("s", query)
	q.Set("api_key", s.APIKey)
	u.RawQuery = q.Encode()
	res, err := http.Get(u.String())
	if res != nil {
		defer res.Body.Close()
	}
	if err != nil {
		return nil, err
	}
	var search giphySearch
	if err := json.NewDecoder(res.Body).Decode(&search); err != nil {
		// Giphy returns a JSON object which has { data: [] } if there are 0 results.
		// This fails to be deserialised by Go.
		return nil, fmt.Errorf("No results")
	}
	return &search.Data, nil
}

func asInt(strInt string) int {
	i64, err := strconv.ParseInt(strInt, 10, 32)
	if err != nil {
		return 0 // default to 0 since these are all just hints to the client
	}
	return int(i64)
}

func init() {
	types.RegisterService(func(serviceID string, serviceUserID id.UserID, webhookEndpointURL string) types.Service {
		return &Service{
			DefaultService: types.NewDefaultService(serviceID, serviceUserID, ServiceType),
		}
	})
}
