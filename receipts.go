package lidl

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"math"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const ticketsAPI = "https://tickets.lidlplus.com/api"

// Amount is a sum of money in cents.
type Amount int64

// UnmarshalJSON reads a number (2.19) or a string ("2,19", "1.234,56").
func (a *Amount) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `" `)
	if s == "" || s == "null" {
		return nil
	}
	f, err := parseNumber(s)
	if err != nil {
		return fmt.Errorf("lidl: bad amount %s", b)
	}
	*a = Amount(math.Round(f * 100))
	return nil
}

// MarshalJSON writes the amount as a decimal number (2.19).
func (a Amount) MarshalJSON() ([]byte, error) {
	return []byte(a.String()), nil
}

func (a Amount) String() string {
	sign := ""
	if a < 0 {
		sign, a = "-", -a
	}
	return fmt.Sprintf("%s%d.%02d", sign, a/100, a%100)
}

// parseNumber reads a number with a dot or a comma as the decimal mark.
func parseNumber(s string) (float64, error) {
	dot, comma := strings.LastIndex(s, "."), strings.LastIndex(s, ",")
	switch {
	case dot >= 0 && comma > dot:
		s = strings.ReplaceAll(s, ".", "")
		s = strings.Replace(s, ",", ".", 1)
	case comma >= 0 && dot > comma:
		s = strings.ReplaceAll(s, ",", "")
	case comma >= 0:
		s = strings.Replace(s, ",", ".", 1)
	}
	return strconv.ParseFloat(s, 64)
}

func parseAmount(s string) Amount {
	f, _ := parseNumber(s)
	return Amount(math.Round(f * 100))
}

// ReceiptSummary is one entry of the receipt list.
type ReceiptSummary struct {
	ID string `json:"id"`
	// Date is the time on the receipt, in the local time of the store.
	Date          string `json:"date"`
	TotalAmount   Amount `json:"totalAmount"`
	StoreCode     string `json:"storeCode"`
	ArticlesCount int    `json:"articlesCount"`
	Currency      struct {
		Code   string `json:"code"`
		Symbol string `json:"symbol"`
	} `json:"currency"`

	// Raw is the entry exactly as Lidl sent it.
	Raw json.RawMessage `json:"-"`
}

// Receipts returns receipt summaries, newest first. A limit of 0 returns all.
func (c *Client) Receipts(ctx context.Context, limit int) ([]ReceiptSummary, error) {
	var all []ReceiptSummary
	for page := 1; ; page++ {
		var res struct {
			Tickets    []json.RawMessage `json:"tickets"`
			TotalCount int               `json:"totalCount"`
		}
		u := fmt.Sprintf("%s/v2/%s/tickets?pageNumber=%d&onlyFavorite=false", ticketsAPI, c.Country, page)
		if err := c.get(ctx, u, &res); err != nil {
			return nil, err
		}
		for _, raw := range res.Tickets {
			s := ReceiptSummary{Raw: raw}
			if err := json.Unmarshal(raw, &s); err != nil {
				return nil, fmt.Errorf("lidl: bad receipt summary: %w", err)
			}
			all = append(all, s)
			if limit > 0 && len(all) == limit {
				return all, nil
			}
		}
		if len(res.Tickets) == 0 || len(all) >= res.TotalCount {
			return all, nil
		}
	}
}

// Receipt is one full receipt.
type Receipt struct {
	ID string `json:"id"`
	// Date is the time on the receipt, in the local time of the store.
	Date        string `json:"date"`
	TotalAmount Amount `json:"totalAmount"`
	Store       Store  `json:"store"`
	// Payment is the payment method as printed, for example "Karte".
	Payment string `json:"payment,omitempty"`
	Items   []Item `json:"items"`

	// HTML is the printed receipt that the items are read from.
	HTML string `json:"-"`
	// Raw is the receipt exactly as Lidl sent it.
	Raw json.RawMessage `json:"-"`
}

// Store is the shop a receipt is from.
type Store struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Address    string `json:"address"`
	PostalCode string `json:"postalCode"`
	Locality   string `json:"locality"`
}

// Item is one article on a receipt.
type Item struct {
	ArticleID string  `json:"articleId,omitempty"`
	Name      string  `json:"name"`
	Quantity  float64 `json:"quantity"`
	UnitPrice Amount  `json:"unitPrice"`
	// Amount is the line price before discounts.
	Amount    Amount     `json:"amount"`
	TaxType   string     `json:"taxType,omitempty"`
	ByWeight  bool       `json:"byWeight,omitempty"`
	Discounts []Discount `json:"discounts,omitempty"`
}

