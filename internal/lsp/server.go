package lsp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

var errExit = errors.New("language server exit")

// Serve runs the grog language server over an LSP stdio transport.
func Serve(context context.Context, reader io.Reader, writer io.Writer) error {
	server := &server{reader: bufio.NewReader(reader), writer: writer, documents: map[string]string{}}
	return server.run(context)
}

type server struct {
	reader    *bufio.Reader
	writer    io.Writer
	documents map[string]string
	shutdown  bool
}

type message struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
}

func (server *server) run(context context.Context) error {
	for {
		select {
		case <-context.Done():
			return context.Err()
		default:
		}
		request, operationError := server.readMessage()
		if operationError != nil {
			if errors.Is(operationError, io.EOF) {
				return nil
			}
			return operationError
		}
		if operationError := server.handle(request); operationError != nil {
			if errors.Is(operationError, errExit) && server.shutdown {
				return nil
			}
			return operationError
		}
	}
}

func (server *server) readMessage() (message, error) {
	var length int
	foundLength := false
	for {
		line, operationError := server.reader.ReadString('\n')
		if operationError != nil {
			return message{}, operationError
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(parts[0]), "Content-Length") {
			length, operationError = strconv.Atoi(strings.TrimSpace(parts[1]))
			if operationError != nil {
				return message{}, fmt.Errorf("parse Content-Length: %w", operationError)
			}
			foundLength = true
		}
	}
	if !foundLength {
		return message{}, fmt.Errorf("missing Content-Length")
	}
	if length < 0 {
		return message{}, fmt.Errorf("invalid Content-Length %d", length)
	}
	payload := make([]byte, length)
	if _, operationError := io.ReadFull(server.reader, payload); operationError != nil {
		return message{}, operationError
	}
	var request message
	if operationError := json.Unmarshal(payload, &request); operationError != nil {
		return message{}, operationError
	}
	return request, nil
}

func (server *server) handle(request message) error {
	if server.shutdown && request.Method != "exit" {
		return server.respondError(request.ID, -32600, "server has shut down")
	}
	switch request.Method {
	case "initialize":
		return server.respond(request.ID, map[string]any{
			"serverInfo": map[string]any{"name": "grog"},
			"capabilities": map[string]any{
				"positionEncoding":       "utf-16",
				"textDocumentSync":       1,
				"hoverProvider":          true,
				"definitionProvider":     true,
				"documentSymbolProvider": true,
				"completionProvider": map[string]any{
					"triggerCharacters": []string{"(", ",", "=", "\"", "'", ":", "_", "/", "."},
				},
				"signatureHelpProvider": map[string]any{"triggerCharacters": []string{"(", ","}},
			}})
	case "shutdown":
		server.shutdown = true
		return server.respond(request.ID, nil)
	case "exit":
		return errExit
	case "initialized", "$/cancelRequest", "$/setTrace":
		return nil
	case "textDocument/didOpen":
		var params didOpenParams
		if operationError := json.Unmarshal(request.Params, &params); operationError != nil {
			return nil
		}
		server.documents[params.TextDocument.URI] = params.TextDocument.Text
		return server.publishDiagnostics(params.TextDocument.URI)
	case "textDocument/didChange":
		var params didChangeParams
		if operationError := json.Unmarshal(request.Params, &params); operationError != nil {
			return nil
		}
		if len(params.ContentChanges) > 0 {
			server.documents[params.TextDocument.URI] = params.ContentChanges[len(params.ContentChanges)-1].Text
		}
		return server.publishDiagnostics(params.TextDocument.URI)
	case "textDocument/didSave":
		var params textDocumentParams
		if operationError := json.Unmarshal(request.Params, &params); operationError != nil {
			return nil
		}
		return server.publishDiagnostics(params.TextDocument.URI)
	case "textDocument/didClose":
		var params textDocumentParams
		if operationError := json.Unmarshal(request.Params, &params); operationError != nil {
			return nil
		}
		delete(server.documents, params.TextDocument.URI)
		return server.notify("textDocument/publishDiagnostics", map[string]any{"uri": params.TextDocument.URI, "diagnostics": []diagnostic{}})
	case "textDocument/completion":
		var params positionedTextDocumentParams
		if operationError := json.Unmarshal(request.Params, &params); operationError != nil {
			return server.respondError(request.ID, -32602, "invalid completion parameters")
		}
		return server.respond(request.ID, server.completionItems(params.TextDocument.URI, params.Position))
	case "textDocument/hover":
		var params positionedTextDocumentParams
		if operationError := json.Unmarshal(request.Params, &params); operationError != nil {
			return server.respondError(request.ID, -32602, "invalid hover parameters")
		}
		return server.respond(request.ID, server.hover(params.TextDocument.URI, params.Position))
	case "textDocument/signatureHelp":
		var params positionedTextDocumentParams
		if operationError := json.Unmarshal(request.Params, &params); operationError != nil {
			return server.respondError(request.ID, -32602, "invalid signature help parameters")
		}
		return server.respond(request.ID, signatureHelp(server.documentText(params.TextDocument.URI), params.Position))
	case "textDocument/definition":
		var params positionedTextDocumentParams
		if operationError := json.Unmarshal(request.Params, &params); operationError != nil {
			return server.respondError(request.ID, -32602, "invalid definition parameters")
		}
		return server.respond(request.ID, server.definition(params.TextDocument.URI, params.Position))
	case "textDocument/documentSymbol":
		var params textDocumentParams
		if operationError := json.Unmarshal(request.Params, &params); operationError != nil {
			return server.respondError(request.ID, -32602, "invalid document symbol parameters")
		}
		return server.respond(request.ID, server.documentSymbols(params.TextDocument.URI))
	default:
		if len(request.ID) > 0 {
			return server.respondError(request.ID, -32601, "method not found")
		}
		return nil
	}
}

func (server *server) respond(requestID json.RawMessage, result any) error {
	return server.write(map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(requestID), "result": result})
}

func (server *server) respondError(requestID json.RawMessage, code int, message string) error {
	if len(requestID) == 0 {
		return nil
	}
	return server.write(map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(requestID), "error": map[string]any{"code": code, "message": message}})
}

func (server *server) notify(method string, params any) error {
	return server.write(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
}

func (server *server) write(value any) error {
	payload, operationError := json.Marshal(value)
	if operationError != nil {
		return operationError
	}
	_, operationError = fmt.Fprintf(server.writer, "Content-Length: %d\r\n\r\n%s", len(payload), payload)
	return operationError
}

type textDocument struct {
	URI  string `json:"uri"`
	Text string `json:"text"`
}
type textDocumentIdentifier struct {
	URI string `json:"uri"`
}
type didOpenParams struct {
	TextDocument textDocument `json:"textDocument"`
}
type didChangeParams struct {
	TextDocument   textDocumentIdentifier `json:"textDocument"`
	ContentChanges []struct {
		Text string `json:"text"`
	} `json:"contentChanges"`
}
type textDocumentParams struct {
	TextDocument textDocumentIdentifier `json:"textDocument"`
}

type positionedTextDocumentParams struct {
	TextDocument textDocumentIdentifier `json:"textDocument"`
	Position     position               `json:"position"`
}

type diagnostic struct {
	Range    rangeValue `json:"range"`
	Severity int        `json:"severity"`
	Source   string     `json:"source"`
	Message  string     `json:"message"`
}
type rangeValue struct {
	Start position `json:"start"`
	End   position `json:"end"`
}
type position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}
