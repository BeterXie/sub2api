package service

import (
	"bufio"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func readPrismKeepalive(t *testing.T, reader *bufio.Reader) {
	t.Helper()
	line, err := reader.ReadString('\n')
	require.NoError(t, err)
	require.Equal(t, ": keepalive\n", line, "only a comment may precede the validated result")
	line, err = reader.ReadString('\n')
	require.NoError(t, err)
	require.Equal(t, "\n", line)
}

func TestPrismAccountTestKeepaliveDuringBufferedResult(t *testing.T) {
	for _, tc := range []struct {
		name, response string
		status         int
		wantError      bool
	}{
		{"completed", prismFallbackFixtureResponse, 200, false},
		{"unknown_outcome", `{"error":{"type":"prism_pending_turn","message":"outcome unknown"}}`, 409, true},
		{"unfinished", `{"status":"in_progress"}`, 200, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gate := make(chan struct{})
			var release sync.Once
			var attempts atomic.Int32
			adapter := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				attempts.Add(1)
				select {
				case <-gate:
				case <-r.Context().Done():
					return
				}
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.response)
			}))
			t.Cleanup(func() { release.Do(func() { close(gate) }); adapter.Close() })
			gatewayService, account := prismTestService(adapter.URL)
			gatewayService.cfg.Gateway.StreamKeepaliveInterval = 1
			service := &AccountTestService{openaiGatewayService: gatewayService}
			completed := make(chan error, 1)
			router := gin.New()
			router.POST("/pelican-test", func(c *gin.Context) {
				completed <- service.testPrismBrowserConnection(c, account, "gpt-6.1-sol", "draw a pelican")
			})
			gateway := httptest.NewServer(router)
			t.Cleanup(gateway.Close)
			client := &http.Client{Timeout: 8 * time.Second}
			response, err := client.Post(gateway.URL+"/pelican-test", "application/json", strings.NewReader(`{}`))
			require.NoError(t, err)
			defer response.Body.Close()
			require.Equal(t, "text/event-stream", response.Header.Get("Content-Type"))
			reader := bufio.NewReader(response.Body)
			for _, eventType := range []string{"test_start", "status"} {
				line, err := reader.ReadString('\n')
				require.NoError(t, err)
				require.Contains(t, line, `"type":"`+eventType+`"`)
				line, err = reader.ReadString('\n')
				require.NoError(t, err)
				require.Equal(t, "\n", line)
			}
			readPrismKeepalive(t, reader)
			release.Do(func() { close(gate) })
			body, err := io.ReadAll(reader)
			require.NoError(t, err)
			testErr := <-completed
			require.EqualValues(t, 1, attempts.Load(), "a test must not replay an unknown request")
			if tc.wantError {
				require.Error(t, testErr)
				require.Equal(t, 1, strings.Count(string(body), `"type":"error"`))
				require.NotContains(t, string(body), `"type":"content"`)
				require.NotContains(t, string(body), `"type":"test_complete"`)
			} else {
				require.NoError(t, testErr)
				require.Equal(t, 1, strings.Count(string(body), `"type":"content"`))
				require.Equal(t, 1, strings.Count(string(body), `"type":"test_complete"`))
				output, message := parseTestSSEOutput(string(body))
				require.Equal(t, "OK", output)
				require.Empty(t, message)
			}
		})
	}
}

func TestPrismBrowserStreamKeepaliveBeforeBufferedResult(t *testing.T) {
	tool := prismToolResponse("function_call")
	delete(tool["output"].([]any)[0].(map[string]any), "namespace")
	toolResponse := encodePrismEvents(prismToolEvents(t, tool))
	const toolRequest = `{"model":"gpt-6.1-sol","input":"test","stream":true,"tools":[{"type":"function","name":"lookup","parameters":{"type":"object"}}]}`
	const textRequest = `{"model":"gpt-6.1-sol","input":"test","stream":true}`
	textResponse := []byte("data: {\"type\":\"response.completed\",\"response\":" + prismFallbackFixtureResponse + "}\n\n")
	for _, tc := range []struct {
		name, request, wantError string
		status                   int
		response                 []byte
	}{
		{"text", textRequest, "", 200, textResponse},
		{"tool", toolRequest, "", 200, toolResponse},
		{"undeclared_tool", textRequest, "invalid_prism_response", 200, toolResponse},
		{"unknown_outcome", textRequest, "prism_pending_turn", 409, []byte(`{"error":{"type":"prism_pending_turn","message":"outcome unknown"}}`)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gate := make(chan struct{})
			var release sync.Once
			var attempts atomic.Int32
			adapter := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				attempts.Add(1)
				select {
				case <-gate:
				case <-r.Context().Done():
					return
				}
				w.WriteHeader(tc.status)
				_, _ = w.Write(tc.response)
			}))
			t.Cleanup(func() { release.Do(func() { close(gate) }); adapter.Close() })
			s, account := prismTestService(adapter.URL)
			s.cfg.Gateway.StreamKeepaliveInterval = 1
			type outcome struct {
				result *OpenAIForwardResult
				err    error
			}
			completed := make(chan outcome, 1)
			router := gin.New()
			router.POST("/v1/responses", func(c *gin.Context) {
				result, err := s.forwardPrismBrowser(c.Request.Context(), c, account, []byte(tc.request), time.Now())
				completed <- outcome{result, err}
			})
			gateway := httptest.NewServer(router)
			t.Cleanup(gateway.Close)
			client := &http.Client{Timeout: 8 * time.Second}
			response, err := client.Post(gateway.URL+"/v1/responses", "application/json", strings.NewReader(tc.request))
			require.NoError(t, err, "response headers must arrive while the adapter is still blocked")
			defer response.Body.Close()
			require.Equal(t, http.StatusOK, response.StatusCode)
			require.Equal(t, "text/event-stream", response.Header.Get("Content-Type"))
			require.Equal(t, "no", response.Header.Get("X-Accel-Buffering"))
			reader := bufio.NewReader(response.Body)
			readPrismKeepalive(t, reader)
			release.Do(func() { close(gate) })
			body, err := io.ReadAll(reader)
			require.NoError(t, err)
			result := <-completed
			require.EqualValues(t, 1, attempts.Load(), "an unknown outcome must never be replayed")
			if tc.wantError == "" {
				require.NoError(t, result.err)
				require.NotNil(t, result.result)
				require.Equal(t, 1, strings.Count(string(body), `"type":"response.completed"`))
				require.NotContains(t, string(body), "response.failed")
				require.GreaterOrEqual(t, *result.result.FirstTokenMs, 1000, "comments must not count as a first model token")
				if tc.name == "tool" {
					require.Equal(t, 1, strings.Count(string(body), `"type":"response.output_item.done"`))
					require.Contains(t, string(body), `"call_id":"call_prism_fixture"`)
				}
			} else {
				require.Error(t, result.err)
				require.Nil(t, result.result)
				require.Equal(t, 1, strings.Count(string(body), `"type":"response.failed"`))
				require.Contains(t, string(body), `"code":"`+tc.wantError+`"`)
				require.NotContains(t, string(body), "call_prism_fixture", "unvalidated tool output must remain buffered")
				require.NotContains(t, string(body), "response.completed")
				var failover *UpstreamFailoverError
				require.False(t, errors.As(result.err, &failover))
			}
		})
	}
}

