package fixr

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/pkg/errors"
)

const (
	homeURL    = "https://fixr.co"
	bookingURL = "https://api.fixr-app.com/api/v2/app/booking"
	promoURL   = "https://api.fixr-app.com/api/v2/app/promo_code/%d/%s"
	loginURL   = "https://api.fixr-app.com/api/v2/app/user/authenticate/with-email"
	eventURL   = "https://api.fixr-app.com/api/v2/app/event/%d"
	cardURL    = "https://api.stripe.com/v1/tokens"
	tokenURL   = "https://api.fixr-app.com/api/v2/app/stripe"
	meURL      = "https://api.fixr-app.com/api/v2/app/user/me"
)

var (
	// FixrVersion represents FIXR's web API version.
	FixrVersion = "1.34.0"

	// FixrPlatformVer represents the "platform" used to "browse" the site.
	FixrPlatformVer = "Chrome/51.0.2704.103"

	// UserAgent is the user agent passed to every API call.
	UserAgent = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/51.0.2704.103 Safari/537.36"
)

type payload map[string]interface{}

type responseParams interface {
	error() error
	clearError()
}

type apiError struct {
	Error string `json:"message"`
}

func (a *apiError) error() error {
	if len(a.Error) > 0 {
		return errors.New(a.Error)
	}
	return nil
}

func (a *apiError) clearError() {
	a.Error = ""
}

// Client provides access to the FIXR API methods.
type Client struct {
	apiError
	Email      string
	FirstName  string      `json:"first_name"`
	LastName   string      `json:"last_name"`
	MagicURL   string      `json:"magic_login_url"`
	AuthToken  string      `json:"auth_token"`
	StripeUser *stripeUser `json:"stripe_user"`
	httpClient *http.Client
}

// Event contains the event details for given event ID.
type Event struct {
	ID      int      `json:"id"`
	Name    string   `json:"name"`
	Tickets []Ticket `json:"tickets"`
	Error   string   `json:"detail"`
}

func (e *Event) error() error {
	if len(e.Error) > 0 {
		return errors.New(e.Error)
	}
	return nil
}

func (e *Event) clearError() {
	e.Error = ""
}

// Ticket contains all the information pertaining to a specific ticket for an event.
type Ticket struct {
	ID         int     `json:"id"`
	Name       string  `json:"name"`
	Type       int     `json:"type"`
	Currency   string  `json:"currency"`
	Price      float64 `json:"price"`
	BookingFee float64 `json:"booking_fee"`
	Max        int     `json:"max_per_user"`
	SoldOut    bool    `json:"sold_out"`
	Expired    bool    `json:"expired"`
	Invalid    bool    `json:"not_yet_valid"`
}

// PromoCode contains the details of a specific promotional code.
type PromoCode struct {
	apiError
	Code       string  `json:"code"`
	Price      float64 `json:"price"`
	BookingFee float64 `json:"booking_fee"`
	Currency   string  `json:"currency"`
	Max        int     `json:"max_per_user"`
	Remaining  int     `json:"remaining"`
}

// Booking contains the resultant booking information.
type Booking struct {
	apiError
	Event Event  `json:"event"`
	Name  string `json:"user_full_name"`
	PDF   string `json:"pdf"`
	State int    `json:"state"`
}

// NewClient returns a FIXR client with the given email and password.
func NewClient(email string) *Client {
	return &Client{Email: email, httpClient: new(http.Client)}
}

func (c *Client) get(addr string, auth bool, obj responseParams) error {
	req, err := http.NewRequest("GET", addr, nil)
	if err != nil {
		return errors.New("error creating GET request")
	}
	return c.req(req, auth, obj)
}

func (c *Client) post(addr string, data *bytes.Buffer, auth bool, obj responseParams) error {
	req, err := http.NewRequest("POST", addr, data)
	if err != nil {
		return errors.New("error creating POST request")
	}
	return c.req(req, auth, obj)
}

func decodeJSONResponse(body io.ReadCloser, obj responseParams) error {
	if err := json.NewDecoder(body).Decode(obj); err != nil {
		return errors.Wrap(err, "JSON decoding failed")
	}
	defer obj.clearError()
	if err := obj.error(); err != nil {
		return err
	}
	return nil
}

func (c *Client) req(req *http.Request, auth bool, obj responseParams) error {
	req.Header.Set("User-Agent", UserAgent)
	if auth {
		req.Header.Set("Authorization", fmt.Sprintf("Token %s", c.AuthToken))
	}
	if req.URL.String() == cardURL {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	} else {
		req.Header.Set("Content-Type", "application/json")
		// The following circumvents canonical formatting
		req.Header["FIXR-Platform"] = []string{"web"}
		req.Header["FIXR-Platform-Version"] = []string{FixrPlatformVer}
		req.Header["FIXR-App-Version"] = []string{FixrVersion}
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return errors.Wrap(err, "error executing request")
	}
	defer resp.Body.Close()
	return decodeJSONResponse(resp.Body, obj)
}

// Logon authenticates the client with FIXR and returns an error if encountered.
func (c *Client) Logon(pass string) error {
	pl := payload{
		"email":    c.Email,
		"password": pass,
	}
	data, err := jsonifyPayload(pl)
	if err != nil {
		return err
	}
	if err := c.post(loginURL, data, false, c); err != nil {
		return errors.Wrap(err, "error logging on")
	}
	return nil
}

// Event returns the event information for a given event ID (integer).
// An error will be returned if one is encountered.
func (c *Client) Event(id int) (*Event, error) {
	event := Event{}
	if err := c.get(fmt.Sprintf(eventURL, id), false, &event); err != nil {
		return nil, errors.Wrap(err, "error getting event")
	}
	return &event, nil
}

// Promo checks for the existence of a promotion code for a given ticket ID.
// The returned *PromoCode can subsequently be passed to Book().
// An error will be returned if one is encountered.
func (c *Client) Promo(ticketID int, code string) (*PromoCode, error) {
	promo := PromoCode{}
	if err := c.get(fmt.Sprintf(promoURL, ticketID, code), true, &promo); err != nil {
		return nil, errors.Wrap(err, "error getting promo code")
	}
	return &promo, nil
}

// Book books a ticket, given a *Ticket and an amout (with the option of a promo code).
// The booking details and an error, if encountered, will be returned.
func (c *Client) Book(ticket *Ticket, amount int, promo *PromoCode) (*Booking, error) {
	fmt.Println(ticket)
	booking := Booking{}
	pl := payload{
		"ticket_id": ticket.ID,
		"amount":    amount,
	}
	/* ticket.Invalid can change upon ticket release (i.e. is time dependent),
	it should therefore be checked with an API call. */
	for t, msg := range map[bool]string{
		ticket.SoldOut: "ticket selection has sold out",
		ticket.Expired: "ticket selection has expired"} {
		if t {
			return nil, errors.New(msg)
		}
	}
	if amount > ticket.Max {
		return nil, fmt.Errorf("cannot purchase more than the maximum (%d)", ticket.Max)
	}
	if ticket.BookingFee+ticket.Price > 0 {
		pl["purchase_key"] = genKey()
	}
	if promo != nil {
		pl["promo_code"] = promo.Code
	}
	data, err := jsonifyPayload(pl)
	if err != nil {
		return nil, err
	}
	if err := c.post(bookingURL, data, true, &booking); err != nil {
		return nil, errors.Wrap(err, "error booking ticket")
	}
	return &booking, nil
}
