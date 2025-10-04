package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"

	"github.com/pkg/errors"
	"github.com/sourcegraph/conc/pool"
	"golang.org/x/sync/errgroup"

	"github.com/rprtr258/punused/internal/lsp"
)

var requestID uint64 = 5000

func newClient(ctx context.Context, workspaceDir string) (*GoplsClient, error) {
	args := []string{
		"serve",
		// "-rpc.trace",
		// "-logfile=gopls.log",
	}
	cmd := exec.Command("gopls", args...)
	cmd.Stderr = os.Stderr
	cmd.Dir = workspaceDir
	conn, err := newConn(cmd)
	if err != nil {
		return nil, errors.Wrap(err, "newConn")
	}

	if err := conn.Start(); err != nil {
		return nil, errors.Wrap(err, "start")
	}

	client := &GoplsClient{
		ctx:          ctx,
		workspaceDir: filepath.Clean(filepath.ToSlash(workspaceDir)),
		conn:         conn,
	}

	initParams := &lsp.InitializeParams{
		RootURI: client.documentURI(""),
		Capabilities: lsp.ClientCapabilities{
			TextDocument: lsp.TextDocumentClientCapabilities{
				DocumentSymbol: struct {
					// SymbolKind struct {
					// 	ValueSet []int `json:"valueSet,omitempty"`
					// } `json:"symbolKind"`
					HierarchicalDocumentSymbolSupport bool `json:"hierarchicalDocumentSymbolSupport,omitempty"`
				}{
					HierarchicalDocumentSymbolSupport: true,
				},
			},
		},
		WorkspaceFolders: []lsp.WorkspaceFolder{
			{
				URI: client.documentURI(""),
			},
		},
	}

	if _, err := client.Initialize(initParams); err != nil {
		return nil, errors.Wrap(err, "initialize")
	}

	err = client.Initialized()

	return client, errors.Wrap(err, "initialized")
}

func newConn(cmd *exec.Cmd) (_ Conn, err error) {
	in, err := cmd.StdinPipe()
	if err != nil {
		return Conn{}, err
	}
	defer func() {
		if err != nil {
			_ = in.Close()
		}
	}()

	out, err := cmd.StdoutPipe()
	return Conn{out, in, cmd}, err
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
		return errors.Wrapf(err, "close: %v", c.Close())
	}
	return errors.Wrap(err, "start")
}

type GoplsClient struct {
	ctx          context.Context
	workspaceDir string

	callMu sync.Mutex
	conn   Conn
}

