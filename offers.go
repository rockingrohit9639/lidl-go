package lidl

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"
)

const offersAPI = "https://offers.lidlplus.com/app/api"

// Offer is one weekly offer of a store.
type Offer struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Brand string `json:"brand,omitempty"`
	// Type is Lidl's name for the kind of offer, for example
	// "StoreSpecialPriceDiscount" or "StoreXforYDiscount".
	Type string `json:"type"`
	// Discount is the saving as printed, for example "-25%" or "2+1".
	Discount string `json:"discount,omitempty"`
	// Price is the offer price. It is zero when the offer has no fixed price.
	Price Amount `json:"price,omitempty"`
	// OldPrice is the crossed-out price. It is zero when Lidl shows none.
	OldPrice Amount `json:"oldPrice,omitempty"`
	// Packaging is the small print: size, limits and the normal price.
	Packaging    string `json:"packaging,omitempty"`
	PricePerUnit string `json:"pricePerUnit,omitempty"`
	// ArticleIDs are the articles the offer covers. They match Item.ArticleID.
	ArticleIDs []string  `json:"articleIds,omitempty"`
	Start      time.Time `json:"start"`
	End        time.Time `json:"end"`

	// Raw is the offer exactly as Lidl sent it.
	Raw json.RawMessage `json:"-"`
}

// Offers returns the current offers of a store. The store ID is the one on
// a receipt, for example "DE5092".
func (c *Client) Offers(ctx context.Context, storeID string) ([]Offer, error) {
	var res struct {
		Offers []json.RawMessage `json:"offers"`
	}
	u := fmt.Sprintf("%s/v4/%s/%s/offers", offersAPI, c.Country, url.PathEscape(storeID))
	if err := c.get(ctx, u, &res); err != nil {
		return nil, err
	}

	all := make([]Offer, 0, len(res.Offers))
	for _, raw := range res.Offers {
		var o struct {
			Offer
			Type     string `json:"offerType"`
			PriceBox struct {
				Message   string `json:"discountMessage"`
				Price     Amount `json:"largePartNumeric"`
				PriceText string `json:"largePartString"`
				OldPrice  Amount `json:"smallPartNumeric"`
			} `json:"priceBox"`
			ArticleIDs []string  `json:"productIds"`
			Start      time.Time `json:"startValidityDateUTC"`
			End        time.Time `json:"endValidityDateUTC"`
		}
		if err := json.Unmarshal(raw, &o); err != nil {
			return nil, fmt.Errorf("lidl: bad offer: %w", err)
		}
		offer := o.Offer
		offer.Type = o.Type
		offer.Discount = strings.TrimSpace(o.PriceBox.Message + " " + o.PriceBox.PriceText)
		offer.Price, offer.OldPrice = o.PriceBox.Price, o.PriceBox.OldPrice
		offer.ArticleIDs, offer.Start, offer.End = o.ArticleIDs, o.Start, o.End
		offer.Raw = raw
		all = append(all, offer)
	}
	return all, nil
}
