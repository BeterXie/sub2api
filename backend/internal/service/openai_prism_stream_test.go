package service

import (
	"bytes"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestPrismEventHeartbeatAndTerminalShareIdentityAndSequence(t *testing.T) {
	c, _ := gin.CreateTestContext(nil)
	s := &prismBrowserStream{id: "resp_client", reasoningID: "rs_transport", model: "gpt-6.1-sol", createdAt: time.Now().Unix()}
	c.Set(prismBrowserStreamKey, s)
	data := append(s.heartbeat(), s.heartbeat()...)
	tool := prismToolResponse("function_call")
	delete(tool["output"].([]any)[0].(map[string]any), "namespace")
	terminal, id, err := prismBrowserStreamResponse(c, encodePrismEvents(prismToolEvents(t, tool)))
	require.NoError(t, err)
	require.Equal(t, s.id, id)
	data = append(data, terminal...)
	sequence, emptyDeltas, terminals := 0, 0, 0
	for _, line := range bytes.Split(data, []byte("\n")) {
		if !bytes.HasPrefix(line, []byte("data:")) {
			continue
		}
		event := gjson.ParseBytes(bytes.TrimSpace(bytes.TrimPrefix(line, []byte("data:"))))
		require.Equal(t, int64(sequence), event.Get("sequence_number").Int())
		sequence++
		switch event.Get("type").String() {
		case "response.reasoning_summary_text.delta":
			require.Equal(t, "", event.Get("delta").String())
			emptyDeltas++
		case "response.created":
			require.Equal(t, s.id, event.Get("response.id").String())
		case "response.function_call_arguments.done":
			require.Equal(t, int64(1), event.Get("output_index").Int())
		case "response.completed":
			terminals++
			require.Equal(t, s.id, event.Get("response.id").String())
			require.Equal(t, "reasoning", event.Get("response.output.0.type").String())
			require.Empty(t, event.Get("response.output.0.summary.0.text").String())
			require.Equal(t, "call_prism_fixture", event.Get("response.output.1.call_id").String())
		}
	}
	require.Equal(t, 2, emptyDeltas)
	require.Equal(t, 1, terminals)
}
