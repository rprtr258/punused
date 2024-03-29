package lib

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"

	lsp "github.com/sourcegraph/go-lsp"
	"golang.org/x/sync/errgroup"
)

var requestID uint64 = 5000

func newClient(ctx context.Context, workspaceDir string) (*GoplsClient, error) {
	workspaceDir = path.Clean(filepath.ToSlash(workspaceDir))

	args := []string{"serve"} // , "-rpc.trace", "-logfile=/Users/rprtr258/dev/gopls.log"}
	cmd := exec.Command("gopls", args...)
	cmd.Stderr = os.Stderr
	conn, err := newConn(cmd)
	if err != nil {
		return nil, err
	}

	if err := conn.Start(); err != nil {
		return nil, err
	}

	client := &GoplsClient{conn: conn, workspaceDir: workspaceDir}

	initParams := &lsp.InitializeParams{
		RootURI: lsp.DocumentURI(client.documentURI("")),
		Capabilities: lsp.ClientCapabilities{
			TextDocument: lsp.TextDocumentClientCapabilities{
				DocumentSymbol: struct {
					SymbolKind struct {
						ValueSet []int `json:"valueSet,omitempty"`
					} `json:"symbolKind,omitEmpty"`

					HierarchicalDocumentSymbolSupport bool `json:"hierarchicalDocumentSymbolSupport,omitempty"`
				}{
					HierarchicalDocumentSymbolSupport: true,
				},
			},
		},
	}

	_, err = client.Initialize(ctx, initParams)
	if err != nil {
		return nil, err
	}

	err = client.Initialized(ctx)

	return client, err
}

func newConn(cmd *exec.Cmd) (_ Conn, err error) {
	in, err := cmd.StdinPipe()
	if err != nil {
		return Conn{}, err
	}
	defer func() {
		if err != nil {
			in.Close()
		}
	}()

	out, err := cmd.StdoutPipe()
	c := Conn{out, in, cmd}

	return c, err
}

type Conn struct {
	io.ReadCloser
	io.WriteCloser
	cmd *exec.Cmd
}

// Close closes conn's WriteCloser ReadClosers.
func (c Conn) Close() error {
	writeErr := c.WriteCloser.Close()
	readErr := c.ReadCloser.Close()

	if writeErr != nil && writeErr != os.ErrClosed {
		return writeErr
	}

	if readErr != nil && readErr != os.ErrClosed {
		return readErr
	}

	return nil
}

// Start starts conn's Cmd.
func (c Conn) Start() error {
	err := c.cmd.Start()
	if err != nil {
		return c.Close()
	}
	return err
}

type GoplsClient struct {
	workspaceDir string

	callMu sync.Mutex
	conn   Conn
}

// Call calls the gopls method with the params given. If result is non-nil, the response body is unmarshalled into it.
func (c *GoplsClient) Call(ctx context.Context, method string, params, result interface{}) error {
	// Only allow one call at a time for now.
	c.callMu.Lock()
	defer c.callMu.Unlock()

	id := atomic.AddUint64(&requestID, 1)
	req := request{
		RPCVersion: "2.0",
		ID: lsp.ID{
			Num: id,
		},
		Method: method,
		Params: params,
	}

	if err := c.Write(req); err != nil {
		return err
	}

	respChan := make(chan response)
	wg, ctx := errgroup.WithContext(ctx)
	wg.Go(func() error {
		return c.Read(ctx, id, respChan)
	})

	var unmarshalErr error

	select {
	case resp := <-respChan:
		if result != nil && resp.Result != nil {
			unmarshalErr = json.Unmarshal(resp.Result, result)
		}
	case <-ctx.Done():
		return ctx.Err()
	}

	if err := wg.Wait(); err != nil {
		return err
	}
	return unmarshalErr
}

func (c *GoplsClient) Close() error {
	return c.conn.Close()
}

