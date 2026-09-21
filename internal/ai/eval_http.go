package ai

import "net/http"

// The evaluation pool may have hundreds of calls in flight. Keep connections
// available between batches instead of the default two idle sockets per host.
// Production clients retain their existing transport and timeout behavior.
func newEvalHTTPClient() *http.Client {
	transport := http.DefaultTransport
	if base, ok := transport.(*http.Transport); ok {
		tuned := base.Clone()
		tuned.MaxIdleConns = 1024
		tuned.MaxIdleConnsPerHost = 1024
		transport = tuned
	}
	return &http.Client{Transport: transport} // request context owns the deadline
}
