package providers

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

type outcomeTransport func(*http.Request) (*http.Response, error)

func (f outcomeTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestMeteredPublishMissingIdentityIsAmbiguous(t *testing.T) {
	for _, provider := range []string{"x", "linkedin", "linkedin_page"} {
		t.Run(provider, func(t *testing.T) {
			ctx := WithHTTPClient(context.Background(), &http.Client{Transport: outcomeTransport(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 201, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{}`))}, nil
			})})
			p, _ := Get(provider)
			_, err := p.Publish(ctx, Credentials{"access_token": "test", "person_id": "test", "organization_id": "test"}, PostContent{Text: "test"}, nil)
			var failure *Failure
			if !errors.As(err, &failure) || !failure.OutcomeUnknown || !errors.Is(err, ErrRejected) {
				t.Fatalf("missing identity must keep its allowance and stop automatic retries: %v", err)
			}
		})
	}
}

func TestLinkedInUnresolvedDuplicateIsAmbiguous(t *testing.T) {
	err := linkedinError(409, `{"message":"Duplicate post"}`)
	var failure *Failure
	if !errors.As(err, &failure) || !failure.OutcomeUnknown {
		t.Fatalf("unresolved duplicate must keep its allowance: %v", err)
	}
}
