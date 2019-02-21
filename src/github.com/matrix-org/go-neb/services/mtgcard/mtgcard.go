// Package mtgcard implements a Service which adds !commands and mentions for scryfall api.
package mtgcard

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	log "github.com/Sirupsen/logrus"
	"github.com/matrix-org/go-neb/types"
	"github.com/matrix-org/gomatrix"
)

// ServiceType of the Mtgcard service.
const ServiceType = "mtgcard"

// Matches [[ then word separated with spaces then ]]. E.g "[[card Name]]
var cardRegex = regexp.MustCompile(`\[\[[\p{L}|\p{Po}+|\s*]+\]\]`)

type scryfallSearch struct {
	Name         string `json:"name"`
	ScryfallURI  string `json:"scryfall_uri"`
	Euro         string `json:"eur"`
	PurchaseURIs struct {
		Cardmarket string `json:cardmarket`
	} `json:purchase_uris`
	ImageURIs struct {
		Normal string `json:"normal"`
		Small  string `json:"small"`
	} `json:"image_uris"`
}

// Service contains the Config fields for the Mtgcard Service.
type Service struct {
	types.DefaultService
}

// Commands supported:
//   !mtgcard some search query without quotes
// Responds with card information into the same room as the command.
func (s *Service) Commands(client *gomatrix.Client) []types.Command {
	return []types.Command{
		types.Command{
			Path: []string{"mtgcard"},
			Command: func(roomID, userID string, args []string) (interface{}, error) {
				return s.cmdScryfall(client, roomID, userID, args)
			},
		},
	}
}

// Expansions expands card mentions represented as:
//    [[ possible name of card ]]
// and responds with card information into the same room as the expansion
func (s *Service) Expansions(cli *gomatrix.Client) []types.Expansion {
	return []types.Expansion{
		types.Expansion{
			Regexp: cardRegex,
			Expand: func(roomID, userID string, cardMentions []string) interface{} {
				return s.expandCard(roomID, userID, cardMentions)
			},
		},
	}
}

// cmdScryfall processes a command by calling the query and returning a message with card info
func (s *Service) cmdScryfall(client *gomatrix.Client, roomID, userID string, args []string) (interface{}, error) {
	// only 1 arg which is the text to search for.
	query := strings.Join(args, " ")

	scryfallResult, err := s.searchScryfall(query)
	if err != nil {
		return nil, err
	}
	if scryfallResult != nil {
		return createMessage(scryfallResult), err
	}
	return nil, err

}

// expandCard processes an expansion by calling the query and returning a message with card info
func (s *Service) expandCard(roomID, userID string, cardMentions []string) interface{} {
	// cardMentions => ["[[magic card]]"]

	logger := log.WithField("cardMentions", cardMentions)
	logger.WithFields(log.Fields{
		"room_id": roomID,
		"user_id": userID,
	}).Print("Expanding card mention")

	cardMentions[0] = strings.TrimPrefix(cardMentions[0], "[[")
	cardMentions[0] = strings.TrimSuffix(cardMentions[0], "]]")
	scryfallResult, err := s.searchScryfall(cardMentions[0])
	if err != nil {
		return err
	}
	if scryfallResult != nil {
		return createMessage(scryfallResult)
	}
	return nil
}

// createMessage returns a nicely formatted matrix.HTMLMessage from a query result
func createMessage(result *scryfallSearch) gomatrix.HTMLMessage {

	var htmlBuffer bytes.Buffer
	var plainBuffer bytes.Buffer
	message := fmt.Sprintf(
		"<ul><li><a href=%s>%s</a>\t<a href=%s>(SF)</a>\t<a href=%s>(%s&euro;)</a></li></ul>",
		result.ImageURIs.Normal,
		html.EscapeString(result.Name),
		result.ScryfallURI,
		result.PurchaseURIs.Cardmarket,
		result.Euro,
	)
	htmlBuffer.WriteString(message)
	plainBuffer.WriteString(fmt.Sprintf("$s, $s, $s", result.Name, result.ScryfallURI, result.Euro))

	return gomatrix.HTMLMessage{
		Body:          plainBuffer.String(),
		MsgType:       "m.notice",
		Format:        "org.matrix.custom.html",
		FormattedBody: htmlBuffer.String(),
	}
}

// searchScryfall queries Scryfall API for card info
func (s *Service) searchScryfall(query string) (*scryfallSearch, error) {
	log.Info("Searching scryfall for ", query)
	u, err := url.Parse("https://api.scryfall.com/cards/named")
	if err != nil {
		return nil, err
	}
	q := u.Query()
	q.Set("fuzzy", query)
	u.RawQuery = q.Encode()
	res, err := http.Get(u.String())
	if res != nil {
		defer res.Body.Close()
	}
	if err != nil {
		log.Error("Problem searching scryfall: ", err)
		return nil, err
	}
	if res.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	var search scryfallSearch
	if err := json.NewDecoder(res.Body).Decode(&search); err != nil {
		return nil, err
	}
	log.Info("Search scryfall returned ", search)
	return &search, nil
}

func init() {
	types.RegisterService(func(serviceID, serviceUserID, webhookEndpointURL string) types.Service {
		return &Service{
			DefaultService: types.NewDefaultService(serviceID, serviceUserID, ServiceType),
		}
	})
}
