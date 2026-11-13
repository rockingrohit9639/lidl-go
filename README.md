# lidl-go

An unofficial Go client and command line tool for Lidl Plus. It reads your digital receipts.

This project is not affiliated with Lidl. It uses the private API of the Lidl Plus app, which can change or stop working at any time. Use it with your own account only.

## Install

```bash
go install github.com/rockingrohit9639/lidl-go/cmd/lidl@latest
```

## Log in

```bash
lidl login -country DE -language de
```

This opens the Lidl login page in your browser. Lidl sends the result to a link that only the phone app understands, so you copy it by hand:

1. Before you log in, open DevTools, go to the Network tab and tick "Preserve log".
2. Log in.
3. The last request is a link that starts with `com.lidlplus.app://callback`. Copy it.
4. Paste it into the terminal, or run `lidl login -code "<link>"`.

You do this once. The token is saved in `~/.config/lidl-go/config.json`, readable only by you. Set `LIDL_GO_CONFIG` to use another file.

## Receipts

```bash
lidl receipts            # the last 20 receipts
lidl receipts -limit 0   # all receipts
lidl receipts -json      # the list as Lidl sent it
lidl receipt <id>        # one receipt with every item and price
lidl receipt -json <id>  # the same as JSON
lidl receipt -raw <id>   # the receipt as Lidl sent it
```

Items are read from the printed receipt that Lidl sends as HTML. This is tested with receipts from Germany.

## Use it from Go

```go
client := lidl.NewClient("DE", "de", refreshToken)
client.OnRefresh = func(tok lidl.Token) error {
	return save(tok.RefreshToken) // Lidl replaces the refresh token on every refresh
}

list, err := client.Receipts(ctx, 10)
receipt, err := client.Receipt(ctx, list[0].ID)
for _, item := range receipt.Items {
	fmt.Println(item.Name, item.Quantity, item.Amount)
}
```

## Thanks

The endpoints and the login flow were worked out by [Andre0512/lidl-plus](https://github.com/Andre0512/lidl-plus), [yagueto/lidl-plus](https://github.com/yagueto/lidl-plus) and [erwindamsma/lidl-receipts-analyser](https://github.com/erwindamsma/lidl-receipts-analyser).
