// Command lidl reads data from a Lidl Plus account.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	lidl "github.com/rockingrohit9639/lidl-go"
)

const usage = `lidl - unofficial Lidl Plus client

Usage:
  lidl login [-country DE] [-language de] [-no-open]   start a login
  lidl login -code <callback URL or code>              finish a login
  lidl receipts [-limit N] [-json]                     list receipts
  lidl receipt [-json|-raw] <id>                       show one receipt with its items
  lidl coupons [-all] [-json]                          list coupons
  lidl activate <id>... | -all                         activate coupons
  lidl deactivate <id>...                              deactivate coupons
  lidl offers [-store DE5092] [-json]                  list this week's offers of a store
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	ctx := context.Background()
	var err error
	switch os.Args[1] {
	case "login":
		err = login(ctx, os.Args[2:])
	case "receipts":
		err = receipts(ctx, os.Args[2:])
	case "receipt":
		err = receipt(ctx, os.Args[2:])
	case "coupons":
		err = coupons(ctx, os.Args[2:])
	case "activate":
		err = activate(ctx, os.Args[2:])
	case "deactivate":
		err = deactivate(ctx, os.Args[2:])
	case "offers":
		err = offers(ctx, os.Args[2:])
	case "help", "-h", "--help":
		fmt.Print(usage)
	default:
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	if errors.Is(err, lidl.ErrNotLoggedIn) {
		fmt.Fprintln(os.Stderr, "Not logged in. Run `lidl login` first.")
		os.Exit(1)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

type config struct {
	Country      string `json:"country"`
	Language     string `json:"language"`
	Store        string `json:"store,omitempty"`
	RefreshToken string `json:"refresh_token,omitempty"`
	Verifier     string `json:"pending_verifier,omitempty"`
}

func configPath() (string, error) {
	if p := os.Getenv("LIDL_GO_CONFIG"); p != "" {
		return p, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "lidl-go", "config.json"), nil
}

func loadConfig() (config, error) {
	cfg := config{Country: "DE", Language: "de"}
	path, err := configPath()
	if err != nil {
		return cfg, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return cfg, err
	}
	return cfg, json.Unmarshal(data, &cfg)
}

func saveConfig(cfg config) error {
	path, err := configPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func newClient() (*lidl.Client, error) {
	cfg, err := loadConfig()
	if err != nil {
		return nil, err
	}
	client := lidl.NewClient(cfg.Country, cfg.Language, cfg.RefreshToken)
	client.OnRefresh = func(tok lidl.Token) error {
		cfg.RefreshToken = tok.RefreshToken
		return saveConfig(cfg)
	}
	return client, nil
}

func login(ctx context.Context, args []string) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	fs := flag.NewFlagSet("login", flag.ExitOnError)
	country := fs.String("country", cfg.Country, "country code (DE, AT, NL, ...)")
	language := fs.String("language", cfg.Language, "language code (de, en, ...)")
	code := fs.String("code", "", "callback URL or code from a started login")
	noOpen := fs.Bool("no-open", false, "do not open the browser")
	fs.Parse(args)

	pasted := *code
	if pasted == "" {
		cfg.Country, cfg.Language = strings.ToUpper(*country), strings.ToLower(*language)
		if cfg.Verifier, err = lidl.NewVerifier(); err != nil {
			return err
		}
		if err := saveConfig(cfg); err != nil {
			return err
		}
		link := lidl.AuthorizeURL(cfg.Verifier, cfg.Country, cfg.Language)
		fmt.Printf(`Open this page and log in:

  %s

Before you log in, open the browser DevTools, go to the Network tab and
tick "Preserve log". After the login, the last request is a link that starts
with com.lidlplus.app://callback - copy it.

