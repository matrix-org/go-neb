// Package imgur implements a Service which adds !commands for imgur image search
package imgur

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"math/rand"
	"net/http"
	"net/url"
	"strings"

	"github.com/matrix-org/go-neb/types"
	"github.com/matrix-org/gomatrix"
	log "github.com/sirupsen/logrus"
)

// ServiceType of the Imgur service
const ServiceType = "imgur"

var httpClient = &http.Client{}

// Represents an Imgur Gallery Image
type imgurGalleryImage struct {
	ID          string `json:"id"`          // The ID for the image
	Title       string `json:"title"`       // The title of the image.
	Description string `json:"description"` // Description of the image.
	DateTime    int64  `json:"datetime"`    // Time inserted into the gallery, epoch time
	Type        string `json:"type"`        // Image MIME type.
	Animated    *bool  `json:"animated"`    // Is the image animated
	Width       int    `json:"width"`       // The width of the image in pixels
	Height      int    `json:"height"`      // The height of the image in pixels
	Size        int64  `json:"size"`        // The size of the image in bytes
	Views       int64  `json:"views"`       // The number of image views
	Link        string `json:"link"`        // The direct link to the the image. (Note: if fetching an animated GIF that was over 20MB in original size, a .gif thumbnail will be returned)
	Gifv        string `json:"gifv"`        // OPTIONAL, The .gifv link. Only available if the image is animated and type is 'image/gif'.
	MP4         string `json:"mp4"`         // OPTIONAL, The direct link to the .mp4. Only available if the image is animated and type is 'image/gif'.
	MP4Size     int64  `json:"mp4_size"`    // OPTIONAL, The Content-Length of the .mp4. Only available if the image is animated and type is 'image/gif'. Note that a zero value (0) is possible if the video has not yet been generated
	Looping     *bool  `json:"looping"`     // OPTIONAL, Whether the image has a looping animation. Only available if the image is animated and type is 'image/gif'.
	NSFW        *bool  `json:"nsfw"`        // Indicates if the image has been marked as nsfw or not. Defaults to null if information is not available.
	Topic       string `json:"topic"`       // Topic of the gallery image.
	Section     string `json:"section"`     // If the image has been categorized by our backend then this will contain the section the image belongs in. (funny, cats, adviceanimals, wtf, etc)
	IsAlbum     *bool  `json:"is_album"`    // If it's an album or not
	// ** Unimplemented fields **
	// bandwidth	integer	Bandwidth consumed by the image in bytes
	// deletehash	string	OPTIONAL, the deletehash, if you're logged in as the image owner
	// comment_count	int	Number of comments on the gallery image.
	// topic_id	integer	Topic ID of the gallery image.
	// vote	string	The current user's vote on the album. null if not signed in or if the user hasn't voted on it.
	// favorite	boolean	Indicates if the current user favorited the image. Defaults to false if not signed in.
	// account_url	string	The username of the account that uploaded it, or null.
	// account_id	integer	The account ID of the account that uploaded it, or null.
	// ups	integer	Upvotes for the image
	// downs	integer	Number of downvotes for the image
	// points	integer	Upvotes minus downvotes
	// score	integer	Imgur popularity score
}

// Represents an Imgur gallery album
type imgurGalleryAlbum struct {
	ID          string              `json:"id"`           // The ID for the album
	Title       string              `json:"title"`        // The title of the album.
	Description string              `json:"description"`  // Description of the album.
	DateTime    int64               `json:"datetime"`     // Time inserted into the gallery, epoch time
	Views       int64               `json:"views"`        // The number of album views
	Link        string              `json:"link"`         // The URL link to the album
	NSFW        *bool               `json:"nsfw"`         // Indicates if the album has been marked as nsfw or not. Defaults to null if information is not available.
	Topic       string              `json:"topic"`        // Topic of the gallery album.
	IsAlbum     *bool               `json:"is_album"`     // If it's an album or not
	Cover       string              `json:"cover"`        // The ID of the album cover image
	CoverWidth  int                 `json:"cover_width"`  // The width, in pixels, of the album cover image
	CoverHeight int                 `json:"cover_height"` // The height, in pixels, of the album cover image
	ImagesCount int                 `json:"images_count"` // The total number of images in the album
	Images      []imgurGalleryImage `json:"images"`       // An array of all the images in the album (only available when requesting the direct album)

	// ** Unimplemented fields **
	// account_url	string	The account username or null if it's anonymous.
	// account_id	integer	The account ID of the account that uploaded it, or null.
	// privacy	string	The privacy level of the album, you can only view public if not logged in as album owner
	// layout	string	The view layout of the album.
	// views	integer	The number of image views
	// ups	integer	Upvotes for the image
	// downs	integer	Number of downvotes for the image
	// points	integer	Upvotes minus downvotes
	// score	integer	Imgur popularity score
	// vote	string	The current user's vote on the album. null if not signed in or if the user hasn't voted on it.
	// favorite	boolean	Indicates if the current user favorited the album. Defaults to false if not signed in.
	// comment_count	int	Number of comments on the gallery album.
	// topic_id	integer	Topic ID of the gallery album.
}

