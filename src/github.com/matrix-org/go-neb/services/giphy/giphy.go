// Package giphy implements a Service which adds !commands for Giphy.
package giphy

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	log "github.com/Sirupsen/logrus"
	"github.com/matrix-org/go-neb/matrix"
	"github.com/matrix-org/go-neb/types"
)

// ServiceType of the Giphy service.
const ServiceType = "giphy"

type result struct {
	Slug   string `json:"slug"`
	Images struct {
		Original struct {
			URL string `json:"url"`
			// Giphy returns ints as strings..
			Width  string `json:"width"`
			Height string `json:"height"`
			Size   string `json:"size"`
		} `json:"original"`
	} `json:"images"`
}

type giphySearch struct {
	Data []result
}

// Service contains the Config fields for the Giphy Service.
//
// Example:
//   {
//       "api_key": "dc6zaTOxFJmzC"
//   }
type Service struct {
	types.DefaultService
	// The Giphy API key to use when making HTTP requests to Giphy.
	// The public beta API key is "dc6zaTOxFJmzC".
	APIKey string `json:"api_key"`
}

// Commands supported:
//   !giphy some search query without quotes
// Responds with a suitable GIF into the same room as the command.
func (s *Service) Commands(client *matrix.Client, roomID string) []types.Command {
	return []types.Command{
		types.Command{
			Path: []string{"giphy"},
			Command: func(roomID, userID string, args []string) (interface{}, error) {
				return s.cmdGiphy(client, roomID, userID, args)
			},
		},
	}
}

func (s *Service) cmdGiphy(client *matrix.Client, roomID, userID string, args []string) (interface{}, error) {
	// only 1 arg which is the text to search for.
	query := strings.Join(args, " ")
	gifResult, err := s.searchGiphy(query)
	if err != nil {
		return nil, err
	}
	mxc, err := client.UploadLink(gifResult.Images.Original.URL)
	if err != nil {
		return nil, err
	}

	return matrix.ImageMessage{
		MsgType: "m.image",
		Body:    gifResult.Slug,
		URL:     mxc,
		Info: matrix.ImageInfo{
			Height:   asInt(gifResult.Images.Original.Height),
			Width:    asInt(gifResult.Images.Original.Width),
			Mimetype: "image/gif",
			Size:     asInt(gifResult.Images.Original.Size),
		},
	}, nil
}

// searchGiphy returns info about a gif
func (s *Service) searchGiphy(query string) (*result, error) {
	log.Info("Searching giphy for ", query)
	u, err := url.Parse("http://api.giphy.com/v1/gifs/search")
	if err != nil {
		return nil, err
	}
	q := u.Query()
	q.Set("q", query)
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
		return nil, err
	}
	if len(search.Data) == 0 {
		return nil, errors.New("No results")
	}
	return &search.Data[0], nil
}

func asInt(strInt string) uint {
	u64, err := strconv.ParseUint(strInt, 10, 32)
	if err != nil {
		return 0 // default to 0 since these are all just hints to the client
	}
	return uint(u64)
}

func init() {
	types.RegisterService(func(serviceID, serviceUserID, webhookEndpointURL string) types.Service {
		return &Service{
			DefaultService: types.NewDefaultService(serviceID, serviceUserID, ServiceType),
		}
	})
}