Paste the link here (or press Enter and run "lidl login -code <link>" later): `, link)
		if !*noOpen {
			openBrowser(link)
		}
		line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		if pasted = strings.TrimSpace(line); pasted == "" {
			fmt.Println()
			return nil
		}
	}

	if cfg.Verifier == "" {
		return errors.New("no login in progress; run `lidl login` first")
	}
	authCode, err := lidl.ParseCallback(pasted)
	if err != nil {
		return err
	}
	tok, err := lidl.ExchangeCode(ctx, nil, authCode, cfg.Verifier)
	if err != nil {
		return err
	}
	cfg.RefreshToken, cfg.Verifier = tok.RefreshToken, ""
	if err := saveConfig(cfg); err != nil {
		return err
	}
	fmt.Println("Logged in.")
	return nil
}

func openBrowser(link string) {
	switch runtime.GOOS {
	case "darwin":
		exec.Command("open", link).Start()
	case "windows":
		exec.Command("rundll32", "url.dll,FileProtocolHandler", link).Start()
	default:
		exec.Command("xdg-open", link).Start()
	}
}

func receipts(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("receipts", flag.ExitOnError)
	limit := fs.Int("limit", 20, "how many receipts to list, 0 for all")
	asJSON := fs.Bool("json", false, "print the list as Lidl sent it")
	fs.Parse(args)

	client, err := newClient()
	if err != nil {
		return err
	}
	list, err := client.Receipts(ctx, *limit)
	if err != nil {
		return err
	}
	if *asJSON {
		raw := make([]json.RawMessage, len(list))
		for i, r := range list {
			raw[i] = r.Raw
		}
		return printJSON(raw)
	}
	for _, r := range list {
		fmt.Printf("%s  %8s %s  %2d items  %s\n", wallTime(r.Date), r.TotalAmount, r.Currency.Code, r.ArticlesCount, r.ID)
	}
	return nil
}

func receipt(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("receipt", flag.ExitOnError)
	asJSON := fs.Bool("json", false, "print the receipt as JSON")
	asRaw := fs.Bool("raw", false, "print the receipt as Lidl sent it")
	fs.Parse(args)
	if fs.NArg() != 1 {
		return errors.New("usage: lidl receipt [-json|-raw] <id>")
	}

	client, err := newClient()
	if err != nil {
		return err
	}
	r, err := client.Receipt(ctx, fs.Arg(0))
	if err != nil {
		return err
	}
	if *asRaw {
		return printJSON(r.Raw)
	}
	if *asJSON {
		return printJSON(r)
	}

	fmt.Printf("%s  %s, %s %s\n\n", wallTime(r.Date), r.Store.Address, r.Store.PostalCode, r.Store.Locality)
	for _, item := range r.Items {
		quantity := ""
		if item.Quantity != 1 {
			quantity = fmt.Sprintf("%g x %s", item.Quantity, item.UnitPrice)
		}
		fmt.Printf("%-24s %14s %8s\n", item.Name, quantity, item.Amount)
		for _, d := range item.Discounts {
			fmt.Printf("  %-22s %14s %8s\n", d.Description, "", d.Amount)
		}
	}
	fmt.Printf("\n%-24s %14s %8s\n", "Total", r.Payment, r.TotalAmount)
	return nil
}

func coupons(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("coupons", flag.ExitOnError)
	all := fs.Bool("all", false, "also list coupons for single other stores and the online shop")
	asJSON := fs.Bool("json", false, "print the coupons as JSON")
	fs.Parse(args)

	client, err := newClient()
	if err != nil {
		return err
	}
	list, err := client.Coupons(ctx)
	if err != nil {
		return err
	}
	if !*all {
		kept := list[:0]
		for _, c := range list {
			if c.Section == "AllStores" {
				kept = append(kept, c)
			}
		}
		list = kept
	}
	if *asJSON {
		return printJSON(list)
	}
	for _, c := range list {
		active := " "
		if c.Activated {
			active = "*"
		}
		fmt.Printf("%s %6s  %-40s until %s  %s\n", active, c.Discount, c.Title, c.End.Local().Format("2006-01-02"), c.ID)
	}
	return nil
}

func activate(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("activate", flag.ExitOnError)
	all := fs.Bool("all", false, "activate every valid coupon that works in every store")
	fs.Parse(args)
	if *all == (fs.NArg() > 0) {
		return errors.New("usage: lidl activate <id>... | -all")
	}

	client, err := newClient()
	if err != nil {
		return err
	}
	if !*all {
		for _, id := range fs.Args() {
			if err := client.ActivateCoupon(ctx, id); err != nil {
				return err
			}
			fmt.Println("Activated", id)
		}
		return nil
	}

	list, err := client.Coupons(ctx)
	if err != nil {
		return err
	}
	now, count := time.Now(), 0
	for _, c := range list {
		if c.Section != "AllStores" || c.Activated || now.Before(c.Start) || now.After(c.End) {
			continue
		}
		if err := client.ActivateCoupon(ctx, c.ID); err != nil {
			return fmt.Errorf("%s: %w", c.Title, err)
		}
		fmt.Printf("Activated %s %s\n", c.Discount, c.Title)
		count++
	}
	fmt.Printf("%d coupons activated.\n", count)
	return nil
}

func deactivate(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("usage: lidl deactivate <id>...")
	}
	client, err := newClient()
	if err != nil {
		return err
	}
	for _, id := range args {
		if err := client.DeactivateCoupon(ctx, id); err != nil {
			return err
		}
		fmt.Println("Deactivated", id)
	}
	return nil
}

func offers(ctx context.Context, args []string) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	fs := flag.NewFlagSet("offers", flag.ExitOnError)
	store := fs.String("store", cfg.Store, "store ID, as on a receipt (default: the store of your last receipt)")
	asJSON := fs.Bool("json", false, "print the offers as JSON")
	fs.Parse(args)

	client, err := newClient()
	if err != nil {
		return err
	}
	if *store == "" {
		last, err := client.Receipts(ctx, 1)
		if err != nil {
			return err
		}
		if len(last) == 0 {
			return errors.New("no receipt to take the store from; use -store")
		}
		*store = last[0].StoreCode
	}
	list, err := client.Offers(ctx, *store)
	if err != nil {
		return err
	}
	if *store != cfg.Store {
		if cfg, err = loadConfig(); err != nil {
			return err
		}
		cfg.Store = *store
		if err := saveConfig(cfg); err != nil {
			return err
		}
	}
	if *asJSON {
		return printJSON(list)
	}
	for _, o := range list {
		price, old := "", ""
		if o.Price != 0 {
			price = o.Price.String()
		}
		if o.OldPrice != 0 {
			old = "was " + o.OldPrice.String()
		}
		fmt.Printf("%-38s %7s %-9s %-14s until %s\n", o.Title, price, old, o.Discount, o.End.Local().Format("2006-01-02"))
	}
	return nil
}

// wallTime shortens a receipt date to "2006-01-02 15:04".
func wallTime(date string) string {
	if len(date) >= 16 {
		return date[:10] + " " + date[11:16]
	}
	return date
}

func printJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}
