// Package google implements a Service which adds !commands for Google custom search engine.
// Initially this package just supports image search but could be expanded to provide other functionality provided by the Google custom search engine API - https://developers.google.com/custom-search/json-api/v1/overview
package google

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"math"
	"net/http"
	"net/url"
	"strings"

	"github.com/matrix-org/go-neb/types"
	"github.com/matrix-org/gomatrix"
	log "github.com/sirupsen/logrus"
)

// ServiceType of the Google service
const ServiceType = "google"

var httpClient = &http.Client{}

type googleSearchResults struct {
	SearchInformation struct {
		TotalResults int64 `json:"totalResults,string"`
	} `json:"searchInformation"`
	Items []googleSearchResult `json:"items"`
}

type googleSearchResult struct {
	Title       string      `json:"title"`
	HTMLTitle   string      `json:"htmlTitle"`
	Link        string      `json:"link"`
	DisplayLink string      `json:"displayLink"`
	Snippet     string      `json:"snippet"`
	HTMLSnippet string      `json:"htmlSnippet"`
	Mime        string      `json:"mime"`
	FileFormat  string      `json:"fileFormat"`
	Image       googleImage `json:"image"`
}

type googleImage struct {
	ContextLink     string  `json:"contextLink"`
	Height          float64 `json:"height"`
	Width           float64 `json:"width"`
	ByteSize        int64   `json:"byteSize"`
	ThumbnailLink   string  `json:"thumbnailLink"`
	ThumbnailHeight float64 `json:"thumbnailHeight"`
	ThumbnailWidth  float64 `json:"thumbnailWidth"`
}

// Service contains the Config fields for the Google service.
//
// Example request:
//   {
//			"api_key": "AIzaSyA4FD39..."
//			"cx": "ASdsaijwdfASD..."
//   }
type Service struct {
	types.DefaultService
	// The Google API key to use when making HTTP requests to Google.
	APIKey string `json:"api_key"`
	// The Google custom search engine ID
	Cx string `json:"cx"`
}

// Commands supported:
//    !google image some_search_query_without_quotes
// Responds with a suitable image into the same room as the command.
func (s *Service) Commands(client *gomatrix.Client) []types.Command {
	return []types.Command{
		types.Command{
			Path: []string{"google", "image"},
			Command: func(roomID, userID string, args []string) (interface{}, error) {
				return s.cmdGoogleImgSearch(client, roomID, userID, args)
			},
		},
		types.Command{
			Path: []string{"google", "help"},
			Command: func(roomID, userID string, args []string) (interface{}, error) {
				return usageMessage(), nil
			},
		},
		types.Command{
			Path: []string{"google"},
			Command: func(roomID, userID string, args []string) (interface{}, error) {
				return usageMessage(), nil
			},
		},
	}
}

// usageMessage returns a matrix TextMessage representation of the service usage
func usageMessage() *gomatrix.TextMessage {
	return &gomatrix.TextMessage{"m.notice",
		`Usage: !google image image_search_text`}
}

func (s *Service) cmdGoogleImgSearch(client *gomatrix.Client, roomID, userID string, args []string) (interface{}, error) {

	if len(args) < 1 {
		return usageMessage(), nil
	}

	// Get the query text to search for.
	querySentence := strings.Join(args, " ")

	searchResult, err := s.text2imgGoogle(querySentence)

	if err != nil {
		return nil, err
	}

	var imgURL = searchResult.Link
	if imgURL == "" {
		return gomatrix.TextMessage{
			MsgType: "m.notice",
			Body:    "No image found!",
		}, nil
	}

	// FIXME -- Sometimes upload fails with a cryptic error - "msg=Upload request failed code=400"
	resUpload, err := client.UploadLink(imgURL)
	if err != nil {
		return nil, fmt.Errorf("Failed to upload Google image at URL %s (content type %s) to matrix: %s", imgURL, searchResult.Mime, err.Error())
	}

	return gomatrix.ImageMessage{
		MsgType: "m.image",
		Body:    querySentence,
		URL:     resUpload.ContentURI,
		Info: gomatrix.ImageInfo{
			Height:   uint(math.Floor(searchResult.Image.Height)),
			Width:    uint(math.Floor(searchResult.Image.Width)),
			Mimetype: searchResult.Mime,
		},
	}, nil
}

// text2imgGoogle returns info about an image
func (s *Service) text2imgGoogle(query string) (*googleSearchResult, error) {
	log.Info("Searching Google for an image of a ", query)

	u, err := url.Parse("https://www.googleapis.com/customsearch/v1")
	if err != nil {
		return nil, err
	}

	q := u.Query()
	q.Set("q", query)            // String to search for
	q.Set("num", "1")            // Just return 1 image result
	q.Set("start", "1")          // No search result offset
	q.Set("imgSize", "large")    // Just search for medium size images
	q.Set("searchType", "image") // Search for images

	q.Set("key", s.APIKey) // Set the API key for the request
	q.Set("cx", s.Cx)      // Set the custom search engine ID

	u.RawQuery = q.Encode()
	// log.Info("Request URL: ", u)

	res, err := httpClient.Get(u.String())
	if res != nil {
		defer res.Body.Close()
	}
	if err != nil {
		return nil, err
	}
	if res.StatusCode > 200 {
		return nil, fmt.Errorf("Request error: %d, %s", res.StatusCode, response2String(res))
	}
	var searchResults googleSearchResults

	// log.Info(response2String(res))
	if err := json.NewDecoder(res.Body).Decode(&searchResults); err != nil {
		return nil, fmt.Errorf("ERROR - %s", err.Error())
	} else if len(searchResults.Items) < 1 {
		return nil, fmt.Errorf("No images found")
	}

	// Return only the first search result
	return &searchResults.Items[0], nil
}

// response2String returns a string representation of an HTTP response body
func response2String(res *http.Response) string {
	bs, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return "Failed to decode response body"
	}
	str := string(bs)
	return str
}

// Initialise the service
func init() {
	types.RegisterService(func(serviceID, serviceUserID, webhookEndpointURL string) types.Service {
		return &Service{
			DefaultService: types.NewDefaultService(serviceID, serviceUserID, ServiceType),
		}
	})
}
