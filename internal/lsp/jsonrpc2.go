// https://microsoft.github.io/language-server-protocol/specifications/base/0.9/specification/
package lsp

import "encoding/json"

// ID represents a JSON-RPC 2.0 request ID, which may be either a
// string or number (or null, which is unsupported).
type ID struct {
	// At most one of Num or Str may be nonzero. If both are zero
	// valued, then IsNum specifies which field's value is to be used
	// as the ID.
	Num uint64
	Str string

	// IsString controls whether the Num or Str field's value should be
	// used as the ID, when both are zero valued. It must always be
	// set to true if the request ID is a string.
	IsString bool
}

// func (id ID) String() string {
// 	if id.IsString {
// 		return strconv.Quote(id.Str)
// 	}
// 	return strconv.FormatUint(id.Num, 10)
// }

// MarshalJSON implements json.Marshaler.
func (id ID) MarshalJSON() ([]byte, error) {
	if id.IsString {
		return json.Marshal(id.Str)
	}
	return json.Marshal(id.Num)
}

// // UnmarshalJSON implements json.Unmarshaler.
// func (id *ID) UnmarshalJSON(data []byte) error {
// 	// Support both uint64 and string IDs.
// 	var v uint64
// 	if err := json.Unmarshal(data, &v); err == nil {
// 		*id = ID{Num: v}
// 		return nil
// 	}
// 	var v2 string
// 	if err := json.Unmarshal(data, &v2); err != nil {
// 		return err
// 	}
// 	*id = ID{Str: v2, IsString: true}
// 	return nil
// }

type Request struct {
	RPCVersion string `json:"jsonrpc"`
	ID         ID     `json:"id"`
	Method     string `json:"method"`
	Params     any    `json:"params"`
}

type ErrorCode int

const (
	// Defined by JSON-RPC
	ParseError     ErrorCode = -32700
	InvalidRequest ErrorCode = -32600
	MethodNotFound ErrorCode = -32601
	InvalidParams  ErrorCode = -32602
	InternalError  ErrorCode = -32603

	// This is the start range of JSON-RPC reserved error codes.
	// It doesn't denote a real error code. No error codes of the
	// base protocol should be defined between the start and end
	// range. For backwards compatibility the `ServerNotInitialized`
	// and the `UnknownErrorCode` are left in the range.
	jsonrpcReservedErrorRangeStart ErrorCode = -32099

	// Error code indicating that a server received a notification or
	// request before the server has received the `initialize` request.
	ServerNotInitialized ErrorCode = -32002
	UnknownErrorCode     ErrorCode = -32001

	// This is the end range of JSON-RPC reserved error codes.
	// It doesn't denote a real error code.
	jsonrpcReservedErrorRangeEnd = -32000

	// This is the start range of LSP reserved error codes.
	// It doesn't denote a real error code.
	lspReservedErrorRangeStart ErrorCode = -32899

	// A request failed but it was syntactically correct, e.g the
	// method name was known and the parameters were valid. The error
	// message should contain human readable information about why
	// the request failed.
	RequestFailed ErrorCode = -32803

	// The server cancelled the request. This error code should
	// only be used for requests that explicitly support being
	// server cancellable.
	ServerCancelled ErrorCode = -32802

	// The server detected that the content of a document got
	// modified outside normal conditions. A server should
	// NOT send this error code if it detects a content change
	// in it unprocessed messages. The result even computed
	// on an older state might still be useful for the client.
	//
	// If a client decides that a result is not of any use anymore
	// the client should cancel the request.
	ContentModified ErrorCode = -32801

	// The client has canceled a request and a server has detected
	// the cancel.
	RequestCancelled ErrorCode = -32800

	// This is the end range of LSP reserved error codes.
	// It doesn't denote a real error code.
	lspReservedErrorRangeEnd ErrorCode = -32800
)

type ResponseError struct {
	// A number indicating the error type that occurred.
	Code ErrorCode `json:"code"`

	// A string providing a short description of the error.
	Message string `json:"message"`

	// A primitive or structured value that contains additional
	// information about the error. Can be omitted.
	Data json.RawMessage `json:"data"`
}

type Response struct {
	RPCVersion string          `json:"jsonrpc"`
	ID         uint64          `json:"id"`
	Result     json.RawMessage `json:"result"`
	// The error object in case a request fails.
	Error *ResponseError `json:"error"`
}
