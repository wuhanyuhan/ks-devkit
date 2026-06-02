package mcpproto

import (
	"encoding/json"
	"testing"
)

func TestJSONRPCRequest_Decode(t *testing.T) {
	raw := `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`
	var req JSONRPCRequest
	if err := json.Unmarshal([]byte(raw), &req); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if req.JSONRPC != "2.0" {
		t.Errorf("jsonrpc: %q", req.JSONRPC)
	}
	if req.Method != "tools/list" {
		t.Errorf("method: %q", req.Method)
	}
}

func TestJSONRPCResponse_Encode(t *testing.T) {
	resp := JSONRPCResponse{JSONRPC: "2.0", ID: 1, Result: map[string]string{"status": "ok"}}
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded map[string]any
	_ = json.Unmarshal(data, &decoded)
	if decoded["jsonrpc"] != "2.0" {
		t.Errorf("jsonrpc: %v", decoded["jsonrpc"])
	}
}

func TestJSONRPCError_Encode(t *testing.T) {
	resp := JSONRPCError{
		JSONRPC: "2.0", ID: 1,
		Error: &JSONRPCErrBody{Code: errCodeMethodNotFound, Message: "method not found"},
	}
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded map[string]any
	_ = json.Unmarshal(data, &decoded)
	errObj := decoded["error"].(map[string]any)
	if errObj["code"].(float64) != -32601 {
		t.Errorf("error code: %v", errObj["code"])
	}
}