// Call calls the gopls method with the params given. If result is non-nil, the response body is unmarshalled into it.
func (c *GoplsClient) Call(method string, params, result any) error {
	// Only allow one call at a time for now.
	c.callMu.Lock()
	defer c.callMu.Unlock()

	id := atomic.AddUint64(&requestID, 1)
	req := lsp.Request{
		RPCVersion: "2.0",
		ID: lsp.ID{
			Num: id,
		},
		Method: method,
		Params: params,
	}

	if err := c.Write(req); err != nil {
		return errors.Wrap(err, "write")
	}

	respChan := make(chan lsp.Response)
	wg, ctx := errgroup.WithContext(c.ctx)
	wg.Go(func() error {
		// Read from gopls until candidate is found and sent on respChan.
		done := make(chan bool)
		wg := pool.New().WithContext(ctx).WithCancelOnError()
		wg.Go(func(ctx context.Context) error {
			for {
				select {
				case <-ctx.Done():
					return ctx.Err()
				default:
					buff := make([]byte, 16)
					if _, err := io.ReadFull(c.conn, buff); err != nil {
						return errors.Wrap(err, "ReadFull")
					}

					cl := make([]byte, 0, 2)
					buff = buff[:1]
					for {
						if _, err := io.ReadFull(c.conn, buff); err != nil {
							return errors.Wrap(err, "ReadFull")
						}
						if buff[0] == '\r' {
							break
						}
						cl = append(cl, buff[0])
					}

					// Consume the \n\r\n
					buff = buff[:3]
					if _, err := io.ReadFull(c.conn, buff); err != nil {
						return errors.Wrap(err, "ReadFull")
					}

					contentLength, err := strconv.Atoi(string(cl))
					if err != nil {
						return errors.Wrap(err, "atoi")
					}

					buff = make([]byte, contentLength)
					if _, err = io.ReadFull(c.conn, buff); err != nil {
						return errors.Wrap(err, "ReadFull")
					}

					var resp lsp.Response
					if err := json.Unmarshal(buff, &resp); err != nil {
						return errors.Wrap(err, "unmarshal")
					}

					// gopls sends a lot of chatter with ID=0 (notifications meant for the editor).
					// We need to ignore those.
					if resp.ID == id {
						close(done)
						respChan <- resp
						return nil
					}
				}
			}
		})

		wg.Go(func(ctx context.Context) error {
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
	})

	var unmarshalErr error
	select {
	case resp := <-respChan:
		if resp.Error != nil {
			log.Println(resp.Error)
		}
		if result != nil && resp.Result != nil {
			unmarshalErr = json.Unmarshal(resp.Result, result)
		}
	case <-ctx.Done():
		return ctx.Err()
	}

	if err := wg.Wait(); err != nil {
		return errors.Wrap(err, "wait")
	}
	return errors.Wrap(unmarshalErr, "unmarshal")
}

func (c *GoplsClient) Close() error {
	return c.conn.Close()
}

func (c *GoplsClient) DocumentReferences(loc lsp.Location) ([]lsp.Location, error) {
	params := &lsp.ReferencesParams{
		Context: lsp.ReferenceContext{
			IncludeDeclaration: false,
		},
		TextDocumentPositionParams: lsp.TextDocumentPositionParams{
			TextDocument: lsp.TextDocumentIdentifier{
				URI: loc.URI,
			},
			Position: loc.Range.Start,
		},
	}

	var result []lsp.Location
	if err := c.Call("textDocument/references", params, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *GoplsClient) DocumentSymbol(filename string) ([]DocumentSymbol, error) {
	uri := c.documentURI(filename)
	params := &lsp.DocumentSymbolParams{
		TextDocument: lsp.TextDocumentIdentifier{
			URI: uri,
		},
	}

	var result []DocumentSymbol
	if err := c.Call("textDocument/documentSymbol", params, &result); err != nil {
		return nil, err
	}
	b, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}
	if err := c.Call("textDocument/didOpen", lsp.DidOpenTextDocumentParams{
		TextDocument: lsp.TextDocumentItem{
			URI:        uri,
			LanguageID: "go",
			Version:    1,
			Text:       string(b),
		},
	}, &struct{}{}); err != nil {
		return nil, err
	}

	return result, nil
}

// Write writes a request to gopls using the format specified by:
// https://github.com/Microsoft/language-server-protocol/blob/gh-pages/_specifications/specification-3-14.md#text-documents
func (c *GoplsClient) Write(r lsp.Request) error {
	b, err := json.Marshal(r)
	if err != nil {
		return errors.Wrap(err, "marshal")
	}

	if _, err = fmt.Fprintf(c.conn, "Content-Length: %d\r\n\r\n", len(b)); err != nil {
		return errors.Wrap(err, "write content-length")
	}

	_, err = c.conn.Write(b)
	return errors.Wrap(err, "write")
}

func (c *GoplsClient) Initialize(params *lsp.InitializeParams) (*lsp.InitializeResult, error) {
	var result lsp.InitializeResult
	if err := c.Call("initialize", params, &result); err != nil {
		return nil, errors.Wrap(err, "initialize")
	}
	return &result, nil
}

func (c *GoplsClient) Initialized() error {
	return c.Call("initialized", &lsp.InitializedParams{}, nil)
}

type Symbol struct {
	DocumentSymbol
	URI lsp.URI
}

func (c *GoplsClient) documentURI(filename string) lsp.URI {
	filename = filepath.ToSlash(filename)
	if filepath.IsAbs(filename) {
		return lsp.URI("file://" + filename)
	}
	return lsp.URI("file://" + filepath.Join(c.workspaceDir, filename))
}
