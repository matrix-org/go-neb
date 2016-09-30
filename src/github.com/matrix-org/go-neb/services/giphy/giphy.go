package services

import (
	"encoding/json"
	"errors"
	log "github.com/Sirupsen/logrus"
	"github.com/matrix-org/go-neb/matrix"
	"github.com/matrix-org/go-neb/plugin"
	"github.com/matrix-org/go-neb/types"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

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

type giphyService struct {
	types.DefaultService
	id            string
	serviceUserID string
	APIKey string `json:"api_key"`// beta key is dc6zaTOxFJmzC
}

func (s *giphyService) ServiceUserID() string { return s.serviceUserID }
func (s *giphyService) ServiceID() string     { return s.id }
func (s *giphyService) ServiceType() string   { return "giphy" }

func (s *giphyService) Plugin(client *matrix.Client, roomID string) plugin.Plugin {
	return plugin.Plugin{
		Commands: []plugin.Command{
			plugin.Command{
				Path: []string{"giphy"},
				Command: func(roomID, userID string, args []string) (interface{}, error) {
					return s.cmdGiphy(client, roomID, userID, args)
				},
			},
		},
	}
}
func (s *giphyService) cmdGiphy(client *matrix.Client, roomID, userID string, args []string) (interface{}, error) {
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
func (s *giphyService) searchGiphy(query string) (*result, error) {
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
		return &giphyService{
			id:            serviceID,
			serviceUserID: serviceUserID,
		}
	})
}