// Imgur gallery search response
type imgurSearchResponse struct {
	Data    []json.RawMessage `json:"data"`    // Data temporarily stored as RawMessage objects, as it can contain a mix of imgurGalleryImage and imgurGalleryAlbum objects
	Success *bool             `json:"success"` // Request completed successfully
	Status  int               `json:"status"`  // HTTP response code
}

// Service contains the Config fields for the Imgur service.
//
// Example request:
//   {
//			"client_id": "AIzaSyA4FD39..."
//			"client_secret": "ASdsaijwdfASD..."
//   }
type Service struct {
	types.DefaultService
	// The Imgur client ID
	ClientID string `json:"client_id"`
	// The API key to use when making HTTP requests to Imgur.
	ClientSecret string `json:"client_secret"`
}

// Commands supported:
//    !imgur some_search_query_without_quotes
// Responds with a suitable image into the same room as the command.
func (s *Service) Commands(client *gomatrix.Client) []types.Command {
	return []types.Command{
		types.Command{
			Path: []string{"imgur", "help"},
			Command: func(roomID, userID string, args []string) (interface{}, error) {
				return usageMessage(), nil
			},
		},
		types.Command{
			Path: []string{"imgur"},
			Command: func(roomID, userID string, args []string) (interface{}, error) {
				return s.cmdImgSearch(client, roomID, userID, args)
			},
		},
	}
}

// usageMessage returns a matrix TextMessage representation of the service usage
func usageMessage() *gomatrix.TextMessage {
	return &gomatrix.TextMessage{"m.notice",
		`Usage: !imgur image_search_text`}
}

// Search Imgur for a relevant image and upload it to matrix
func (s *Service) cmdImgSearch(client *gomatrix.Client, roomID, userID string, args []string) (interface{}, error) {
	// Check for query text
	if len(args) < 1 {
		return usageMessage(), nil
	}

	// Perform search
	querySentence := strings.Join(args, " ")
	searchResultImage, searchResultAlbum, err := s.text2img(querySentence)
	if err != nil {
		return nil, err
	}

	// Image returned
	if searchResultImage != nil {
		var imgURL = searchResultImage.Link
		if imgURL == "" {
			return gomatrix.TextMessage{
				MsgType: "m.notice",
				Body:    "No image found!",
			}, nil
		}

		// Upload image
		resUpload, err := client.UploadLink(imgURL)
		if err != nil {
			return nil, fmt.Errorf("Failed to upload Imgur image (%s) to matrix: %s", imgURL, err.Error())
		}

		// Return image message
		return gomatrix.ImageMessage{
			MsgType: "m.image",
			Body:    querySentence,
			URL:     resUpload.ContentURI,
			Info: gomatrix.ImageInfo{
				Height:   uint(searchResultImage.Height),
				Width:    uint(searchResultImage.Width),
				Mimetype: searchResultImage.Type,
			},
		}, nil
	} else if searchResultAlbum != nil {
		return gomatrix.TextMessage{
			MsgType: "m.notice",
			Body:    "Search returned an album - Not currently supported",
		}, nil
	} else {
		return gomatrix.TextMessage{
			MsgType: "m.notice",
			Body:    "No image found!",
		}, nil
	}
}

// text2img returns info about an image or an album
func (s *Service) text2img(query string) (*imgurGalleryImage, *imgurGalleryAlbum, error) {
	log.Info("Searching Imgur for an image of a ", query)
	bytes, err := queryImgur(query, s.ClientID)
	if err != nil {
		return nil, nil, err
	}

	var searchResults imgurSearchResponse
	// if err := json.NewDecoder(res.Body).Decode(&searchResults); err != nil {
	if err := json.Unmarshal(bytes, &searchResults); err != nil {
		return nil, nil, fmt.Errorf("No images found - %s", err.Error())
	} else if len(searchResults.Data) < 1 {
		return nil, nil, fmt.Errorf("No images found")
	}

	log.Printf("%d results were returned from Imgur", len(searchResults.Data))
	// Return a random image result
	var images []imgurGalleryImage
	for i := 0; i < len(searchResults.Data); i++ {
		var image imgurGalleryImage
		if err := json.Unmarshal(searchResults.Data[i], &image); err == nil && !*(image.IsAlbum) {
			images = append(images, image)
		}
	}
	if len(images) > 0 {
		var r = 0
		if len(images) > 1 {
			r = rand.Intn(len(images) - 1)
		}
		return &images[r], nil, nil
	}

	return nil, nil, fmt.Errorf("No images found")
}

// Query imgur and return HTTP response or error
func queryImgur(query, clientID string) ([]byte, error) {
	query = url.QueryEscape(query)

	// Build the query URL
	var sort = "time"  // time | viral | top
	var window = "all" // day | week | month | year | all
	var page = 1
	var urlString = fmt.Sprintf("https://api.imgur.com/3/gallery/search/%s/%s/%d?q=%s", sort, window, page, query)

	u, err := url.Parse(urlString)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("GET", u.String(), nil)
	if err != nil {
		return nil, err
	}

	// Add authorisation header
	req.Header.Add("Authorization", "Client-ID "+clientID)
	res, err := httpClient.Do(req)
	if res != nil {
		defer res.Body.Close()
	}
	if err != nil {
		return nil, err
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, fmt.Errorf("Request error: %d, %s", res.StatusCode, response2String(res))
	}

	// Read and return response body
	bytes, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	return bytes, nil
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
