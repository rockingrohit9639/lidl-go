package lidl

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

const couponsAPI = "https://coupons.lidlplus.com/app/api"

// Coupon is one Lidl Plus coupon.
type Coupon struct {
	ID string `json:"id"`
	// Section is the group Lidl lists the coupon in:
	// "AllStores", "OtherStores" or "OnlineShop".
	Section string `json:"section"`
	Title   string `json:"title"`
	// Discount is the saving as printed, for example "-15%".
	Discount string `json:"discount"`
	// Condition is the small print next to the discount.
	Condition string    `json:"condition,omitempty"`
	Start     time.Time `json:"start"`
	End       time.Time `json:"end"`
	Activated bool      `json:"activated"`

	// Raw is the coupon exactly as Lidl sent it.
	Raw json.RawMessage `json:"-"`
}

// Coupons returns all coupons of the account.
func (c *Client) Coupons(ctx context.Context) ([]Coupon, error) {
	var res struct {
		Sections []struct {
			Name       string            `json:"name"`
			Promotions []json.RawMessage `json:"promotions"`
		} `json:"sections"`
	}
	if err := c.get(ctx, couponsAPI+"/v2/promotionslist", &res); err != nil {
		return nil, err
	}

	var all []Coupon
	for _, section := range res.Sections {
		for _, raw := range section.Promotions {
			var p struct {
				ID       string `json:"id"`
				Title    string `json:"title"`
				Discount struct {
					Title       string `json:"title"`
					Description string `json:"description"`
				} `json:"discount"`
				Validity struct {
					Start time.Time `json:"start"`
					End   time.Time `json:"end"`
				} `json:"validity"`
				IsActivated bool `json:"isActivated"`
			}
			if err := json.Unmarshal(raw, &p); err != nil {
				return nil, fmt.Errorf("lidl: bad coupon: %w", err)
			}
			all = append(all, Coupon{
				ID:        p.ID,
				Section:   section.Name,
				Title:     p.Title,
				Discount:  p.Discount.Title,
				Condition: p.Discount.Description,
				Start:     p.Validity.Start,
				End:       p.Validity.End,
				Activated: p.IsActivated,
				Raw:       raw,
			})
		}
	}
	return all, nil
}

// ActivateCoupon switches a coupon on, so it applies at the checkout.
func (c *Client) ActivateCoupon(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodPost, couponsAPI+"/v1/promotions/"+url.PathEscape(id)+"/activation", nil)
}

// DeactivateCoupon switches a coupon off again.
func (c *Client) DeactivateCoupon(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, couponsAPI+"/v1/promotions/"+url.PathEscape(id)+"/activation", nil)
}
