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
			// guggy returns ints as strings..
			Width  string `json:"width"`
			Height string `json:"height"`
			Size   string `json:"size"`
		} `json:"original"`
	} `json:"images"`
}

type guggyQuery struct {
	// "mp4" of "gif"
	Format string `json:"format"`
	// Query sentence
	Sentence  string `json:"sentence"`
}

type guggySearch struct {
	Data []result
}

type guggyService struct {
	id            string
	serviceUserID string
	APIKey        string // key is Cb7aEsJIdjD37c3
}

func (s *guggyService) ServiceUserID() string { return s.serviceUserID }
func (s *guggyService) ServiceID() string     { return s.id }
func (s *guggyService) ServiceType() string   { return "guggy" }
func (s *guggyService) OnReceiveWebhook(w http.ResponseWriter, req *http.Request, cli *matrix.Client) {
}
func (s *guggyService) Register(oldService types.Service, client *matrix.Client) error { return nil }
func (s *guggyService) PostRegister(oldService types.Service)                          {}

func (s *guggyService) Plugin(client *matrix.Client, roomID string) plugin.Plugin {
	return plugin.Plugin{
		Commands: []plugin.Command{
			plugin.Command{
				Path: []string{"guggy"},
				Command: func(roomID, userID string, args []string) (interface{}, error) {
					return s.cmdGuggy(client, roomID, userID, args)
				},
			},
		},
	}
}
func (s *guggyService) cmdGuggy(client *matrix.Client, roomID, userID string, args []string) (interface{}, error) {
	// only 1 arg which is the text to search for.
	querySentence := strings.Join(args, " ")
	gifResult, err := s.text2gifGuggy(querySentence)
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

// text2gifGuggy returns info about a gif
func (s *guggyService) text2gifGuggy(querySentence string) (*result, error) {
	log.Info("Transforming to GIF query ", querySentence)
	u, err := url.Parse("http://text2gif.guggy.com/guggify")
	if err != nil {
		return nil, err
	}

	client := &http.Client{ }

	var query guggyQuery
	query.format = "gif"
	query.sentence = querySentence

	var reqBody bytes.Buffer
	if json.NewEncoder(reqBody).Encode(query); err != nil {
		return nil, err
	}

	req, err := http.NewRequest("POST", u, reqBody)
	if err != nil {
		return nil, err
	}
	req.Header.Add("Content-Type", "application/json")
	req.Header.Add("apiKey", s.APIKey)

	res, err := client.Do(req)
	if res != nil {
		defer res.Body.Close()
	}
	if err != nil {
		return nil, err
	}
	var search guggySearch
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
		return &guggyService{
			id:            serviceID,
			serviceUserID: serviceUserID,
		}
	})
}