// Discount is a price reduction on one item. The amount is negative.
type Discount struct {
	Description string `json:"description"`
	Amount      Amount `json:"amount"`
}

// Receipt returns one full receipt with its items.
func (c *Client) Receipt(ctx context.Context, id string) (*Receipt, error) {
	var raw json.RawMessage
	var err error
	for _, version := range []string{"v3", "v2"} {
		u := fmt.Sprintf("%s/%s/%s/tickets/%s", ticketsAPI, version, c.Country, url.PathEscape(id))
		var apiErr *APIError
		if err = c.get(ctx, u, &raw); err == nil || !errors.As(err, &apiErr) {
			break
		}
	}
	if err != nil {
		return nil, err
	}

	var body struct {
		Receipt
		HTML string `json:"htmlPrintedReceipt"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, fmt.Errorf("lidl: bad receipt: %w", err)
	}
	r := body.Receipt
	r.HTML, r.Raw = body.HTML, raw
	r.Items = parseItems(r.HTML)
	if m := tenderAttr.FindStringSubmatch(r.HTML); m != nil {
		r.Payment = html.UnescapeString(m[1])
	}
	return &r, nil
}

var (
	itemSpan       = regexp.MustCompile(`<span id="purchase_list_line_(\d+)"([^>]*)>([^<]*)</span>`)
	spanAttr       = regexp.MustCompile(`([a-z-]+)="([^"]*)"`)
	trailingAmount = regexp.MustCompile(`(-?\d[\d.,]*[.,]\d{2})\s*([A-Z])?\s*$`)
	tenderAttr     = regexp.MustCompile(`data-tender-description="([^"]*)"`)
)

type receiptLine struct {
	text  string
	class string
	attrs map[string]string
}

// parseItems reads the articles from the printed receipt HTML.
func parseItems(doc string) []Item {
	lines := map[int]*receiptLine{}
	for _, m := range itemSpan.FindAllStringSubmatch(doc, -1) {
		n, _ := strconv.Atoi(m[1])
		line := lines[n]
		if line == nil {
			line = &receiptLine{attrs: map[string]string{}}
			lines[n] = line
		}
		line.text += html.UnescapeString(m[3])
		for _, a := range spanAttr.FindAllStringSubmatch(m[2], -1) {
			if a[1] == "class" {
				line.class += " " + a[2]
			} else {
				line.attrs[a[1]] = html.UnescapeString(a[2])
			}
		}
	}
	numbers := make([]int, 0, len(lines))
	for n := range lines {
		numbers = append(numbers, n)
	}
	sort.Ints(numbers)

	var items []Item
	current := -1
	for _, n := range numbers {
		line := lines[n]
		classes := strings.Fields(strings.ReplaceAll(line.class, "css_bold", ""))
		if hasClass(classes, "currency") {
			continue
		}
		amount := trailingAmount.FindStringSubmatchIndex(line.text)
		articleID := line.attrs["data-art-id"]

		switch {
		case articleID != "" && amount == nil:
			if current >= 0 && strings.Contains(line.text, "kg") {
				items[current].ByWeight = true
			}
		case articleID != "":
			item := Item{
				ArticleID: articleID,
				Name:      line.attrs["data-art-description"],
				Quantity:  1,
				UnitPrice: parseAmount(line.attrs["data-unit-price"]),
				Amount:    parseAmount(line.text[amount[2]:amount[3]]),
				TaxType:   line.attrs["data-tax-type"],
			}
			if q, err := parseNumber(line.attrs["data-art-quantity"]); err == nil {
				item.Quantity = q
			}
			items = append(items, item)
			current = len(items) - 1
		case amount == nil:
		case current >= 0 && (len(classes) == 0 || hasClass(classes, "discount") || line.attrs["data-promotion-id"] != ""):
			items[current].Discounts = append(items[current].Discounts, Discount{
				Description: strings.TrimSpace(line.text[:amount[0]]),
				Amount:      parseAmount(line.text[amount[2]:amount[3]]),
			})
		default:
			price := parseAmount(line.text[amount[2]:amount[3]])
			items = append(items, Item{
				Name:      strings.TrimSpace(line.text[:amount[0]]),
				Quantity:  1,
				UnitPrice: price,
				Amount:    price,
			})
			current = -1
		}
	}
	return items
}

func hasClass(classes []string, name string) bool {
	for _, c := range classes {
		if c == name {
			return true
		}
	}
	return false
}
