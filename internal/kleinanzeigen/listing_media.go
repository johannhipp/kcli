package kleinanzeigen

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
)

func (t *HTTPTransport) OpenMedia(input Request) (MediaResponse, error) {
	if input.Host != HostMedia {
		return MediaResponse{}, fmt.Errorf("streaming media requires the media host")
	}
	requestURL, err := t.requestURL(input)
	if err != nil {
		return MediaResponse{}, err
	}
	if input.Context == nil {
		input.Context = context.Background()
	}
	request, err := http.NewRequestWithContext(input.Context, http.MethodGet, requestURL.String(), nil)
	if err != nil {
		return MediaResponse{}, fmt.Errorf("create media request: %w", err)
	}
	for key, values := range input.Headers {
		if strings.ContainsAny(key, "\r\n") {
			return MediaResponse{}, fmt.Errorf("invalid header name")
		}
		for _, value := range values {
			if strings.ContainsAny(value, "\r\n") {
				return MediaResponse{}, fmt.Errorf("invalid header value")
			}
			request.Header.Add(key, value)
		}
	}
	client := *t.client
	client.CheckRedirect = func(next *http.Request, _ []*http.Request) error {
		if t.redirectAllowed(HostMedia, next.URL) {
			return nil
		}
		return http.ErrUseLastResponse
	}
	response, err := client.Do(request)
	if err != nil {
		return MediaResponse{}, ConnectError(err)
	}
	return MediaResponse{StatusCode: response.StatusCode, Headers: cloneHeaders(response.Header), Body: response.Body}, nil
}

func (t *MobileTransport) OpenMedia(input Request) (MediaResponse, error) {
	if input.Context == nil {
		input.Context = context.Background()
	}
	requestContext, cancel := requestDeadline(input.Context, HostMedia, input.Timeout)
	input.Context = requestContext
	input.Host = HostMedia
	input.Method = http.MethodGet
	input.Headers = t.mobileHeaders(HostMedia, input.Headers)
	if err := t.reserve(requestContext, HostMedia, false); err != nil {
		cancel()
		return MediaResponse{}, err
	}
	if opener, ok := t.base.(interface {
		OpenMedia(Request) (MediaResponse, error)
	}); ok {
		response, err := opener.OpenMedia(input)
		if err != nil {
			cancel()
			return MediaResponse{}, err
		}
		response.Body = &listingCancelReadCloser{ReadCloser: response.Body, cancel: cancel}
		return response, nil
	}
	response, err := t.base.Do(input)
	if err != nil {
		cancel()
		return MediaResponse{}, err
	}
	return MediaResponse{StatusCode: response.StatusCode, Headers: response.Headers, Body: &listingCancelReadCloser{ReadCloser: io.NopCloser(bytes.NewReader(response.Body)), cancel: cancel}}, nil
}

type listingCancelReadCloser struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (r *listingCancelReadCloser) Close() error {
	err := r.ReadCloser.Close()
	r.cancel()
	return err
}

func listingMediaMaps(media []ListingMedia) []map[string]any {
	out := make([]map[string]any, 0, len(media))
	for _, item := range media {
		entry := map[string]any{"index": item.Index, "relation": item.Relation, "url": item.URL}
		if item.Width > 0 {
			entry["width"] = item.Width
		}
		if item.Height > 0 {
			entry["height"] = item.Height
		}
		if item.SizeLabel != "" {
			entry["size_label"] = item.SizeLabel
		}
		out = append(out, entry)
	}
	return out
}
