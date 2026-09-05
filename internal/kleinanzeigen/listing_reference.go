package kleinanzeigen

import (
	"net/url"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/johannhipp/kcli/internal/domain"
)

var (
	listingIDPattern        = regexp.MustCompile(`^[0-9]+$`)
	listingURLSuffixPattern = regexp.MustCompile(`^/s-anzeige/[^/]+/([0-9]+)-[0-9]+-[0-9]+/?$`)
)

func ListingReferenceID(reference string) (string, error) {
	if reference == "" || len(reference) > 1024 || !utf8.ValidString(reference) || strings.IndexFunc(reference, func(r rune) bool { return r < 0x20 || r == 0x7f }) >= 0 {
		return "", &domain.Error{Code: domain.CodeInvalidIdentifier, Message: "invalid listing reference"}
	}
	if listingIDPattern.MatchString(reference) {
		return reference, nil
	}
	lower := strings.ToLower(reference)
	if strings.Contains(lower, "%2f") || strings.Contains(lower, "%5c") || strings.Contains(lower, "%2e") || strings.Contains(reference, `\`) {
		return "", &domain.Error{Code: domain.CodeInvalidIdentifier, Message: "invalid listing URL"}
	}
	parsed, err := url.Parse(reference)
	if err != nil || parsed.Scheme != "https" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Port() != "" {
		return "", &domain.Error{Code: domain.CodeInvalidIdentifier, Message: "invalid listing URL"}
	}
	decodedPath, err := url.PathUnescape(parsed.EscapedPath())
	if err != nil || strings.IndexFunc(decodedPath, func(r rune) bool { return r < 0x20 || r == 0x7f }) >= 0 {
		return "", &domain.Error{Code: domain.CodeInvalidIdentifier, Message: "listing URL path contains encoded control characters"}
	}
	host := strings.ToLower(parsed.Hostname())
	if host != "kleinanzeigen.de" && host != "www.kleinanzeigen.de" {
		return "", &domain.Error{Code: domain.CodeInvalidIdentifier, Message: "listing URL host is not allowed"}
	}
	match := listingURLSuffixPattern.FindStringSubmatch(parsed.EscapedPath())
	if len(match) != 2 {
		return "", &domain.Error{Code: domain.CodeInvalidIdentifier, Message: "listing URL path is not a recognized public listing form"}
	}
	return match[1], nil
}
