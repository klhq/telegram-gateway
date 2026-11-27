package main

import (
	"io"
	"log/slog"
	"regexp"
)

// botTokenPattern matches the bot token segment of a Bot API URL. Go's
// net/http errors quote the full request URL, so a failed call to
// https://api.telegram.org/bot<token>/sendMessage carries the token.
var botTokenPattern = regexp.MustCompile(`bot[0-9]+:[A-Za-z0-9_-]+`)

func redactBotToken(s string) string {
	return botTokenPattern.ReplaceAllString(s, "bot<redacted>")
}

// newLogHandler returns the JSON handler the gateway logs through. It
// redacts bot tokens from every string and error attribute, so no call
// site has to remember to scrub an error before logging it.
func newLogHandler(w io.Writer, level slog.Leveler) slog.Handler {
	return slog.NewJSONHandler(w, &slog.HandlerOptions{
		Level: level,
		ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
			switch a.Value.Kind() {
			case slog.KindString:
				a.Value = slog.StringValue(redactBotToken(a.Value.String()))
			case slog.KindAny:
				if err, ok := a.Value.Any().(error); ok && err != nil {
					a.Value = slog.StringValue(redactBotToken(err.Error()))
				}
			}
			return a
		},
	})
}