func TestPrismBrowserKeepaliveSurvivesCapacityFailover(t *testing.T) {
	gates := []chan struct{}{make(chan struct{}), make(chan struct{})}
	var releases [2]sync.Once
	var attempts atomic.Int32
	admitted := make(chan int, 2)
	adapter := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		index := int(attempts.Add(1)) - 1
		if index >= len(gates) {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		admitted <- index
		select {
		case <-gates[index]:
		case <-r.Context().Done():
			return
		}
		if index == 0 {
			w.WriteHeader(http.StatusConflict)
			_, _ = io.WriteString(w, `{"error":{"type":"prism_busy","request_submitted":false}}`)
			return
		}
		_, _ = io.WriteString(w, "data: {\"type\":\"response.completed\",\"response\":"+prismFallbackFixtureResponse+"}\n\n")
	}))
	t.Cleanup(func() {
		for i := range gates {
			releases[i].Do(func() { close(gates[i]) })
		}
		adapter.Close()
	})
	s, account := prismTestService(adapter.URL)
	s.cfg.Gateway.StreamKeepaliveInterval = 1
	const request = `{"model":"gpt-6.1-sol","input":"test","stream":true}`
	type outcome struct {
		failover      bool
		committed     bool
		before, after int
		result        *OpenAIForwardResult
		err           error
	}
	completed := make(chan outcome, 1)
	router := gin.New()
	router.POST("/v1/responses", func(c *gin.Context) {
		before := OpenAICompactKeepaliveAdjustedWrittenSize(c)
		_, err := s.forwardPrismBrowser(c.Request.Context(), c, account, []byte(request), time.Now())
		var failover *UpstreamFailoverError
		state := outcome{failover: errors.As(err, &failover), before: before,
			after: OpenAICompactKeepaliveAdjustedWrittenSize(c), committed: IsResponseCommitted(c)}
		if !state.failover {
			state.err = err
			completed <- state
			return
		}
		state.result, state.err = s.forwardPrismBrowser(c.Request.Context(), c, account, []byte(request), time.Now())
		completed <- state
	})
	gateway := httptest.NewServer(router)
	t.Cleanup(gateway.Close)
	client := &http.Client{Timeout: 8 * time.Second}
	response, err := client.Post(gateway.URL+"/v1/responses", "application/json", strings.NewReader(request))
	require.NoError(t, err)
	defer response.Body.Close()
	reader := bufio.NewReader(response.Body)
	readPrismKeepalive(t, reader)
	require.Equal(t, 0, <-admitted)
	releases[0].Do(func() { close(gates[0]) })
	select {
	case index := <-admitted:
		require.Equal(t, 1, index)
	case <-time.After(4 * time.Second):
		t.Fatal("capacity rejection did not permit another account attempt")
	}
	readPrismKeepalive(t, reader)
	releases[1].Do(func() { close(gates[1]) })
	body, err := io.ReadAll(reader)
	require.NoError(t, err)
	state := <-completed
	require.True(t, state.failover)
	require.False(t, state.committed, "a heartbeat must not mark a semantic response as committed")
	require.Equal(t, state.before, state.after, "heartbeat bytes must not prevent the handler from switching accounts")
	require.NoError(t, state.err)
	require.NotNil(t, state.result)
	require.EqualValues(t, 2, attempts.Load())
	require.Equal(t, 1, strings.Count(string(body), `"type":"response.completed"`))
	require.NotContains(t, string(body), "response.failed")
}
