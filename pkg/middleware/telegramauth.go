package middleware

import (
	"crypto/subtle"
	"net/http"
)

// TelegramSecretHeader is the header Telegram sets on webhook requests when
// a secret_token was configured via setWebhook (NFR-23).
const TelegramSecretHeader = "X-Telegram-Bot-Api-Secret-Token" // #nosec G101 -- header name, not a credential value

// TelegramSecretAuth returns middleware validating the Telegram webhook
// secret token header using a constant-time comparison. Requests with a
// missing or mismatched token receive a 401 with the standardized error
// envelope (NFR-21).
//
// An empty secret always denies (rather than only matching an empty header):
// r.Header.Get returns "" for a missing header, so comparing empty-to-empty
// would otherwise let every request through when the deployment forgot to
// configure TELEGRAM_WEBHOOK_SECRET — an auth bypass, not a safe default.
func TelegramSecretAuth(secret string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			got := r.Header.Get(TelegramSecretHeader)
			if secret == "" || subtle.ConstantTimeCompare([]byte(got), []byte(secret)) != 1 {
				WriteError(w, r, http.StatusUnauthorized, "unauthorized", "invalid webhook secret token")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
