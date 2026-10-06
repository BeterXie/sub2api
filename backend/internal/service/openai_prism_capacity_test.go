package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestPrismBrowserCapacityRejectionOnlyFailsOverBeforeSubmission(t *testing.T) {
	for _, stream := range []bool{false, true} {
		for name, tc := range map[string]struct {
			status int
			body   string
			safe   bool
		}{
			"busy":             {409, `{"error":{"type":"prism_busy","request_submitted":false}}`, true},
			"blocked_pool":     {409, `{"error":{"type":"prism_pending_turn","request_submitted":false}}`, true},
			"unknown_outcome":  {409, `{"error":{"type":"prism_pending_turn","message":"Prism request outcome is unknown"}}`, false},
			"submitted":        {409, `{"error":{"type":"prism_pending_turn","request_submitted":true}}`, false},
			"string_marker":    {409, `{"error":{"type":"prism_pending_turn","request_submitted":"false"}}`, false},
			"numeric_marker":   {409, `{"error":{"type":"prism_pending_turn","request_submitted":0}}`, false},
			"null_marker":      {409, `{"error":{"type":"prism_pending_turn","request_submitted":null}}`, false},
			"unmarked_busy":    {409, `{"error":{"type":"prism_busy"}}`, false},
			"different_error":  {409, `{"error":{"type":"invalid_request","request_submitted":false}}`, false},
			"different_status": {503, `{"error":{"type":"prism_pending_turn","request_submitted":false}}`, false},
			"incomplete_json":  {409, `{"error":{"type":"prism_pending_turn","request_submitted":false}`, false},
		} {
			t.Run(name+map[bool]string{false: "_json", true: "_sse"}[stream], func(t *testing.T) {
				attempts := 0
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					attempts++
					w.WriteHeader(tc.status)
					_, _ = io.WriteString(w, tc.body)
				}))
				defer server.Close()
				s, account := prismTestService(server.URL)
				rec := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(rec)
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
				c.Request.Header.Set("X-Prism-Request-Submitted", "false")
				body := []byte(`{"model":"gpt-6.1-sol","input":"test","stream":false}`)
				if stream {
					body = []byte(`{"model":"gpt-6.1-sol","input":"test","stream":true}`)
				}
				result, err := s.forwardPrismBrowser(context.Background(), c, account, body, time.Now())
				require.Nil(t, result)
				require.Error(t, err)
				require.Equal(t, 1, attempts)
				var failover *UpstreamFailoverError
				if tc.safe {
					require.ErrorAs(t, err, &failover)
					require.True(t, failover.ShouldRetryNextAccount())
					require.False(t, failover.RetryableOnSameAccount)
					require.False(t, c.Writer.Written())
					require.Empty(t, rec.Body.String())
					require.Equal(t, tc.body, string(failover.ResponseBody))
				} else {
					require.False(t, errors.As(err, &failover))
					require.True(t, c.Writer.Written())
					require.Equal(t, tc.status, rec.Code)
					require.NotEmpty(t, gjson.Get(rec.Body.String(), "error.type").String())
					require.NotEmpty(t, gjson.Get(rec.Body.String(), "error.message").String())
					if gjson.Valid(tc.body) {
						require.Equal(t, gjson.Get(tc.body, "error.type").String(), gjson.Get(rec.Body.String(), "error.type").String())
						require.Equal(t, gjson.Get(tc.body, "error.request_submitted").Raw, gjson.Get(rec.Body.String(), "error.request_submitted").Raw)
					}
				}
			})
		}
	}
}
