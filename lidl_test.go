package lidl

import (
	"encoding/json"
	"testing"
)

func TestAmount(t *testing.T) {
	cases := map[string]Amount{
		`2.19`:       219,
		`"2,19"`:     219,
		`"1.234,56"`: 123456,
		`"1,234.56"`: 123456,
		`-0.21`:      -21,
		`12`:         1200,
		`null`:       0,
	}
	for in, want := range cases {
		var got Amount
		if err := json.Unmarshal([]byte(in), &got); err != nil {
			t.Errorf("%s: %v", in, err)
		} else if got != want {
			t.Errorf("%s: got %d, want %d", in, got, want)
		}
	}
	if s := Amount(-5).String(); s != "-0.05" {
		t.Errorf("String: got %s", s)
	}
}

func TestParseCallback(t *testing.T) {
	cases := map[string]string{
		"com.lidlplus.app://callback?code=ABC123&scope=openid": "ABC123",
		"  'com.lidlplus.app://callback?state=x&code=A%2DB'  ": "A-B",
		"ABC123": "ABC123",
	}
	for in, want := range cases {
		got, err := ParseCallback(in)
		if err != nil || got != want {
			t.Errorf("%q: got %q, %v; want %q", in, got, err, want)
		}
	}
	if _, err := ParseCallback("not a code!"); err == nil {
		t.Error("expected an error for bad input")
	}
}

func TestParseItems(t *testing.T) {
	span := func(n int, attrs, text string) string {
		return `<span id="purchase_list_line_` + string(rune('0'+n)) + `" ` + attrs + `>` + text + `</span>`
	}
	doc := `<span class="purchase_list">` +
		span(1, `class="currency css_bold" data-currency="€"`, "EUR") +
		span(2, `class="article css_bold" data-art-id="0000001" data-unit-price="1,50" data-tax-type="A" data-art-description="Bread &amp; Rolls"`, "Bread &amp; Rolls") +
		span(2, `class="article" data-art-id="0000001" data-unit-price="1,50" data-tax-type="A" data-art-description="Bread &amp; Rolls"`, "      1,50 x   2    3,00 A") +
		span(3, `class="discount"`, "  Coupon             -0,50") +
		span(4, `class="article" data-art-id="0000002" data-art-quantity="0,500" data-unit-price="2,00" data-tax-type="B" data-art-description="Apples"`, "Apples      1,00 B") +
		span(5, `class="article" data-art-id="0000002" data-art-quantity="0,500" data-unit-price="2,00" data-tax-type="B" data-art-description="Apples"`, "  0,500 kg x 2,00   EUR/kg") +
		`</span>`

	items := parseItems(doc)
	if len(items) != 2 {
		t.Fatalf("got %d items, want 2: %+v", len(items), items)
	}
	bread, apples := items[0], items[1]
	if bread.Name != "Bread & Rolls" || bread.Amount != 300 || bread.UnitPrice != 150 || bread.Quantity != 1 || bread.TaxType != "A" {
		t.Errorf("bread: %+v", bread)
	}
	if len(bread.Discounts) != 1 || bread.Discounts[0].Amount != -50 || bread.Discounts[0].Description != "Coupon" {
		t.Errorf("bread discounts: %+v", bread.Discounts)
	}
	if apples.Quantity != 0.5 || apples.Amount != 100 || !apples.ByWeight {
		t.Errorf("apples: %+v", apples)
	}
}
