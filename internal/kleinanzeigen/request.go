package kleinanzeigen

import (
	"context"
	"net/http"
)

// getJSONRequest builds the anonymous JSON GET request shared by the read-only
// discovery and detail paths. It centralises the host, method, response size
// cap, and the operation class/one-shot policy so every anonymous read follows
// the same shape.
func getJSONRequest(ctx context.Context, path string, query map[string][]string, class OperationClass, oneShot bool) Request {
	return Request{
		Context:          ctx,
		Host:             HostMain,
		Method:           http.MethodGet,
		Path:             path,
		Query:            query,
		Class:            class,
		OneShot:          oneShot,
		MaxResponseBytes: JSONResponseLimit,
	}
}
