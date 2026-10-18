package common

// CanonicalRelayPath maps the registered playground endpoint to its public API
// path for channel selection and forwarding without changing the HTTP request.
func CanonicalRelayPath(path string) string {
	if path == "/pg/chat/completions" {
		return "/v1/chat/completions"
	}
	return path
}
