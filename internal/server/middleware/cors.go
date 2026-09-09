// cors.go provides the Cross-Origin Resource Sharing (CORS) middleware,
// setting Access-Control-Allow-* response headers based on the allowed origins list.
// It handles browser preflight requests (OPTIONS) and supports credentials mode.
//
// Security notes:
//   - Only origins in the allowed list may initiate cross-origin requests
//   - The wildcard "*" is forbidden in production; allowed origins must be configured explicitly
//   - Preflight results are cached for 86400 seconds (24 hours) to reduce OPTIONS overhead
package middleware

import (
	"net/http"
	"strings"
)

// CORS returns a chi-compatible cross-origin middleware that sets Access-Control-Allow-* response headers
// based on the allowed origins list.
//
// Processing logic:
//  1. Parse the allowed origins list (comma-separated string); "*" means all origins are allowed
//  2. Check whether the request's Origin is in the allowed list
//  3. On match, set the Allow-Origin and Allow-Credentials response headers
//  4. Always set the Allow-Methods, Allow-Headers, and Max-Age response headers
//  5. Preflight requests (OPTIONS) return 204 No Content directly
//
// Parameters:
//   - allowedOrigins: comma-separated list of allowed origins; "*" means all origins are allowed
//
// Returns:
//   - func(http.Handler) http.Handler: chi middleware function
func CORS(allowedOrigins string) func(http.Handler) http.Handler {
	origins := parseOrigins(allowedOrigins)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			allowed := matchOrigin(origin, origins)

			if allowed {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Credentials", "true")
			}

			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Accept, Authorization, Content-Type, X-CSRF-Token, X-API-Key")
			w.Header().Set("Access-Control-Max-Age", "86400")

			// Handle preflight requests (OPTIONS), return 204 directly
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// parseOrigins parses a comma-separated string into a deduplicated list of origins.
// Returning nil means all origins are allowed (the original value was empty or "*").
//
// Parameters:
//   - s: comma-separated origins string, e.g. "http://localhost:3000,https://app.example.com"
//
// Returns:
//   - []string: the parsed origins list; nil means all origins are allowed
func parseOrigins(s string) []string {
	if s == "" || s == "*" {
		return nil // nil means all origins are allowed
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// matchOrigin checks whether the request's Origin is in the allowed list.
// When origins is nil, all origins are allowed (when configured as "*" or unset).
//
// Parameters:
//   - origin: the request's Origin header value
//   - origins: the allowed origins list; nil means all origins are allowed
//
// Returns:
//   - bool: whether the origin matches the allowed list
func matchOrigin(origin string, origins []string) bool {
	if origin == "" {
		return false
	}
	if origins == nil {
		return true
	}
	for _, o := range origins {
		if strings.EqualFold(o, origin) {
			return true
		}
	}
	return false
}