func (c *GoplsClient) DocumentReferences(ctx context.Context, loc lsp.Location) ([]*lsp.Location, error) {
	start := loc.Range.Start

	params := &lsp.ReferenceParams{
		Context: lsp.ReferenceContext{
			IncludeDeclaration: false,
		},
		TextDocumentPositionParams: lsp.TextDocumentPositionParams{
			TextDocument: lsp.TextDocumentIdentifier{
				URI: loc.URI,
			},
			Position: start,
		},
	}

	var result []*lsp.Location

	if err := c.Call(ctx, "textDocument/references", params, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *GoplsClient) DocumentSymbol(ctx context.Context, filename string) ([]*Symbol, error) {
	uri := lsp.DocumentURI(c.documentURI(filename))
	params := &lsp.DocumentSymbolParams{
		TextDocument: lsp.TextDocumentIdentifier{
			URI: uri,
		},
	}

	var result []DocumentSymbol

	if err := c.Call(ctx, "textDocument/documentSymbol", params, &result); err != nil {
		return nil, err
	}

	var symbols []*Symbol
	for _, r := range result {
		symbols = append(symbols, c.documentSymbolToSymbol(uri, r))
	}

	return symbols, nil
}

// Read reads from gopls until candidate is found and sent on respChan.
func (c *GoplsClient) Read(ctx context.Context, candidate uint64, respChan chan<- response) error {
	done := make(chan bool)
	var wg errgroup.Group

	wg.Go(func() error {
		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
				buff := make([]byte, 16)
				_, err := io.ReadFull(c.conn, buff)
				if err != nil {
					return err
				}

				cl := make([]byte, 0, 2)
				buff = buff[:1]
				for {
					_, err := io.ReadFull(c.conn, buff)
					if err != nil {
						return err
					}
					if buff[0] == '\r' {
						break
					}
					cl = append(cl, buff[0])
				}

				// Consume the \n\r\n
				buff = buff[:3]
				_, err = io.ReadFull(c.conn, buff)
				if err != nil {
					return err
				}

				contentLength, err := strconv.Atoi(string(cl))
				if err != nil {
					return err
				}

				buff = make([]byte, contentLength)
				_, err = io.ReadFull(c.conn, buff)
				if err != nil {
					return err
				}

				var resp response
				if err := json.Unmarshal(buff, &resp); err != nil {
					return err
				}

				// gopls sends a lot of chatter with ID=0 (notifications meant for the editor).
				// We need to ignore those.
				if resp.ID == candidate {
					close(done)
					respChan <- resp
					return nil
				}
			}
		}
	})

	wg.Go(func() error {
		for {
			select {
			case <-done:
				return nil
			case <-ctx.Done():
				return c.conn.Close()
			}
		}
	})

	return wg.Wait()
}

// Write writes a request to gopls using the format specified by:
// https://github.com/Microsoft/language-server-protocol/blob/gh-pages/_specifications/specification-3-14.md#text-documents
func (c *GoplsClient) Write(r request) error {
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}

	if _, err = fmt.Fprintf(c.conn, "Content-Length: %d\r\n\r\n", len(b)); err != nil {
		return err
	}

	_, err = c.conn.Write(b)
	return err
}

func (c *GoplsClient) Initialize(ctx context.Context, params *lsp.InitializeParams) (*lsp.InitializeResult, error) {
	var result lsp.InitializeResult

	if err := c.Call(ctx, "initialize", params, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *GoplsClient) Initialized(ctx context.Context) error {
	return c.Call(ctx, "initialized", &InitializedParams{}, nil)
}

func (c *GoplsClient) documentSymbolToSymbol(uri lsp.DocumentURI, ds DocumentSymbol) *Symbol {
	s := &Symbol{
		Name: ds.Name,
		Kind: ds.Kind,
		Location: lsp.Location{
			URI:   uri,
			Range: ds.SelectionRange,
		},
	}

	for _, d := range ds.Children {
		s.Children = append(s.Children, c.documentSymbolToSymbol(uri, d))
	}

	return s
}

func (c *GoplsClient) documentURI(filename string) string {
	filename = filepath.ToSlash(filename)
	if path.IsAbs(filename) {
		return "file://" + filename
	}
	return "file://" + path.Join(c.workspaceDir, filename)
}

type Symbol struct {
	Name     string
	Kind     lsp.SymbolKind
	Location lsp.Location
	Children []*Symbol
}

type request struct {
	RPCVersion string      `json:"jsonrpc"`
	ID         lsp.ID      `json:"id"`
	Method     string      `json:"method"`
	Params     interface{} `json:"params"`
}

type response struct {
	RPCVersion string          `json:"jsonrpc"`
	ID         uint64          `json:"id"`
	Result     json.RawMessage `json:"result"`
}
